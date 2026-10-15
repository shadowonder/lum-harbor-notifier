package tracking

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
	"lum-harbor-notifier/internal/foundation"
	"lum-harbor-notifier/internal/identity"
	"lum-harbor-notifier/internal/workitem"

	"database/sql"
)

const entityTypeMonitorPoint = "MONITOR_POINT"

var (
	ErrMonitorPointNotFound = errors.New("monitor point not found")
	ErrInvalidMonitorState  = errors.New("invalid monitor point state transition")
	ErrFinalTargetExists    = errors.New("subject already has an active final target")
)

// MonitorKind is the domain-neutral MonitorPoint kind vocabulary.
type MonitorKind string

const (
	MonitorFinalTarget MonitorKind = "FINAL_TARGET"
	MonitorCheckpoint  MonitorKind = "CHECKPOINT"
	MonitorDeadline    MonitorKind = "DEADLINE"
	MonitorReview      MonitorKind = "REVIEW"
	MonitorCustom      MonitorKind = "CUSTOM"
)

func (k MonitorKind) Valid() bool {
	switch k {
	case MonitorFinalTarget, MonitorCheckpoint, MonitorDeadline, MonitorReview, MonitorCustom:
		return true
	}
	return false
}

// MonitorState is the authoritative MonitorPoint state. OVERDUE / AT_RISK are
// derived by a future evaluator from due_at + now + state; they are never stored.
type MonitorState string

const (
	MonitorActive    MonitorState = "ACTIVE"
	MonitorSatisfied MonitorState = "SATISFIED"
	MonitorCancelled MonitorState = "CANCELLED"
)

func (s MonitorState) Valid() bool {
	return s == MonitorActive || s == MonitorSatisfied || s == MonitorCancelled
}

// SubjectKind says what a MonitorPoint is about.
type SubjectKind string

const (
	SubjectWorkItem    SubjectKind = "WORK_ITEM"
	SubjectTrackedItem SubjectKind = "TRACKED_ITEM"
)

// ExpectationKind is the typed expected condition. It is fully determined by
// the subject: a WorkItem subject expects a WorkItem lifecycle; a TrackedItem
// subject expects one workflow step to be COMPLETED.
type ExpectationKind string

const (
	ExpectWorkItemLifecycle ExpectationKind = "WORK_ITEM_LIFECYCLE"
	ExpectStepCompleted     ExpectationKind = "TRACKED_ITEM_STEP_COMPLETED"
)

// MonitorSubject identifies the monitored entity. For SubjectWorkItem, ID is
// the owning WorkItem.
type MonitorSubject struct {
	Kind SubjectKind
	ID   string
}

// MonitorExpectation is the typed expected condition. Exactly the field that
// matches Kind is set.
type MonitorExpectation struct {
	Kind          ExpectationKind
	WorkItemState *workitem.LifecycleState // when Kind == ExpectWorkItemLifecycle
	StepID        *string                  // when Kind == ExpectStepCompleted
}

// MonitorPoint is an authoritative time-based expectation. It carries no
// evaluation, scheduling or notification state.
type MonitorPoint struct {
	ID             string
	WorkItemID     string
	Subject        MonitorSubject
	Kind           MonitorKind
	Title          string
	DueAt          time.Time
	Timezone       string
	Expected       MonitorExpectation
	State          MonitorState
	SatisfiedAt    *time.Time
	LastRevisionID string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CreateMonitorPointInput contains only product facts. SubjectTrackedItemID nil
// means the WorkItem itself is the subject. Exactly one of the expectation
// fields must match the subject: ExpectedWorkItemLifecycle for a WorkItem
// subject, ExpectedStepID for a TrackedItem subject.
type CreateMonitorPointInput struct {
	WorkItemID                string
	SubjectTrackedItemID      *string
	Kind                      MonitorKind
	Title                     string
	DueAt                     time.Time
	Timezone                  string
	ExpectedWorkItemLifecycle *workitem.LifecycleState
	ExpectedStepID            *string
}

// SetMonitorPointStateInput requests a state transition.
type SetMonitorPointStateInput struct {
	ID     string
	Target MonitorState
}

// CreateMonitorPoint atomically creates one USER Revision, the MonitorPoint
// Entity and the MonitorPoint. It never mutates the WorkItem, TrackedItems or
// step states, and never evaluates the expectation.
func (s *Service) CreateMonitorPoint(ctx context.Context, input CreateMonitorPointInput) (*MonitorPoint, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create monitor point transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	point, err := s.CreateMonitorPointTx(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create monitor point transaction: %w", err)
	}
	return point, nil
}

// CreateMonitorPointTx creates the MonitorPoint within a caller-provided transaction.
func (s *Service) CreateMonitorPointTx(ctx context.Context, tx *sql.Tx, input CreateMonitorPointInput) (*MonitorPoint, error) {
	workItemID := strings.TrimSpace(input.WorkItemID)
	title := strings.TrimSpace(input.Title)
	timezone := strings.TrimSpace(input.Timezone)
	subjectID := normalizeOptional(input.SubjectTrackedItemID)
	stepID := normalizeOptional(input.ExpectedStepID)
	if workItemID == "" {
		return nil, fmt.Errorf("%w: work item ID must not be empty", ErrInvalidInput)
	}
	if title == "" {
		return nil, fmt.Errorf("%w: title must not be blank", ErrInvalidInput)
	}
	if !input.Kind.Valid() {
		return nil, fmt.Errorf("%w: unsupported monitor kind %q", ErrInvalidInput, input.Kind)
	}
	if input.DueAt.IsZero() {
		return nil, fmt.Errorf("%w: due time is required", ErrInvalidInput)
	}
	if timezone == "" {
		return nil, fmt.Errorf("%w: timezone is required", ErrInvalidInput)
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return nil, fmt.Errorf("%w: unknown timezone %q", ErrInvalidInput, timezone)
	}

	// The expected condition must match the subject exactly.
	var expected MonitorExpectation
	subject := MonitorSubject{Kind: SubjectWorkItem, ID: workItemID}
	if subjectID == nil {
		if input.ExpectedWorkItemLifecycle == nil || stepID != nil {
			return nil, fmt.Errorf("%w: a work item subject requires exactly an expected work item lifecycle", ErrInvalidInput)
		}
		if !input.ExpectedWorkItemLifecycle.Valid() {
			return nil, fmt.Errorf("%w: unsupported expected lifecycle %q", ErrInvalidInput, *input.ExpectedWorkItemLifecycle)
		}
		lifecycle := *input.ExpectedWorkItemLifecycle
		expected = MonitorExpectation{Kind: ExpectWorkItemLifecycle, WorkItemState: &lifecycle}
	} else {
		if stepID == nil || input.ExpectedWorkItemLifecycle != nil {
			return nil, fmt.Errorf("%w: a tracked item subject requires exactly an expected workflow step", ErrInvalidInput)
		}
		subject = MonitorSubject{Kind: SubjectTrackedItem, ID: *subjectID}
		expected = MonitorExpectation{Kind: ExpectStepCompleted, StepID: stepID}
	}

	monitorID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate monitor point ID: %w", err)
	}
	revisionID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate monitor point revision ID: %w", err)
	}
	now := time.Now().UTC()

	repo := NewRepository(tx)

	if _, err := s.workItems.GetByIDTx(ctx, tx, workItemID); err != nil {
		if errors.Is(err, workitem.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrWorkItemUnavailable, workItemID)
		}
		return nil, err
	}
	if subjectID != nil {
		item, err := repo.GetItem(ctx, *subjectID)
		if err != nil {
			if errors.Is(err, ErrTrackedItemNotFound) {
				return nil, fmt.Errorf("%w: subject %s does not exist", ErrInvalidParent, *subjectID)
			}
			return nil, err
		}
		if item.WorkItemID != workItemID {
			return nil, fmt.Errorf("%w: subject %s belongs to another work item", ErrInvalidParent, *subjectID)
		}
		workflow, err := repo.GetWorkflow(ctx, item.WorkflowID)
		if err != nil {
			return nil, err
		}
		step, err := repo.getStep(ctx, *stepID)
		if err != nil {
			return nil, err
		}
		if step.WorkflowDefinitionID != workflow.WorkflowDefinitionID {
			return nil, fmt.Errorf("%w: step %s, item %s", ErrStepNotInWorkflow, *stepID, item.ID)
		}
	}
	if input.Kind == MonitorFinalTarget {
		count, err := repo.countActiveFinalTargets(ctx, workItemID, subjectID)
		if err != nil {
			return nil, err
		}
		if count > 0 {
			return nil, fmt.Errorf("%w", ErrFinalTargetExists)
		}
	}

	summary := "Create monitor point"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return nil, err
	}
	if err := foundation.NewRepository(tx).CreateEntity(ctx, monitorID, entityTypeMonitorPoint, now); err != nil {
		return nil, err
	}
	point := &MonitorPoint{
		ID: monitorID, WorkItemID: workItemID, Subject: subject, Kind: input.Kind, Title: title,
		DueAt: input.DueAt.UTC(), Timezone: timezone, Expected: expected, State: MonitorActive,
		LastRevisionID: revisionID, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.createMonitorPoint(ctx, point); err != nil {
		return nil, err
	}
	if err := recordMonitorCreated(ctx, tx, point); err != nil {
		return nil, err
	}
	return point, nil
}

// SetMonitorPointState applies the narrow V1 transitions ACTIVE→SATISFIED and
// ACTIVE→CANCELLED. Same-state is a no-op (no Revision, no timestamp change);
// terminal states cannot change. It records an explicit human decision only:
// nothing is evaluated automatically, and no other row is touched.
func (s *Service) SetMonitorPointState(ctx context.Context, input SetMonitorPointStateInput) (*MonitorPoint, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin set monitor point state transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	current, err := s.SetMonitorPointStateTx(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit set monitor point state transaction: %w", err)
	}
	return current, nil
}

// SetMonitorPointStateTx applies the transition within a caller-provided transaction.
func (s *Service) SetMonitorPointStateTx(ctx context.Context, tx *sql.Tx, input SetMonitorPointStateInput) (*MonitorPoint, error) {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return nil, fmt.Errorf("%w: monitor point ID must not be empty", ErrInvalidInput)
	}
	if !input.Target.Valid() {
		return nil, fmt.Errorf("%w: unsupported monitor state %q", ErrInvalidInput, input.Target)
	}
	repo := NewRepository(tx)

	current, err := repo.getMonitorPoint(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.State == input.Target {
		return current, nil
	}
	if current.State != MonitorActive || input.Target == MonitorActive {
		return nil, fmt.Errorf("%w: %s to %s", ErrInvalidMonitorState, current.State, input.Target)
	}

	revisionID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate monitor point state revision ID: %w", err)
	}
	now := time.Now().UTC()
	summary := fmt.Sprintf("Set monitor point state to %s", input.Target)
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return nil, err
	}
	var satisfiedAt *time.Time
	if input.Target == MonitorSatisfied {
		satisfiedAt = &now
	}
	if err := repo.updateMonitorPointState(ctx, id, input.Target, satisfiedAt, revisionID, now); err != nil {
		return nil, err
	}
	if err := recordMonitorStateChanged(ctx, tx, current, current.State, input.Target, satisfiedAt, revisionID, now); err != nil {
		return nil, err
	}
	current.State, current.SatisfiedAt, current.LastRevisionID, current.UpdatedAt = input.Target, satisfiedAt, revisionID, now
	return current, nil
}

func (r *Repository) createMonitorPoint(ctx context.Context, m *MonitorPoint) error {
	var lifecycle *string
	if m.Expected.WorkItemState != nil {
		value := string(*m.Expected.WorkItemState)
		lifecycle = &value
	}
	var subjectID *string
	if m.Subject.Kind == SubjectTrackedItem {
		id := m.Subject.ID
		subjectID = &id
	}
	if err := r.queries.CreateMonitorPoint(ctx, dbgen.CreateMonitorPointParams{
		EntityID: m.ID, WorkItemID: m.WorkItemID, SubjectTrackedItemID: subjectID,
		MonitorKind: string(m.Kind), Title: m.Title, DueAt: codec.FormatTime(m.DueAt), Timezone: m.Timezone,
		ExpectedKind: string(m.Expected.Kind), ExpectedWorkItemLifecycle: lifecycle, ExpectedStepID: m.Expected.StepID,
		State: string(m.State), SatisfiedAt: codec.FormatOptionalTime(m.SatisfiedAt),
		LastRevisionID: m.LastRevisionID, CreatedAt: codec.FormatTime(m.CreatedAt), UpdatedAt: codec.FormatTime(m.UpdatedAt),
	}); err != nil {
		return fmt.Errorf("create monitor point %s: %w", m.ID, err)
	}
	return nil
}

func (r *Repository) getMonitorPoint(ctx context.Context, id string) (*MonitorPoint, error) {
	row, err := r.queries.GetMonitorPointByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrMonitorPointNotFound, id)
		}
		return nil, fmt.Errorf("load monitor point %s: %w", id, err)
	}
	return monitorFromDB(row)
}

func (r *Repository) countActiveFinalTargets(ctx context.Context, workItemID string, subjectTrackedItemID *string) (int, error) {
	key := ""
	if subjectTrackedItemID != nil {
		key = *subjectTrackedItemID
	}
	count, err := r.queries.CountActiveFinalTargets(ctx, dbgen.CountActiveFinalTargetsParams{WorkItemID: workItemID, SubjectKey: &key})
	if err != nil {
		return 0, fmt.Errorf("count active final targets: %w", err)
	}
	return int(count), nil
}

func (r *Repository) updateMonitorPointState(ctx context.Context, id string, state MonitorState, satisfiedAt *time.Time, revisionID string, at time.Time) error {
	if err := r.queries.UpdateMonitorPointState(ctx, dbgen.UpdateMonitorPointStateParams{
		State: string(state), SatisfiedAt: codec.FormatOptionalTime(satisfiedAt), LastRevisionID: revisionID,
		UpdatedAt: codec.FormatTime(at), EntityID: id,
	}); err != nil {
		return fmt.Errorf("update monitor point %s state: %w", id, err)
	}
	return nil
}

func (r *Repository) listMonitorPoints(ctx context.Context, workItemID string) ([]MonitorPoint, error) {
	rows, err := r.queries.ListMonitorPointsByWorkItem(ctx, workItemID)
	if err != nil {
		return nil, fmt.Errorf("list monitor points: %w", err)
	}
	points := make([]MonitorPoint, 0, len(rows))
	for _, row := range rows {
		point, err := monitorFromDB(row)
		if err != nil {
			return nil, err
		}
		points = append(points, *point)
	}
	return points, nil
}

func monitorFromDB(row dbgen.MonitorPoint) (*MonitorPoint, error) {
	kind, state := MonitorKind(row.MonitorKind), MonitorState(row.State)
	if !kind.Valid() || !state.Valid() {
		return nil, fmt.Errorf("monitor point %s has invalid kind/state %q/%q", row.EntityID, row.MonitorKind, row.State)
	}
	due, err := codec.ParseTime(row.DueAt)
	if err != nil {
		return nil, fmt.Errorf("parse monitor point %s due_at: %w", row.EntityID, err)
	}
	created, err := codec.ParseTime(row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse monitor point %s created_at: %w", row.EntityID, err)
	}
	updated, err := codec.ParseTime(row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse monitor point %s updated_at: %w", row.EntityID, err)
	}
	satisfied, err := codec.ParseOptionalTime(row.SatisfiedAt)
	if err != nil {
		return nil, fmt.Errorf("parse monitor point %s satisfied_at: %w", row.EntityID, err)
	}
	point := &MonitorPoint{
		ID: row.EntityID, WorkItemID: row.WorkItemID, Kind: kind, Title: row.Title, DueAt: due, Timezone: row.Timezone,
		State: state, SatisfiedAt: satisfied, LastRevisionID: row.LastRevisionID, CreatedAt: created, UpdatedAt: updated,
		Subject: MonitorSubject{Kind: SubjectWorkItem, ID: row.WorkItemID},
	}
	if row.SubjectTrackedItemID != nil {
		point.Subject = MonitorSubject{Kind: SubjectTrackedItem, ID: *row.SubjectTrackedItemID}
	}
	point.Expected = MonitorExpectation{Kind: ExpectationKind(row.ExpectedKind), StepID: row.ExpectedStepID}
	if row.ExpectedWorkItemLifecycle != nil {
		lifecycle := workitem.LifecycleState(*row.ExpectedWorkItemLifecycle)
		point.Expected.WorkItemState = &lifecycle
	}
	return point, nil
}
