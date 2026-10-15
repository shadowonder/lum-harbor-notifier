package planner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
	"lum-harbor-notifier/internal/foundation"
	"lum-harbor-notifier/internal/identity"
	"lum-harbor-notifier/internal/temporal"
)

var ErrInvalidTimeRange = errors.New(
	"planner time range end must be after start",
)

// ErrExternalAuthority indicates that a Planner Item is owned by an
// external system and therefore cannot be mutated as a local Todo.
var ErrExternalAuthority = errors.New(
	"planner item is externally authoritative",
)

// ErrNotExternalAuthority indicates the opposite direction from
// ErrExternalAuthority: a deterministic external-sync mutation targeted a
// Planner Item that is not EXTERNAL (e.g. a LOCAL Todo), which it may never
// modify.
var ErrNotExternalAuthority = errors.New(
	"planner item is not externally authoritative",
)

// ErrInvalidWorkflow indicates that a specified workflow reference does not exist or is invalid.
var ErrInvalidWorkflow = errors.New("planner: referenced workflow does not exist")

// ErrInvalidWorkflowStep indicates that a specified workflow step reference does not exist or is invalid.
var ErrInvalidWorkflowStep = errors.New("planner: referenced workflow step does not exist")

// ErrInvalidWorkItem indicates that a specified work item reference does not exist or is invalid.
var ErrInvalidWorkItem = errors.New("planner: referenced work item does not exist")

// ErrInvalidTrackedItem indicates that a typed tracking-derived Todo relation
// does not identify a current TrackedItem or conflicts with its WorkItem /
// Workflow provenance.
var ErrInvalidTrackedItem = errors.New("planner: referenced tracked item does not exist or is incompatible")

// CreateLocalTodoInput contains only the information a caller is
// allowed to provide when creating a normal local Todo.
//
// is deliberately NOT accepted from the caller.
// The Service owns those invariants.
type CreateLocalTodoInput struct {
	Title            string
	Details          *string
	ScheduledAt      *time.Time
	DueAt            *time.Time
	ScheduledDate    *temporal.LocalDate
	DueDate          *temporal.LocalDate
	WorkItemID       *string
	WorkflowID       *string
	WorkflowStepID   *string
	TrackedItemID    *string
	SourceRevisionID *string
}

// TrackingTodoDueTarget is the bounded Planner projection used to derive
// TrackedItem health. DueAt is always an exact UTC instant; date-only Todos
// are normalized to the final second of their UTC calendar date, matching the
// existing reminder evaluator.
type TrackingTodoDueTarget struct {
	PlannerItemID string
	TrackedItemID string
	WorkItemID    string
	DueAt         time.Time
}

type TodoCompletionHook func(ctx context.Context, tx *sql.Tx, plannerItemID, revisionID string, now time.Time) error

// Service owns Planner use cases and their transaction boundaries.
//
// The DB-scoped repository is reused for reads. db is kept separately because
// mutations must begin a transaction and construct repositories bound to that
// transaction rather than reuse the process-scoped repository.
type Service struct {
	db              *sql.DB
	repository      *Repository
	completionHooks []TodoCompletionHook

	// beforeExternalCreate, when set, is invoked immediately after
	// ReconcileExternalPlannerItem resolves an identity-miss and immediately
	// before it attempts CreateExternalPlannerItem. It exists only so tests
	// can deterministically inject a create race (a concurrent caller
	// creating the same external identity in that exact window) without a
	// real second goroutine or a distributed lock (M14 Slice 2, moved here
	// from internal/plannersync.Reconciler's identical M13 Slice 2B hook). It
	// is nil in every production path.
	beforeExternalCreate func()
}

func (s *Service) AddCompletionHook(hook TodoCompletionHook) {
	s.completionHooks = append(s.completionHooks, hook)
}

// NewService creates a Planner service with process-scoped read dependencies.
//
// db is retained exclusively so mutation use cases can open transactions.
// repository must be bound to that same *sql.DB and is reused by read use
// cases. Mutation use cases deliberately create new repositories bound to the
// active *sql.Tx so every read and write in a mutation shares one transaction.
func NewService(db *sql.DB, repository *Repository) *Service {
	return &Service{
		db:         db,
		repository: repository,
	}
}

// CreateLocalTodo creates a user-owned Planner Todo.
// This is the first complete Planner mutation
//
// If any step fails, the transaction is rolled back and no partial
// Planner state should remain in SQLite.
func (s *Service) CreateLocalTodo(ctx context.Context, input CreateLocalTodoInput) (*PlannerItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create planner todo transaction: %w", err)
	}

	// Rollback is safe even after Commit; database/sql will simply return
	// sql.ErrTxDone, which we intentionally ignore here.
	//
	// This pattern guarantees every early-return error path attempts to
	// release the transaction.
	defer func() {
		_ = tx.Rollback()
	}()

	item, err := s.CreateLocalTodoTx(ctx, tx, input)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf(
			"commit create planner todo transaction: %w",
			err,
		)
	}

	return item, nil
}

// CreateLocalTodoTx executes the same local Todo creation use case within a
// caller-provided transaction, so a Proposal Apply can create the Foundation
// Entity, Revision and PlannerItem atomically with its own CAS/History
// bookkeeping - and so that no entity, revision, or PlannerItem row exists
// merely because a pending Proposal was created (Proposal-first boundary).
func (s *Service) CreateLocalTodoTx(ctx context.Context, tx *sql.Tx, input CreateLocalTodoInput) (*PlannerItem, error) {
	if err := validateCreateLocalTodo(input); err != nil {
		return nil, err
	}

	// Validate foreign key references before persistence to ensure domain errors rather than raw SQLite FK constraint failures.
	if input.WorkflowID != nil && strings.TrimSpace(*input.WorkflowID) != "" {
		wfID := strings.TrimSpace(*input.WorkflowID)
		var exists int
		err := tx.QueryRowContext(ctx, "SELECT 1 FROM workflows WHERE id = ?", wfID).Scan(&exists)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("%w: workflow %q not found", ErrInvalidWorkflow, wfID)
			}
			return nil, fmt.Errorf("check workflow existence: %w", err)
		}
	}
	if input.WorkflowStepID != nil && strings.TrimSpace(*input.WorkflowStepID) != "" {
		stepID := strings.TrimSpace(*input.WorkflowStepID)
		var exists int
		err := tx.QueryRowContext(ctx, "SELECT 1 FROM workflow_steps WHERE id = ?", stepID).Scan(&exists)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("%w: workflow step %q not found", ErrInvalidWorkflowStep, stepID)
			}
			return nil, fmt.Errorf("check workflow step existence: %w", err)
		}
	}
	if input.WorkItemID != nil && strings.TrimSpace(*input.WorkItemID) != "" {
		wiID := strings.TrimSpace(*input.WorkItemID)
		var exists int
		err := tx.QueryRowContext(ctx, "SELECT 1 FROM work_items WHERE entity_id = ?", wiID).Scan(&exists)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("%w: work item %q not found", ErrInvalidWorkItem, wiID)
			}
			return nil, fmt.Errorf("check work item existence: %w", err)
		}
	}
	if err := validateTrackedItemRelation(ctx, tx, input); err != nil {
		return nil, err
	}

	plannerId, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate planner item ID: %w", err)
	}

	revisionId, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate revision item ID: %w", err)
	}

	// Capture "now" once so Revision, Entity and PlannerItem all represent
	// the same logical mutation time.
	now := time.Now().UTC()

	foundationRepo := foundation.NewRepository(tx)

	summary := "Create local planner todo"

	if _, err := foundationRepo.CreateRevision(
		ctx,
		revisionId,
		foundation.RevisionOriginUser,
		&summary,
		now,
	); err != nil {
		return nil, err
	}

	if err := foundationRepo.CreateEntity(
		ctx,
		plannerId,
		"PLANNER_ITEM",
		now,
	); err != nil {
		return nil, err
	}

	item := &PlannerItem{
		ID:              plannerId,
		Authority:       AuthorityLocal,
		Type:            ItemTypeTodo,
		WorkItemID:      input.WorkItemID,
		WorkflowID:      input.WorkflowID,
		WorkflowStepID:  input.WorkflowStepID,
		RelatedEntityID: trimmedOptional(input.TrackedItemID),
		Title:           strings.TrimSpace(input.Title),
		Details:         input.Details,
		Status:          StatusOpen,
		ScheduledAt:     input.ScheduledAt,
		DueAt:           input.DueAt,
		ScheduledDate:   input.ScheduledDate,
		DueDate:         input.DueDate,
		ContextStatus:   ContextUnknown,
		LastRevisionID:  revisionId,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	plannerRepo := NewRepository(tx)
	if err := plannerRepo.Create(ctx, item); err != nil {
		if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
			return nil, fmt.Errorf("%w: %v", ErrInvalidWorkflow, err)
		}
		return nil, err
	}
	if input.SourceRevisionID != nil && strings.TrimSpace(*input.SourceRevisionID) != "" {
		if err := foundationRepo.LinkRevisionSource(ctx, revisionId, *input.SourceRevisionID, "PRIMARY_SOURCE"); err != nil {
			return nil, err
		}
		atStr := codec.FormatTime(now)
		_ = dbgen.New(tx).CompleteSourceProcessingWithRevision(ctx, dbgen.CompleteSourceProcessingWithRevisionParams{
			SourceRevisionID: *input.SourceRevisionID,
			RevisionID:       &revisionId,
			CompletedAt:      &atStr,
			UpdatedAt:        atStr,
		})
	}
	if err := recordPlannerCreated(ctx, tx, item, input.SourceRevisionID); err != nil {
		return nil, err
	}

	return item, nil
}

func validateTrackedItemRelation(ctx context.Context, tx *sql.Tx, input CreateLocalTodoInput) error {
	if input.TrackedItemID == nil || strings.TrimSpace(*input.TrackedItemID) == "" {
		return nil
	}
	trackedItemID := strings.TrimSpace(*input.TrackedItemID)
	var workItemID, workflowID, workflowDefinitionID string
	err := tx.QueryRowContext(ctx, `
SELECT ti.work_item_id, ti.workflow_id, w.workflow_definition_id
FROM tracked_items ti
JOIN entities e ON e.id = ti.entity_id
JOIN workflows w ON w.id = ti.workflow_id
WHERE ti.entity_id = ? AND e.entity_type = 'TRACKED_ITEM' AND e.archived_at IS NULL`, trackedItemID).
		Scan(&workItemID, &workflowID, &workflowDefinitionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %q", ErrInvalidTrackedItem, trackedItemID)
		}
		return fmt.Errorf("validate tracked item %q: %w", trackedItemID, err)
	}
	if input.WorkItemID != nil && strings.TrimSpace(*input.WorkItemID) != workItemID {
		return fmt.Errorf("%w: tracked item %q belongs to work item %q", ErrInvalidTrackedItem, trackedItemID, workItemID)
	}
	if input.WorkflowID != nil && strings.TrimSpace(*input.WorkflowID) != workflowID {
		return fmt.Errorf("%w: tracked item %q belongs to workflow %q", ErrInvalidTrackedItem, trackedItemID, workflowID)
	}
	if input.WorkflowStepID != nil && strings.TrimSpace(*input.WorkflowStepID) != "" {
		var stepDefinitionID string
		err := tx.QueryRowContext(ctx, "SELECT workflow_definition_id FROM workflow_steps WHERE id = ?", strings.TrimSpace(*input.WorkflowStepID)).Scan(&stepDefinitionID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: workflow step %q not found", ErrInvalidWorkflowStep, strings.TrimSpace(*input.WorkflowStepID))
			}
			return fmt.Errorf("check tracked item workflow step: %w", err)
		}
		if stepDefinitionID != workflowDefinitionID {
			return fmt.Errorf("%w: workflow step %q is not in tracked item %q workflow", ErrInvalidTrackedItem, strings.TrimSpace(*input.WorkflowStepID), trackedItemID)
		}
	}
	return nil
}

func trimmedOptional(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	return &trimmed
}

// validateExactOrDateOnly enforces the mutual-exclusion rule shared by every
// exact-instant-or-date-only Planner field pair: at most one of exact and
// dateOnly may be set, and a set dateOnly must itself be a valid calendar
// date. fieldName is the snake_case fragment shared by both column names
// (e.g. "due" for due_at/due_date), used only to produce a precise message.
//
// LOCAL creation's scheduled/due pairs and EXTERNAL create/sync's due pair
// all call this so the rule cannot drift between authority paths.
func validateExactOrDateOnly(fieldName string, exact *time.Time, dateOnly *temporal.LocalDate) error {
	if exact != nil && dateOnly != nil {
		return fmt.Errorf("planner item cannot have both %s_at and %s_date", fieldName, fieldName)
	}
	if dateOnly != nil && !dateOnly.Valid() {
		return fmt.Errorf("invalid %s date %q", fieldName, *dateOnly)
	}
	return nil
}

// validateExternalNativeState validates the fields every EXTERNAL Planner
// mutation (create or sync refresh) must satisfy: a non-empty title, the due
// exact/date-only mutual exclusion, and status/completed_at consistency. It
// returns the trimmed title so callers store the exact value they validated.
//
// completedAt is never invented here: it must already be a real,
// authoritative external completion timestamp (or nil for OPEN) supplied by
// the caller. There is no scheduled_at/scheduled_date validation because
// external sync never sets those Lum Harbor-only fields.
func validateExternalNativeState(title string, dueAt *time.Time, dueDate *temporal.LocalDate, status Status, completedAt *time.Time) (string, error) {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		return "", errors.New("planner item title must not be empty")
	}
	if err := validateExactOrDateOnly("due", dueAt, dueDate); err != nil {
		return "", err
	}
	if !status.Valid() {
		return "", fmt.Errorf("invalid planner status %q", status)
	}
	switch status {
	case StatusCompleted:
		if completedAt == nil {
			return "", errors.New("planner item completed status requires a completion timestamp")
		}
	case StatusOpen:
		if completedAt != nil {
			return "", errors.New("planner item open status must not have a completion timestamp")
		}
	}
	return trimmed, nil
}

// validateCreateLocalTodo verifies only the invariants that must be true
// before a local Todo can be created.
//
// More sophisticated context analysis belongs to later Planner/Rule
// Engine work and should not be mixed into basic creation validation.
func validateCreateLocalTodo(input CreateLocalTodoInput) error {
	if strings.TrimSpace(input.Title) == "" {
		return errors.New("planner item title must not be empty")
	}
	if err := validateExactOrDateOnly("scheduled", input.ScheduledAt, input.ScheduledDate); err != nil {
		return err
	}
	if err := validateExactOrDateOnly("due", input.DueAt, input.DueDate); err != nil {
		return err
	}

	// Exact timestamps are directly comparable.
	if input.ScheduledAt != nil &&
		input.DueAt != nil &&
		input.DueAt.Before(*input.ScheduledAt) {

		return errors.New("planner item due time cannot be before scheduled time")
	}

	// YYYY-MM-DD has fixed-width lexical ordering, so validated LocalDate
	// values can be compared safely as strings.
	if input.ScheduledDate != nil &&
		input.DueDate != nil &&
		string(*input.DueDate) < string(*input.ScheduledDate) {

		return errors.New("planner item due date cannot be before scheduled date")
	}

	return nil
}

// CompleteLocalTodo marks a LOCAL Planner Todo as completed.
//
// The operation is transactional because completion creates a new
// Revision and changes authoritative Planner state as one logical
// mutation.
func (s *Service) CompleteLocalTodo(ctx context.Context, id string) (*PlannerItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin complete planner todo transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback()
	}()

	revisionID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate completion revision ID: %w", err)
	}

	now := time.Now().UTC()
	completed, err := s.CompleteLocalTodoTx(ctx, tx, id, revisionID, now)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit complete planner todo transaction: %w", err)
	}

	return completed, nil
}

// CompleteLocalTodoTx executes Todo completion within an existing transaction,
// invoking any registered completion hooks (e.g. reminder satisfaction and attention resolution).
func (s *Service) CompleteLocalTodoTx(ctx context.Context, tx *sql.Tx, id string, revisionID string, now time.Time) (*PlannerItem, error) {
	plannerRepo := NewRepository(tx)
	foundationRepo := foundation.NewRepository(tx)

	current, err := plannerRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if current.Authority != AuthorityLocal {
		return nil, ErrExternalAuthority
	}

	// Completion is idempotent.
	// Repeated UI clicks must not create meaningless Revisions.
	if current.Status == StatusCompleted {
		return current, nil
	}

	summary := "Complete local planner todo"
	if _, err := foundationRepo.CreateRevision(
		ctx,
		revisionID,
		foundation.RevisionOriginUser,
		&summary,
		now,
	); err != nil {
		return nil, err
	}

	completed, err := plannerRepo.Complete(ctx, id, now, revisionID)
	if err != nil {
		return nil, err
	}
	if err := recordPlannerCompleted(ctx, tx, current, completed); err != nil {
		return nil, err
	}

	for _, hook := range s.completionHooks {
		if err := hook(ctx, tx, id, revisionID, now); err != nil {
			return nil, err
		}
	}

	return completed, nil
}

// ListCurrentOpen returns Planner Items that belong to the user's current action queue.
//
// today is a calendar date without timezone semantics.
// location is used only to determine where that local calendar day ends,
// so exact timestamps can be compared against tomorrow's local midnight.
func (s *Service) ListCurrentOpen(
	ctx context.Context,
	today temporal.LocalDate,
	location *time.Location,
) ([]PlannerItem, error) {

	if !today.Valid() {
		return nil, fmt.Errorf("invalid current local date %q", today)
	}

	if location == nil {
		return nil, fmt.Errorf("planner location must not be nil")
	}

	todayStart, err := time.ParseInLocation(temporal.LocalDateLayout, string(today), location)
	if err != nil {
		return nil, fmt.Errorf("parse current local date %q: %w", today, err)
	}

	tomorrowStart := todayStart.AddDate(0, 0, 1)

	items, err := s.repository.ListCurrentOpen(ctx, today, tomorrowStart)
	if err != nil {
		return nil, fmt.Errorf("list current open planner items: %w", err)
	}

	return items, nil
}

// ListOpenTrackingTodoDueTargets returns every usable due target for current
// open tracking-derived Todos in one set-based read.
func (s *Service) ListOpenTrackingTodoDueTargets(ctx context.Context) ([]TrackingTodoDueTarget, error) {
	rows, err := s.repository.queries.ListOpenTrackingTodoDueTargets(ctx)
	if err != nil {
		return nil, fmt.Errorf("list open tracking Todo due targets: %w", err)
	}

	targets := make([]TrackingTodoDueTarget, 0, len(rows))
	for _, row := range rows {
		var dueAt time.Time
		switch {
		case row.DueAt != nil && strings.TrimSpace(*row.DueAt) != "":
			dueAt, err = codec.ParseTime(*row.DueAt)
		case row.DueDate != nil && strings.TrimSpace(*row.DueDate) != "":
			var date temporal.LocalDate
			date, err = temporal.ParseLocalDate(*row.DueDate)
			if err == nil {
				dueAt, err = time.ParseInLocation(temporal.LocalDateLayout, string(date), time.UTC)
				dueAt = dueAt.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
			}
		default:
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("parse due target for planner item %s: %w", row.PlannerItemID, err)
		}
		targets = append(targets, TrackingTodoDueTarget{
			PlannerItemID: row.PlannerItemID,
			TrackedItemID: row.TrackedItemID,
			WorkItemID:    row.WorkItemID,
			DueAt:         dueAt.UTC(),
		})
	}
	return targets, nil
}

// ListOpenByTrackedItemID returns a bounded set of current local Todos linked
// to one exact TrackedItem. It is used only for Source interpretation context.
func (s *Service) ListOpenByTrackedItemID(ctx context.Context, trackedItemID string) ([]PlannerItem, error) {
	trackedItemID = strings.TrimSpace(trackedItemID)
	if trackedItemID == "" {
		return nil, fmt.Errorf("tracked item ID must not be empty")
	}
	rows, err := s.repository.queries.ListOpenPlannerItemsByTrackedItemID(ctx, &trackedItemID)
	if err != nil {
		return nil, fmt.Errorf("list open planner items for tracked item %s: %w", trackedItemID, err)
	}
	items := make([]PlannerItem, 0, len(rows))
	for _, row := range rows {
		item, err := plannerItemFromDB(row)
		if err != nil {
			return nil, fmt.Errorf("convert linked planner item %s: %w", row.EntityID, err)
		}
		items = append(items, *item)
	}
	return items, nil
}

// HasOpenTrackingTodoFromSourceRecord answers the Phase A consequence
// suppression question using durable Source lineage and current OPEN state.
func (s *Service) HasOpenTrackingTodoFromSourceRecord(ctx context.Context, trackedItemID, sourceRecordID string) (bool, error) {
	trackedItemID = strings.TrimSpace(trackedItemID)
	sourceRecordID = strings.TrimSpace(sourceRecordID)
	if trackedItemID == "" || sourceRecordID == "" {
		return false, fmt.Errorf("tracked item ID and source record ID must not be empty")
	}
	return s.repository.queries.HasOpenTrackingTodoFromSourceRecord(ctx, dbgen.HasOpenTrackingTodoFromSourceRecordParams{
		TrackedItemID: &trackedItemID, SourceRecordID: sourceRecordID,
	})
}

// ListCompletedByRange returns Planner Items completed within the
// requested exact time range.
func (s *Service) ListCompletedByRange(ctx context.Context, start time.Time, end time.Time) ([]PlannerItem, error) {

	if !end.After(start) {
		return nil, fmt.Errorf("completed planner range end must be after start")
	}

	items, err := s.repository.ListCompletedByRange(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("list completed planner items by range: %w", err)
	}

	return items, nil
}

// ListByRange returns Planner Items positioned within a visible Calendar
// range, including exact timestamps and date-only values. The injected
// DB-scoped Repository performs the read; no transaction is required because
// this use case coordinates no mutation.
func (s *Service) ListByRange(
	ctx context.Context,
	startAt time.Time,
	endAt time.Time,
	startDate temporal.LocalDate,
	endDate temporal.LocalDate,
) ([]PlannerItem, error) {
	if !endAt.After(startAt) {
		return nil, ErrInvalidTimeRange
	}

	items, err := s.repository.ListByRange(ctx, startAt, endAt, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("list planner items by range: %w", err)
	}
	return items, nil
}

// GetByID returns one current Planner Item by its exact entity ID (M15
// Slice 1 readcap entity.search exact-ID lookup).
func (s *Service) GetByID(ctx context.Context, id string) (*PlannerItem, error) {
	return s.repository.GetByID(ctx, id)
}

// GetExternalIdentity returns the AUTHORITATIVE external identity an
// EXTERNAL Planner Item was created with (read-only provenance).
func (s *Service) GetExternalIdentity(ctx context.Context, id string) (*foundation.ExternalRef, error) {
	return foundation.NewRepository(s.db).GetAuthoritativeExternalRefByEntity(ctx, id)
}

// SearchByTitle returns up to limit current Planner Items whose title
// contains query as a literal, case-insensitive substring (M15 Slice 1
// readcap entity.search capability).
func (s *Service) SearchByTitle(ctx context.Context, query string, limit int32) ([]PlannerItem, error) {
	return s.repository.SearchByTitle(ctx, query, limit)
}
