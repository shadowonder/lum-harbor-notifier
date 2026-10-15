package reminder

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
)

type Repository struct {
	queries *dbgen.Queries
}

func NewRepository(db dbgen.DBTX) *Repository {
	return &Repository{queries: dbgen.New(db)}
}

func (r *Repository) Create(ctx context.Context, rem *Reminder) error {
	offsetsJSON, err := json.Marshal(rem.OffsetsSeconds)
	if err != nil {
		return fmt.Errorf("marshal offsets: %w", err)
	}
	firedJSON, err := json.Marshal(rem.FiredOffsets)
	if err != nil {
		return fmt.Errorf("marshal fired offsets: %w", err)
	}

	mode := string(rem.RemindMode)
	if mode == "" {
		mode = string(RemindModeBeforeDue)
	}

	return r.queries.CreateTodoReminder(ctx, dbgen.CreateTodoReminderParams{
		EntityID:           rem.ID,
		PlannerItemID:      rem.PlannerItemID,
		State:              string(rem.State),
		OffsetsSecondsJson: string(offsetsJSON),
		FiredOffsetsJson:   string(firedJSON),
		RemindMode:         mode,
		LastRevisionID:     rem.LastRevisionID,
		CreatedAt:          codec.FormatTime(rem.CreatedAt),
		UpdatedAt:          codec.FormatTime(rem.UpdatedAt),
		SatisfiedAt:        codec.FormatOptionalTime(rem.SatisfiedAt),
	})
}

func (r *Repository) GetByID(ctx context.Context, id string) (*Reminder, error) {
	row, err := r.queries.GetTodoReminderByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("get todo reminder %s: %w", id, err)
	}
	return reminderFromDB(row)
}

func (r *Repository) GetByPlannerItemID(ctx context.Context, plannerItemID string) (*Reminder, error) {
	row, err := r.queries.GetTodoReminderByPlannerItemID(ctx, plannerItemID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w for planner item: %s", ErrNotFound, plannerItemID)
		}
		return nil, fmt.Errorf("get todo reminder for item %s: %w", plannerItemID, err)
	}
	return reminderFromDB(row)
}

func (r *Repository) ListActive(ctx context.Context) ([]Reminder, error) {
	rows, err := r.queries.ListActiveTodoReminders(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active todo reminders: %w", err)
	}
	result := make([]Reminder, 0, len(rows))
	for _, row := range rows {
		rem, err := reminderFromDB(row)
		if err != nil {
			return nil, err
		}
		result = append(result, *rem)
	}
	return result, nil
}

func (r *Repository) UpdateFiredOffsets(ctx context.Context, id, revisionID string, fired []int, updatedAt time.Time) error {
	firedJSON, err := json.Marshal(fired)
	if err != nil {
		return fmt.Errorf("marshal fired offsets: %w", err)
	}
	return r.queries.UpdateTodoReminderFiredOffsets(ctx, dbgen.UpdateTodoReminderFiredOffsetsParams{
		FiredOffsetsJson: string(firedJSON),
		LastRevisionID:   revisionID,
		UpdatedAt:        codec.FormatTime(updatedAt),
		EntityID:         id,
	})
}

func (r *Repository) SetState(ctx context.Context, id, revisionID string, state State, satisfiedAt *time.Time, updatedAt time.Time) error {
	return r.queries.SetTodoReminderState(ctx, dbgen.SetTodoReminderStateParams{
		State:          string(state),
		SatisfiedAt:    codec.FormatOptionalTime(satisfiedAt),
		LastRevisionID: revisionID,
		UpdatedAt:      codec.FormatTime(updatedAt),
		EntityID:       id,
	})
}

func (r *Repository) UpdateConfiguration(ctx context.Context, id, revisionID string, offsets, fired []int, mode RemindMode, state State, updatedAt time.Time) error {
	offsetsJSON, err := json.Marshal(offsets)
	if err != nil {
		return fmt.Errorf("marshal offsets: %w", err)
	}
	firedJSON, err := json.Marshal(fired)
	if err != nil {
		return fmt.Errorf("marshal fired offsets: %w", err)
	}
	return r.queries.UpdateTodoReminderConfiguration(ctx, dbgen.UpdateTodoReminderConfigurationParams{
		OffsetsSecondsJson: string(offsetsJSON),
		FiredOffsetsJson:   string(firedJSON),
		RemindMode:         string(mode),
		State:              string(state),
		LastRevisionID:     revisionID,
		UpdatedAt:          codec.FormatTime(updatedAt),
		EntityID:           id,
	})
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	return r.queries.DeleteTodoReminder(ctx, id)
}

func reminderFromDB(row dbgen.TodoReminder) (*Reminder, error) {
	created, err := codec.ParseTime(row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse todo reminder %s created_at: %w", row.EntityID, err)
	}
	updated, err := codec.ParseTime(row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse todo reminder %s updated_at: %w", row.EntityID, err)
	}
	satisfied, err := codec.ParseOptionalTime(row.SatisfiedAt)
	if err != nil {
		return nil, fmt.Errorf("parse todo reminder %s satisfied_at: %w", row.EntityID, err)
	}

	var offsets []int
	if err := json.Unmarshal([]byte(row.OffsetsSecondsJson), &offsets); err != nil {
		return nil, fmt.Errorf("unmarshal offsets_seconds_json for %s: %w", row.EntityID, err)
	}
	if offsets == nil {
		offsets = []int{}
	}

	var fired []int
	if err := json.Unmarshal([]byte(row.FiredOffsetsJson), &fired); err != nil {
		return nil, fmt.Errorf("unmarshal fired_offsets_json for %s: %w", row.EntityID, err)
	}
	if fired == nil {
		fired = []int{}
	}

	return &Reminder{
		ID:             row.EntityID,
		PlannerItemID:  row.PlannerItemID,
		State:          State(row.State),
		OffsetsSeconds: offsets,
		FiredOffsets:   fired,
		RemindMode:     RemindMode(row.RemindMode),
		LastRevisionID: row.LastRevisionID,
		CreatedAt:      created,
		UpdatedAt:      updated,
		SatisfiedAt:    satisfied,
	}, nil
}
