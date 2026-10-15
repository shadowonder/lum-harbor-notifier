package applicationruntime

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/background"
	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
	"lum-harbor-notifier/internal/foundation"
	"lum-harbor-notifier/internal/identity"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/reminder"
	"lum-harbor-notifier/internal/temporal"
)

// TodoReminderCycleJobID is the static Scheduler identity of the M18 cycle.
const TodoReminderCycleJobID = "todo.reminder.cycle"

// TodoReminderCycle is the evaluation interface driving Todo reminder checks.
type TodoReminderCycle interface {
	EvaluateReminders(ctx context.Context, now time.Time) (TodoReminderCycleResult, error)
}

// TodoReminderCycleResult carries metrics from one evaluation run.
type TodoReminderCycleResult struct {
	Evaluated int
	Fired     int
	Satisfied int
}

// TodoReminderEvaluator implements TodoReminderCycle.
type TodoReminderEvaluator struct {
	db        *sql.DB
	reminder  *reminder.Service
	attention *attention.Service
}

func NewTodoReminderCycle(db *sql.DB, reminderService *reminder.Service, attentionService *attention.Service) *TodoReminderEvaluator {
	return &TodoReminderEvaluator{
		db:        db,
		reminder:  reminderService,
		attention: attentionService,
	}
}

// EvaluateReminders inspects all ACTIVE reminders, triggering attention and notifications
// when threshold offsets are reached, and satisfying reminders if the Todo was completed.
func (e *TodoReminderEvaluator) EvaluateReminders(ctx context.Context, now time.Time) (TodoReminderCycleResult, error) {
	now = now.UTC()
	active, err := e.reminder.ListActive(ctx)
	if err != nil {
		return TodoReminderCycleResult{}, fmt.Errorf("list active reminders: %w", err)
	}

	result := TodoReminderCycleResult{Evaluated: len(active)}
	for _, rem := range active {
		rem := rem
		if err := e.evaluateOne(ctx, &rem, now, &result); err != nil {
			return result, err
		}
	}

	if result.Fired > 0 {
		if _, err := e.attention.DispatchPendingNotifications(ctx, now); err != nil {
			slog.Warn("dispatch pending notifications after todo reminder fired", "error", err)
		}
	}

	return result, nil
}

func (e *TodoReminderEvaluator) evaluateOne(ctx context.Context, rem *reminder.Reminder, now time.Time, result *TodoReminderCycleResult) error {
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin evaluate reminder tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	queries := dbgen.New(tx)
	item, err := queries.GetPlannerItemByID(ctx, rem.PlannerItemID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("get planner item %s: %w", rem.PlannerItemID, err)
	}

	// If item is completed, satisfy reminder and resolve any open attention issues.
	if item.Status == string(planner.StatusCompleted) {
		revID, err := identity.New()
		if err != nil {
			return fmt.Errorf("generate revision ID: %w", err)
		}
		summary := "Satisfy reminder for completed todo"
		if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revID, foundation.RevisionOriginSystem, &summary, now); err != nil {
			return err
		}
		if err := e.reminder.SatisfyReminderTx(ctx, tx, rem.PlannerItemID, revID, now); err != nil {
			return err
		}
		if err := e.attention.ResolveByPlannerItemTx(ctx, tx, rem.PlannerItemID, revID, now); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit satisfy reminder tx: %w", err)
		}
		result.Satisfied++
		return nil
	}

	// Parse due time.
	var dueTime time.Time
	if item.DueAt != nil && *item.DueAt != "" {
		parsed, err := codec.ParseTime(*item.DueAt)
		if err != nil {
			return nil
		}
		dueTime = parsed
	} else if item.DueDate != nil && *item.DueDate != "" {
		dueDate, err := temporal.ParseLocalDate(*item.DueDate)
		if err != nil {
			return nil
		}
		t, err := time.ParseInLocation(temporal.LocalDateLayout, string(dueDate), time.UTC)
		if err != nil {
			return nil
		}
		dueTime = t.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	} else {
		return nil
	}

	offsets := rem.OffsetsSeconds
	if len(offsets) == 0 {
		offsets = []int{0}
	}

	firedSet := make(map[int]bool)
	for _, f := range rem.FiredOffsets {
		firedSet[f] = true
	}

	var passedOffsets []int
	for _, offset := range offsets {
		if firedSet[offset] {
			continue
		}
		triggerTime := dueTime.Add(-time.Duration(offset) * time.Second)
		if !now.Before(triggerTime) {
			passedOffsets = append(passedOffsets, offset)
		}
	}

	if len(passedOffsets) > 0 {
		// Sort by ascending offsetSeconds (proximity to due date / most recent trigger time).
		sort.Ints(passedOffsets)
		toFire := passedOffsets[0]

		input := attention.OpenTodoReminderAttentionInput{
			PlannerItemID: rem.PlannerItemID,
			WorkItemID:    item.WorkItemID,
			Title:         item.Title,
			OffsetSeconds: toFire,
			DueAt:         dueTime,
		}
		_, _, err := e.attention.OpenTodoReminderAttentionTx(ctx, tx, input, now)
		if err != nil {
			if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
				return fmt.Errorf("open todo reminder attention: %w", err)
			}
		}
		result.Fired++

		// Mark all passed offsets as handled so older missed thresholds are suppressed.
		for _, offset := range passedOffsets {
			firedSet[offset] = true
		}

		allFired := make([]int, 0, len(firedSet))
		for f := range firedSet {
			allFired = append(allFired, f)
		}
		sort.Ints(allFired)

		revID, err := identity.New()
		if err != nil {
			return fmt.Errorf("generate revision ID: %w", err)
		}
		summary := "Record fired reminder offsets"
		if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revID, foundation.RevisionOriginSystem, &summary, now); err != nil {
			return err
		}
		if err := e.reminder.UpdateFiredOffsetsTx(ctx, tx, rem.ID, revID, allFired, now); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit update fired offsets tx: %w", err)
		}
	}

	return nil
}

type todoReminderCycleJob struct {
	cycle TodoReminderCycle
	clock func() time.Time
}

func (j todoReminderCycleJob) Run(ctx context.Context) error {
	now := j.clock().UTC()
	result, err := j.cycle.EvaluateReminders(ctx, now)
	if err != nil {
		return fmt.Errorf("todo reminder cycle: %w", err)
	}
	if result.Fired > 0 || result.Satisfied > 0 {
		slog.Info("todo reminder cycle", "evaluated", result.Evaluated, "fired", result.Fired, "satisfied", result.Satisfied)
	}
	return nil
}

// RegisterTodoReminderCycle registers the todo reminder cycle with the scheduler.
func RegisterTodoReminderCycle(
	scheduler *background.Scheduler,
	cycle TodoReminderCycle,
	interval, timeout time.Duration,
) (background.JobDefinition, error) {
	return registerTodoReminderCycle(scheduler, cycle, interval, timeout, time.Now)
}

func registerTodoReminderCycle(
	scheduler *background.Scheduler,
	cycle TodoReminderCycle,
	interval, timeout time.Duration,
	clock func() time.Time,
) (background.JobDefinition, error) {
	if scheduler == nil {
		return background.JobDefinition{}, fmt.Errorf("register todo reminder cycle: scheduler must not be nil")
	}
	if cycle == nil {
		return background.JobDefinition{}, fmt.Errorf("register todo reminder cycle: cycle must not be nil")
	}
	definition := background.JobDefinition{
		ID:            TodoReminderCycleJobID,
		Interval:      interval,
		Timeout:       timeout,
		Enabled:       true,
		OverlapPolicy: background.OverlapPolicySkip,
	}
	if err := scheduler.Register(definition, todoReminderCycleJob{cycle: cycle, clock: clock}); err != nil {
		return background.JobDefinition{}, fmt.Errorf("register todo reminder cycle: %w", err)
	}
	return definition, nil
}
