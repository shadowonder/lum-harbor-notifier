package attention

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"lum-harbor-notifier/internal/foundation"
	"lum-harbor-notifier/internal/identity"
	"lum-harbor-notifier/internal/tracking"
)

const (
	entityTypeAttention    = "ATTENTION_ISSUE"
	entityTypeNotification = "NOTIFICATION"
	maxReconcileAttempts   = 3
)

// Service owns Attention/Notification validation, transactions and Revisions.
// System reconciliation uses SYSTEM Revisions; human interactions use USER.
type Service struct {
	db       *sql.DB
	tracking *tracking.Service
	clock    func() time.Time
}

func NewService(db *sql.DB, trackingService *tracking.Service) *Service {
	return &Service{db: db, tracking: trackingService, clock: func() time.Time { return time.Now().UTC() }}
}

type SnoozeInput struct {
	ID    string
	Until time.Time
}

// Reconcile deterministically evaluates rule MONITOR_OVERDUE_UNMET against one
// consistent transactional snapshot of Tracking truth: an ACTIVE MonitorPoint
// with due_at <= now whose typed expectation is unmet has exactly one OPEN
// AttentionIssue (and, on creation, exactly one initial Notification); an OPEN
// issue whose condition no longer holds is RESOLVED. It is idempotent, uses no
// AI, and never mutates Tracking truth.
func (s *Service) Reconcile(ctx context.Context, now time.Time) (ReconcileResult, error) {
	now = now.UTC()
	var lastErr error
	for attempt := 0; attempt < maxReconcileAttempts; attempt++ {
		result, err := s.reconcileOnce(ctx, now)
		if err == nil {
			return result, nil
		}
		// A concurrent reconciler winning the dedupe race surfaces as a unique
		// violation; the whole transaction is rolled back and re-evaluated, which
		// then sees the winner's issue and converges to the same logical result.
		if !isUniqueViolation(err) {
			return ReconcileResult{}, err
		}
		lastErr = err
	}
	return ReconcileResult{}, lastErr
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func inapplicable(err error) bool {
	return errors.Is(err, tracking.ErrWorkItemUnavailable) || errors.Is(err, tracking.ErrTrackedItemNotFound) ||
		errors.Is(err, tracking.ErrMonitorPointNotFound) || errors.Is(err, tracking.ErrStepNotInWorkflow)
}

type unmetMonitor struct {
	point      tracking.MonitorPoint
	evaluation tracking.ExpectationEvaluation
}

func (s *Service) reconcileOnce(ctx context.Context, now time.Time) (ReconcileResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("begin attention reconcile transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	repo := NewRepository(tx)

	due, err := s.tracking.ListDueActiveMonitorPointsTx(ctx, tx, now)
	if err != nil {
		return ReconcileResult{}, err
	}
	result := ReconcileResult{Evaluated: len(due)}
	unmet := map[string]unmetMonitor{}
	order := make([]string, 0, len(due))
	for _, point := range due {
		point := point
		evaluation, err := s.tracking.EvaluateExpectationTx(ctx, tx, &point)
		if err != nil {
			if inapplicable(err) {
				continue
			}
			return ReconcileResult{}, fmt.Errorf("evaluate monitor point %s: %w", point.ID, err)
		}
		if !evaluation.Met {
			unmet[point.ID] = unmetMonitor{point: point, evaluation: evaluation}
			order = append(order, point.ID)
		}
	}

	open, err := repo.listOpenIssues(ctx)
	if err != nil {
		return ReconcileResult{}, err
	}
	openMonitors := map[string]struct{}{}
	for _, issue := range open {
		if issue.Type != IssueMonitorOverdueUnmet || issue.MonitorPointID == nil {
			continue
		}
		if _, stillUnmet := unmet[*issue.MonitorPointID]; stillUnmet {
			openMonitors[*issue.MonitorPointID] = struct{}{}
			continue
		}
		if err := s.resolveInTx(ctx, tx, repo, issue, now); err != nil {
			return ReconcileResult{}, err
		}
		result.Resolved++
	}

	sort.SliceStable(order, func(i, j int) bool {
		a, b := unmet[order[i]].point, unmet[order[j]].point
		if !a.DueAt.Equal(b.DueAt) {
			return a.DueAt.Before(b.DueAt)
		}
		return a.ID < b.ID
	})
	for _, monitorID := range order {
		if _, exists := openMonitors[monitorID]; exists {
			continue
		}
		if err := s.openInTx(ctx, tx, repo, unmet[monitorID], now); err != nil {
			return ReconcileResult{}, err
		}
		result.Opened++
	}
	if err := tx.Commit(); err != nil {
		return ReconcileResult{}, fmt.Errorf("commit attention reconcile transaction: %w", err)
	}
	return result, nil
}

// openInTx atomically creates the SYSTEM Revision, the AttentionIssue Entity and
// row, and the single initial Notification Entity and row. The Attention issue
// and its initial Notification share one logical SYSTEM Revision.
func (s *Service) openInTx(ctx context.Context, tx *sql.Tx, repo *Repository, m unmetMonitor, now time.Time) error {
	issueID, err := identity.New()
	if err != nil {
		return fmt.Errorf("generate attention issue ID: %w", err)
	}
	notificationID, err := identity.New()
	if err != nil {
		return fmt.Errorf("generate notification ID: %w", err)
	}
	revisionID, err := identity.New()
	if err != nil {
		return fmt.Errorf("generate attention revision ID: %w", err)
	}
	facts := buildReasonFacts(issueID, m.point, m.evaluation)
	reasonJSON, err := facts.JSON()
	if err != nil {
		return err
	}
	reason := reasonText(m.point, m.evaluation)
	action := recommendedAction(m.point)
	var subject *string
	if m.point.Subject.Kind == tracking.SubjectTrackedItem {
		id := m.point.Subject.ID
		subject = &id
	}

	found := foundation.NewRepository(tx)
	summary := "Open attention issue and initial notification"
	if _, err := found.CreateRevision(ctx, revisionID, foundation.RevisionOriginSystem, &summary, now); err != nil {
		return err
	}
	if err := found.CreateEntity(ctx, issueID, entityTypeAttention, now); err != nil {
		return err
	}
	if err := found.CreateEntity(ctx, notificationID, entityTypeNotification, now); err != nil {
		return err
	}
	severity := SeverityWarning
	if m.point.Kind == tracking.MonitorDeadline {
		severity = SeverityCritical
	}
	issue := &AttentionIssue{
		ID: issueID, WorkItemID: &m.point.WorkItemID, SubjectTrackedItemID: subject, MonitorPointID: &m.point.ID,
		Type: IssueMonitorOverdueUnmet, Severity: severity, State: IssueOpen, Reason: reason, ReasonJSON: reasonJSON,
		RecommendedAction: &action, OpenedAt: now, DedupeKey: dedupeKey(m.point), LastRevisionID: revisionID, UpdatedAt: now,
	}
	if err := repo.createIssue(ctx, issue); err != nil {
		return err
	}
	notification := &Notification{
		ID: notificationID, AttentionIssueID: issueID, WorkItemID: &m.point.WorkItemID, SubjectTrackedItemID: subject,
		Title: m.point.Title, Message: reason, ReasonSnapshotJSON: reasonJSON, State: DeliveryPending, Channel: ChannelInApp,
		DedupeKey: initialNotificationKey(issueID), LastRevisionID: revisionID, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.createNotification(ctx, notification); err != nil {
		return err
	}
	openedEvent, err := recordAttentionOpened(ctx, tx, issue, now)
	if err != nil {
		return err
	}
	return recordNotificationCreated(ctx, tx, notification, issueCorrelation(*issue), openedEvent.ID, now)
}

func (s *Service) resolveInTx(ctx context.Context, tx *sql.Tx, repo *Repository, issue AttentionIssue, now time.Time) error {
	revisionID, err := identity.New()
	if err != nil {
		return fmt.Errorf("generate attention resolve revision ID: %w", err)
	}
	summary := "Resolve attention issue"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginSystem, &summary, now); err != nil {
		return err
	}
	if err := repo.resolveIssue(ctx, issue.ID, revisionID, now); err != nil {
		return err
	}
	return recordAttentionResolved(ctx, tx, issue, revisionID, now)
}

// SnoozeAttentionIssue sets snoozed_until on an OPEN issue. Snooze changes only
// that timestamp: it does not resolve the issue, touch Notifications, or change
// domain truth. Re-snoozing to the same instant is a no-op.
func (s *Service) SnoozeAttentionIssue(ctx context.Context, input SnoozeInput) (*AttentionIssue, error) {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return nil, fmt.Errorf("%w: attention issue ID must not be empty", ErrInvalidInput)
	}
	now := s.clock()
	until := input.Until.UTC()
	if input.Until.IsZero() || !until.After(now) {
		return nil, fmt.Errorf("%w: snooze time must be in the future", ErrInvalidInput)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin snooze transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	repo := NewRepository(tx)
	issue, err := repo.getIssue(ctx, id)
	if err != nil {
		return nil, err
	}
	if issue.State != IssueOpen {
		return nil, fmt.Errorf("%w: %s", ErrIssueNotOpen, id)
	}
	if issue.SnoozedUntil != nil && issue.SnoozedUntil.Equal(until.Truncate(time.Nanosecond)) {
		return issue, nil
	}
	revisionID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate snooze revision ID: %w", err)
	}
	summary := "Snooze attention issue"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return nil, err
	}
	if err := repo.snoozeIssue(ctx, id, until, revisionID, now); err != nil {
		return nil, err
	}
	if err := recordAttentionSnoozed(ctx, tx, issue, until, revisionID, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit snooze transaction: %w", err)
	}
	issue.SnoozedUntil, issue.LastRevisionID, issue.UpdatedAt = &until, revisionID, now
	return issue, nil
}

// MarkNotificationRead sets read_at once. It changes nothing else: not the
// delivery state, the linked AttentionIssue, snooze, or any domain truth.
func (s *Service) MarkNotificationRead(ctx context.Context, id string) (*Notification, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: notification ID must not be empty", ErrInvalidInput)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin mark read transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	repo := NewRepository(tx)
	notification, err := repo.getNotification(ctx, id)
	if err != nil {
		return nil, err
	}
	// Only a delivered (SENT) Notification is user-readable; PENDING and
	// CANCELLED are never product-visible, so reading them is rejected.
	if notification.State != DeliverySent {
		return nil, fmt.Errorf("%w: %s is %s", ErrNotDelivered, id, notification.State)
	}
	if notification.ReadAt != nil {
		return notification, nil
	}
	now := s.clock()
	revisionID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate mark read revision ID: %w", err)
	}
	summary := "Mark notification read"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return nil, err
	}
	if err := repo.markRead(ctx, id, revisionID, now); err != nil {
		return nil, err
	}
	issue, err := repo.getIssue(ctx, notification.AttentionIssueID)
	if err != nil {
		return nil, err
	}
	if err := recordNotificationRead(ctx, tx, notification, revisionID, now, *issue); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit mark read transaction: %w", err)
	}
	notification.ReadAt, notification.LastRevisionID, notification.UpdatedAt = &now, revisionID, now
	return notification, nil
}

// MarkNotificationPresented sets presented_at once when an in-app alert is shown.
// It is idempotent (if presented_at is already set, it returns cleanly without a new Revision or History event).
func (s *Service) MarkNotificationPresented(ctx context.Context, id string) (*Notification, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: notification ID must not be empty", ErrInvalidInput)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin mark presented transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	repo := NewRepository(tx)
	notification, err := repo.getNotification(ctx, id)
	if err != nil {
		return nil, err
	}
	if notification.PresentedAt != nil {
		return notification, nil
	}
	now := s.clock()
	revisionID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate mark presented revision ID: %w", err)
	}
	summary := "Notification presented in-app"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginSystem, &summary, now); err != nil {
		return nil, err
	}
	changed, err := repo.markPresented(ctx, id, revisionID, now)
	if err != nil {
		return nil, err
	}
	if !changed {
		return notification, nil
	}
	issue, err := repo.getIssue(ctx, notification.AttentionIssueID)
	if err != nil {
		return nil, err
	}
	if err := recordNotificationPresented(ctx, tx, notification, revisionID, now, *issue); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit mark presented transaction: %w", err)
	}
	notification.PresentedAt, notification.LastRevisionID, notification.UpdatedAt = &now, revisionID, now
	return notification, nil
}

// ResolveByPlannerItemTx resolves any open AttentionIssues linked to a planner item
// and appends canonical AttentionResolved history events.
func (s *Service) ResolveByPlannerItemTx(ctx context.Context, tx *sql.Tx, plannerItemID, revisionID string, now time.Time) error {
	repo := NewRepository(tx)
	open, err := repo.listOpenIssuesByPlannerItem(ctx, plannerItemID)
	if err != nil {
		return err
	}
	for _, issue := range open {
		if err := repo.resolveIssue(ctx, issue.ID, revisionID, now); err != nil {
			return err
		}
		if err := recordAttentionResolved(ctx, tx, issue, revisionID, now); err != nil {
			return err
		}
	}
	return nil
}

// ResolveByPlannerItem resolves any open AttentionIssues linked to a planner item
// within a new user-initiated transaction.
func (s *Service) ResolveByPlannerItem(ctx context.Context, plannerItemID string) error {
	plannerItemID = strings.TrimSpace(plannerItemID)
	if plannerItemID == "" {
		return nil
	}
	now := s.clock()
	revisionID, err := identity.New()
	if err != nil {
		return fmt.Errorf("generate revision ID: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin resolve by planner item tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	summary := "Resolve attention issues for planner item"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return err
	}
	if err := s.ResolveByPlannerItemTx(ctx, tx, plannerItemID, revisionID, now); err != nil {
		return err
	}
	return tx.Commit()
}

type OpenTodoReminderAttentionInput struct {
	PlannerItemID string
	WorkItemID    *string
	Title         string
	OffsetSeconds int
	DueAt         time.Time
}

func formatOffset(seconds int) string {
	if seconds == 0 {
		return "due now"
	}
	if seconds > 0 {
		if seconds%86400 == 0 {
			d := seconds / 86400
			if d == 1 {
				return "due in 1 day"
			}
			return fmt.Sprintf("due in %d days", d)
		}
		if seconds%3600 == 0 {
			h := seconds / 3600
			if h == 1 {
				return "due in 1 hour"
			}
			return fmt.Sprintf("due in %d hours", h)
		}
		if seconds%60 == 0 {
			m := seconds / 60
			if m == 1 {
				return "due in 1 minute"
			}
			return fmt.Sprintf("due in %d minutes", m)
		}
		return fmt.Sprintf("due in %d seconds", seconds)
	}
	past := -seconds
	if past%86400 == 0 {
		d := past / 86400
		if d == 1 {
			return "overdue by 1 day"
		}
		return fmt.Sprintf("overdue by %d days", d)
	}
	if past%3600 == 0 {
		h := past / 3600
		if h == 1 {
			return "overdue by 1 hour"
		}
		return fmt.Sprintf("overdue by %d hours", h)
	}
	if past%60 == 0 {
		m := past / 60
		if m == 1 {
			return "overdue by 1 minute"
		}
		return fmt.Sprintf("overdue by %d minutes", m)
	}
	return fmt.Sprintf("overdue by %d seconds", past)
}

// OpenTodoReminderAttention wraps OpenTodoReminderAttentionTx in a new transaction.
func (s *Service) OpenTodoReminderAttention(ctx context.Context, input OpenTodoReminderAttentionInput, now time.Time) (*AttentionIssue, *Notification, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin open todo reminder attention tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	issue, notif, err := s.OpenTodoReminderAttentionTx(ctx, tx, input, now)
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit open todo reminder attention tx: %w", err)
	}
	return issue, notif, nil
}

// OpenTodoReminderAttentionTx atomically creates or updates the single OPEN AttentionIssue for the Todo,
// creates the SYSTEM Revision, and creates the threshold-specific PENDING Notification.
func (s *Service) OpenTodoReminderAttentionTx(ctx context.Context, tx *sql.Tx, input OpenTodoReminderAttentionInput, now time.Time) (*AttentionIssue, *Notification, error) {
	notificationID, err := identity.New()
	if err != nil {
		return nil, nil, fmt.Errorf("generate notification ID: %w", err)
	}
	revisionID, err := identity.New()
	if err != nil {
		return nil, nil, fmt.Errorf("generate attention revision ID: %w", err)
	}

	found := foundation.NewRepository(tx)
	summary := fmt.Sprintf("Todo reminder threshold reached (%ds)", input.OffsetSeconds)
	if _, err := found.CreateRevision(ctx, revisionID, foundation.RevisionOriginSystem, &summary, now); err != nil {
		return nil, nil, err
	}
	if err := found.CreateEntity(ctx, notificationID, entityTypeNotification, now); err != nil {
		return nil, nil, err
	}

	offsetDesc := formatOffset(input.OffsetSeconds)
	reason := fmt.Sprintf("Todo reminder (%s): %s", offsetDesc, input.Title)
	action := "Open Planner to view or complete this task"
	factsMap := map[string]any{
		"version":         1,
		"planner_item_id": input.PlannerItemID,
		"title":           input.Title,
		"offset_seconds":  input.OffsetSeconds,
		"due_at":          input.DueAt.UTC().Format(time.RFC3339Nano),
	}
	factsJSON, err := json.Marshal(factsMap)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal reason facts: %w", err)
	}

	repo := NewRepository(tx)

	// Single Attention Issue per Todo: check if an issue is already open for this Planner item.
	openIssues, err := repo.listOpenIssuesByPlannerItem(ctx, input.PlannerItemID)
	if err != nil {
		return nil, nil, fmt.Errorf("list open attention issues for planner item: %w", err)
	}

	var targetIssue *AttentionIssue
	for _, oi := range openIssues {
		if oi.Type == IssueTodoReminderDue && oi.State == IssueOpen {
			existing := oi
			targetIssue = &existing
			break
		}
	}

	var causeEventID string
	if targetIssue != nil {
		// Update existing open attention issue to latest urgency.
		if err := repo.updateIssueReason(ctx, targetIssue.ID, reason, string(factsJSON), revisionID, now); err != nil {
			return nil, nil, err
		}
		targetIssue.Reason = reason
		targetIssue.ReasonJSON = string(factsJSON)
		targetIssue.LastRevisionID = revisionID
		targetIssue.UpdatedAt = now
	} else {
		// Create new AttentionIssue.
		issueID, err := identity.New()
		if err != nil {
			return nil, nil, fmt.Errorf("generate attention issue ID: %w", err)
		}
		if err := found.CreateEntity(ctx, issueID, entityTypeAttention, now); err != nil {
			return nil, nil, err
		}

		targetIssue = &AttentionIssue{
			ID:                issueID,
			PlannerItemID:     &input.PlannerItemID,
			WorkItemID:        input.WorkItemID,
			Type:              IssueTodoReminderDue,
			Severity:          SeverityWarning,
			State:             IssueOpen,
			Reason:            reason,
			ReasonJSON:        string(factsJSON),
			RecommendedAction: &action,
			OpenedAt:          now,
			DedupeKey:         fmt.Sprintf("todo-reminder:%s", input.PlannerItemID),
			LastRevisionID:    revisionID,
			UpdatedAt:         now,
		}
		if err := repo.createIssue(ctx, targetIssue); err != nil {
			return nil, nil, err
		}

		openedEvent, err := recordAttentionOpened(ctx, tx, targetIssue, now)
		if err != nil {
			return nil, nil, err
		}
		causeEventID = openedEvent.ID
	}

	notification := &Notification{
		ID:                 notificationID,
		AttentionIssueID:   targetIssue.ID,
		PlannerItemID:      &input.PlannerItemID,
		WorkItemID:         input.WorkItemID,
		Title:              fmt.Sprintf("Reminder: %s", input.Title),
		Message:            reason,
		ReasonSnapshotJSON: string(factsJSON),
		State:              DeliveryPending,
		Channel:            ChannelInApp,
		DedupeKey:          fmt.Sprintf("todo-reminder-notif:%s:%d", input.PlannerItemID, input.OffsetSeconds),
		LastRevisionID:     revisionID,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := repo.createNotification(ctx, notification); err != nil {
		return nil, nil, err
	}

	if err := recordNotificationCreated(ctx, tx, notification, issueCorrelation(*targetIssue), causeEventID, now); err != nil {
		return nil, nil, err
	}

	return targetIssue, notification, nil
}

func (s *Service) GetAttentionIssue(ctx context.Context, id string) (*AttentionIssue, error) {
	return NewRepository(s.db).getIssue(ctx, strings.TrimSpace(id))
}

func (s *Service) GetNotification(ctx context.Context, id string) (*Notification, error) {
	return NewRepository(s.db).getNotification(ctx, strings.TrimSpace(id))
}

// ListAttentionIssues returns current issues (any lifecycle), newest first.
func (s *Service) ListAttentionIssues(ctx context.Context) ([]AttentionIssue, error) {
	return NewRepository(s.db).ListIssues(ctx)
}

// ListIssuesForWorkItem returns all AttentionIssues whose WorkItemID equals workItemID.
func (s *Service) ListIssuesForWorkItem(ctx context.Context, workItemID string) ([]AttentionIssue, error) {
	workItemID = strings.TrimSpace(workItemID)
	if workItemID == "" {
		return nil, fmt.Errorf("%w: work item ID is required", ErrInvalidInput)
	}
	issues, err := s.ListAttentionIssues(ctx)
	if err != nil {
		return nil, err
	}
	var result []AttentionIssue
	for _, issue := range issues {
		if issue.WorkItemID != nil && *issue.WorkItemID == workItemID {
			result = append(result, issue)
		}
	}
	return result, nil
}

// ListNotificationViews returns every Notification with its linked issue,
// newest first, so callers derive Needs Action / Unread / Snoozed / Resolved
// (see AttentionIssue.DispositionAt and Notification.ReadAt) without re-reading.
func (s *Service) ListNotificationViews(ctx context.Context) ([]NotificationView, error) {
	repo := NewRepository(s.db)
	notifications, err := repo.ListNotifications(ctx)
	if err != nil {
		return nil, err
	}
	issues, err := repo.ListIssues(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]AttentionIssue, len(issues))
	for _, issue := range issues {
		byID[issue.ID] = issue
	}
	views := make([]NotificationView, 0, len(notifications))
	for _, n := range notifications {
		issue, ok := byID[n.AttentionIssueID]
		if !ok {
			return nil, fmt.Errorf("notification %s references unavailable attention issue %s", n.ID, n.AttentionIssueID)
		}
		views = append(views, NotificationView{Notification: n, Issue: issue})
	}
	return views, nil
}

// DispatchPendingNotifications is the deterministic IN_APP dispatcher (no AI).
// For every PENDING Notification, against its linked AttentionIssue:
//   - issue RESOLVED           → CANCELLED (obsolete before first delivery; never SENT);
//   - issue OPEN, snoozed now  → stays PENDING (no Revision, no churn);
//   - issue OPEN, not snoozed  → SENT with sent_at = now.
//
// Each transition is conditional on state = 'PENDING' and creates one SYSTEM
// Revision; SENT/CANCELLED rows are never touched again, so repeated or
// concurrent dispatch converges. A lost conditional race rolls the transaction
// back and re-evaluates. Notifications are never deleted.
func (s *Service) DispatchPendingNotifications(ctx context.Context, now time.Time) (DispatchResult, error) {
	now = now.UTC()
	var lastErr error
	for attempt := 0; attempt < maxReconcileAttempts; attempt++ {
		result, err := s.dispatchOnce(ctx, now)
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, errConflict) {
			return DispatchResult{}, err
		}
		lastErr = err
	}
	return DispatchResult{}, lastErr
}

func (s *Service) dispatchOnce(ctx context.Context, now time.Time) (DispatchResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return DispatchResult{}, fmt.Errorf("begin dispatch transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	repo := NewRepository(tx)
	pending, err := repo.listNotificationsByState(ctx, DeliveryPending)
	if err != nil {
		return DispatchResult{}, err
	}
	var result DispatchResult
	for _, n := range pending {
		issue, err := repo.getIssue(ctx, n.AttentionIssueID)
		if err != nil {
			return DispatchResult{}, err
		}
		if issue.State == IssueOpen && issue.SnoozedUntil != nil && issue.SnoozedUntil.After(now) {
			result.Deferred++
			continue
		}
		revisionID, err := identity.New()
		if err != nil {
			return DispatchResult{}, fmt.Errorf("generate dispatch revision ID: %w", err)
		}
		if issue.State == IssueResolved {
			summary := "Cancel obsolete pending notification"
			if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginSystem, &summary, now); err != nil {
				return DispatchResult{}, err
			}
			if err := repo.markCancelled(ctx, n.ID, revisionID, now); err != nil {
				return DispatchResult{}, err
			}
			if err := recordNotificationCancelled(ctx, tx, n, *issue, revisionID, now); err != nil {
				return DispatchResult{}, err
			}
			result.Cancelled++
			continue
		}
		summary := "Deliver notification in-app"
		if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginSystem, &summary, now); err != nil {
			return DispatchResult{}, err
		}
		if err := repo.markSent(ctx, n.ID, revisionID, now); err != nil {
			return DispatchResult{}, err
		}
		if err := recordNotificationSent(ctx, tx, n, *issue, revisionID, now); err != nil {
			return DispatchResult{}, err
		}
		result.Sent++
	}
	if err := tx.Commit(); err != nil {
		return DispatchResult{}, fmt.Errorf("commit dispatch transaction: %w", err)
	}
	return result, nil
}

// NotificationCenter returns every delivered (SENT) Notification with its
// linked issue and the disposition derived at the service clock. PENDING and
// CANCELLED Notifications are durable backend truth but never appear here.
func (s *Service) NotificationCenter(ctx context.Context) (NotificationCenter, error) {
	now := s.clock()
	views, err := s.ListNotificationViews(ctx)
	if err != nil {
		return NotificationCenter{}, err
	}
	center := NotificationCenter{At: now, Items: []CenterItem{}}
	for _, view := range views {
		if view.Notification.State != DeliverySent {
			continue
		}
		center.Items = append(center.Items, CenterItem{View: view, Disposition: view.Issue.DispositionAt(now)})
	}
	return center, nil
}
