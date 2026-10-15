package reminder

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	dbgen "lum-harbor-notifier/internal/database/generated"
	"lum-harbor-notifier/internal/foundation"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/identity"
)

const entityTypeReminder = "TODO_REMINDER"

type reminderSetSnapshot struct {
	Version        int    `json:"version"`
	PlannerItemID  string `json:"planner_item_id"`
	OffsetsSeconds []int  `json:"offsets_seconds"`
	RemindMode     string `json:"remind_mode"`
	State          string `json:"state"`
}

type reminderRemovedSnapshot struct {
	Version       int    `json:"version"`
	PlannerItemID string `json:"planner_item_id"`
	PreviousState string `json:"previous_state"`
}

type Service struct {
	db    *sql.DB
	clock func() time.Time
}

func NewService(db *sql.DB) *Service {
	return &Service{
		db:    db,
		clock: func() time.Time { return time.Now().UTC() },
	}
}

func (s *Service) SetClock(clock func() time.Time) {
	s.clock = clock
}

func (s *Service) SetReminder(ctx context.Context, input SetReminderInput) (*Reminder, error) {
	input.PlannerItemID = strings.TrimSpace(input.PlannerItemID)
	if input.PlannerItemID == "" {
		return nil, fmt.Errorf("%w: planner_item_id is required", ErrInvalidInput)
	}

	now := s.clock()
	revisionID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate revision ID: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin set reminder transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	summary := "Set todo reminder"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return nil, err
	}

	rem, err := s.SetReminderTx(ctx, tx, input, revisionID, now)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit set reminder transaction: %w", err)
	}

	return rem, nil
}

func (s *Service) SetReminderTx(ctx context.Context, tx *sql.Tx, input SetReminderInput, revisionID string, now time.Time) (*Reminder, error) {
	input.PlannerItemID = strings.TrimSpace(input.PlannerItemID)
	if input.PlannerItemID == "" {
		return nil, fmt.Errorf("%w: planner_item_id is required", ErrInvalidInput)
	}

	if input.RemindMode == "" {
		input.RemindMode = RemindModeBeforeDue
	}
	if !input.RemindMode.Valid() {
		return nil, fmt.Errorf("%w: invalid remind_mode %q", ErrInvalidInput, input.RemindMode)
	}

	if input.OffsetsSeconds == nil {
		input.OffsetsSeconds = []int{}
	}

	repo := NewRepository(tx)
	existing, err := repo.GetByPlannerItemID(ctx, input.PlannerItemID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	var workItemID *string
	pItem, pErr := dbgen.New(tx).GetPlannerItemByID(ctx, input.PlannerItemID)
	if pErr != nil {
		if errors.Is(pErr, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: planner item %q not found", ErrNotFound, input.PlannerItemID)
		}
		return nil, fmt.Errorf("get planner item %q: %w", input.PlannerItemID, pErr)
	}
	workItemID = pItem.WorkItemID

	hasDueTarget := (pItem.DueAt != nil && strings.TrimSpace(*pItem.DueAt) != "") ||
		(pItem.DueDate != nil && strings.TrimSpace(*pItem.DueDate) != "")
	if !hasDueTarget && (input.RemindMode == "" || input.RemindMode == RemindModeBeforeDue) {
		return nil, fmt.Errorf("%w: %w: set a due date before adding a due-time reminder", ErrInvalidInput, ErrUndatedTodo)
	}

	var result *Reminder
	if existing != nil {
		// Existing reminder: update configuration, activate state. When offsets
		// change, thresholds that already fired and are still configured stay
		// fired (adding or removing another threshold must not re-arm them);
		// fired entries for removed thresholds are dropped.
		fired := existing.FiredOffsets
		if !equalIntSlices(existing.OffsetsSeconds, input.OffsetsSeconds) {
			fired = retainedFiredOffsets(existing.FiredOffsets, input.OffsetsSeconds)
		}
		if err := repo.UpdateConfiguration(ctx, existing.ID, revisionID, input.OffsetsSeconds, fired, input.RemindMode, StateActive, now); err != nil {
			return nil, err
		}
		existing.OffsetsSeconds = input.OffsetsSeconds
		existing.FiredOffsets = fired
		existing.RemindMode = input.RemindMode
		existing.State = StateActive
		existing.LastRevisionID = revisionID
		existing.UpdatedAt = now
		result = existing
	} else {
		remID, err := identity.New()
		if err != nil {
			return nil, fmt.Errorf("generate reminder ID: %w", err)
		}
		if err := foundation.NewRepository(tx).CreateEntity(ctx, remID, entityTypeReminder, now); err != nil {
			return nil, err
		}
		rem := &Reminder{
			ID:             remID,
			PlannerItemID:  input.PlannerItemID,
			State:          StateActive,
			OffsetsSeconds: input.OffsetsSeconds,
			FiredOffsets:   []int{},
			RemindMode:     input.RemindMode,
			LastRevisionID: revisionID,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := repo.Create(ctx, rem); err != nil {
			return nil, err
		}
		result = rem
	}

	// Record History event
	if err := recordReminderSet(ctx, tx, result, workItemID, revisionID, now, input.ConfiguredAtCreation); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *Service) GetByPlannerItemID(ctx context.Context, plannerItemID string) (*Reminder, error) {
	return NewRepository(s.db).GetByPlannerItemID(ctx, strings.TrimSpace(plannerItemID))
}

func (s *Service) ListActive(ctx context.Context) ([]Reminder, error) {
	return NewRepository(s.db).ListActive(ctx)
}

func (s *Service) SatisfyReminderTx(ctx context.Context, tx *sql.Tx, plannerItemID, revisionID string, now time.Time) error {
	repo := NewRepository(tx)
	rem, err := repo.GetByPlannerItemID(ctx, strings.TrimSpace(plannerItemID))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if rem.State != StateActive {
		return nil
	}
	return repo.SetState(ctx, rem.ID, revisionID, StateSatisfied, &now, now)
}

func (s *Service) RemoveReminder(ctx context.Context, plannerItemID string) error {
	plannerItemID = strings.TrimSpace(plannerItemID)
	if plannerItemID == "" {
		return fmt.Errorf("%w: planner_item_id is required", ErrInvalidInput)
	}

	now := s.clock()
	revisionID, err := identity.New()
	if err != nil {
		return fmt.Errorf("generate revision ID: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin remove reminder transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	summary := "Remove todo reminder"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return err
	}

	if err := s.RemoveReminderTx(ctx, tx, plannerItemID, revisionID, now); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit remove reminder transaction: %w", err)
	}

	return nil
}

func (s *Service) RemoveReminderTx(ctx context.Context, tx *sql.Tx, plannerItemID, revisionID string, now time.Time) error {
	repo := NewRepository(tx)
	rem, err := repo.GetByPlannerItemID(ctx, strings.TrimSpace(plannerItemID))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if rem.State == StateCancelled {
		return nil
	}

	prevState := string(rem.State)
	if err := repo.SetState(ctx, rem.ID, revisionID, StateCancelled, nil, now); err != nil {
		return err
	}

	var workItemID *string
	pItem, pErr := dbgen.New(tx).GetPlannerItemByID(ctx, plannerItemID)
	if pErr == nil {
		workItemID = pItem.WorkItemID
	}

	return recordReminderRemoved(ctx, tx, plannerItemID, prevState, workItemID, revisionID, now)
}

func (s *Service) UpdateFiredOffsetsTx(ctx context.Context, tx *sql.Tx, id, revisionID string, fired []int, now time.Time) error {
	return NewRepository(tx).UpdateFiredOffsets(ctx, id, revisionID, fired, now)
}

func recordReminderSet(ctx context.Context, tx *sql.Tx, rem *Reminder, workItemID *string, revisionID string, now time.Time, configuredAtCreation bool) error {
	after, err := history.Snapshot(reminderSetSnapshot{
		Version:        1,
		PlannerItemID:  rem.PlannerItemID,
		OffsetsSeconds: rem.OffsetsSeconds,
		RemindMode:     string(rem.RemindMode),
		State:          string(rem.State),
	})
	if err != nil {
		return err
	}
	subjectID := rem.PlannerItemID
	historyRepo := history.NewRepository(tx)
	var causeID *string
	if configuredAtCreation {
		created, err := historyRepo.FindLatestBySubjectAndType(ctx, rem.PlannerItemID, history.EventPlannerItemCreated)
		if err != nil {
			return err
		}
		if created != nil {
			causeID = &created.ID
		}
	}
	_, err = historyRepo.Append(ctx, history.Draft{
		Type:             history.EventPlannerItemReminderSet,
		Scope:            history.ScopePlanner,
		Actor:            history.ActorUser,
		Summary:          "Reminder configured for planner item",
		SubjectEntityID:  &subjectID,
		WorkItemID:       workItemID,
		RevisionID:       &revisionID,
		CorrelationID:    history.PlannerCorrelation(rem.PlannerItemID),
		CausationEventID: causeID,
		AfterJSON:        after,
		OccurredAt:       now,
	})
	return err
}

func recordReminderRemoved(ctx context.Context, tx *sql.Tx, plannerItemID, prevState string, workItemID *string, revisionID string, now time.Time) error {
	before, err := history.Snapshot(reminderRemovedSnapshot{
		Version:       1,
		PlannerItemID: plannerItemID,
		PreviousState: prevState,
	})
	if err != nil {
		return err
	}
	subjectID := plannerItemID
	_, err = history.NewRepository(tx).Append(ctx, history.Draft{
		Type:            history.EventPlannerItemReminderRemoved,
		Scope:           history.ScopePlanner,
		Actor:           history.ActorUser,
		Summary:         "Reminder removed for planner item",
		SubjectEntityID: &subjectID,
		WorkItemID:      workItemID,
		RevisionID:      &revisionID,
		CorrelationID:   history.PlannerCorrelation(plannerItemID),
		BeforeJSON:      before,
		OccurredAt:      now,
	})
	return err
}

// retainedFiredOffsets returns the fired offsets that remain configured.
func retainedFiredOffsets(fired, offsets []int) []int {
	retained := []int{}
	for _, f := range fired {
		for _, o := range offsets {
			if f == o {
				retained = append(retained, f)
				break
			}
		}
	}
	return retained
}

func equalIntSlices(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
