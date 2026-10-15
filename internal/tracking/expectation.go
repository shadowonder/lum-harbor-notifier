package tracking

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"lum-harbor-notifier/internal/database/codec"
	"lum-harbor-notifier/internal/workitem"
)

// ExpectationEvaluation is the authoritative answer to "is this MonitorPoint's
// typed expected condition currently satisfied?" together with the facts that
// justify it. It is read-only evidence for consumers such as Attention; it is
// never persisted here and changes no Monitor state.
type ExpectationEvaluation struct {
	Met bool
	// SubjectTitle is the TrackedItem title, empty for a WorkItem subject.
	SubjectTitle string
	// StepTitle is the expected workflow step's title (step expectations only).
	StepTitle string
	// Actual* carry the observed condition matching the expectation kind.
	ActualWorkItemLifecycle *workitem.LifecycleState
	ActualStepState         *StepState
}

// ListDueActiveMonitorPointsTx returns ACTIVE MonitorPoints with due_at <= now
// (current WorkItems only) inside the caller's transaction, so a consumer
// evaluates one consistent snapshot. This is a narrow consumption seam for M11;
// it does not change M10 semantics.
func (s *Service) ListDueActiveMonitorPointsTx(ctx context.Context, tx *sql.Tx, now time.Time) ([]MonitorPoint, error) {
	rows, err := NewRepository(tx).queries.ListDueActiveMonitorPoints(ctx, codec.FormatTime(now))
	if err != nil {
		return nil, fmt.Errorf("list due active monitor points: %w", err)
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

// GetMonitorPointTx loads one current MonitorPoint inside the caller's
// transaction. It returns ErrMonitorPointNotFound when it is unknown/archived.
func (s *Service) GetMonitorPointTx(ctx context.Context, tx *sql.Tx, id string) (*MonitorPoint, error) {
	return NewRepository(tx).getMonitorPoint(ctx, id)
}

// EvaluateExpectationTx evaluates the typed expected condition against
// authoritative truth in the caller's transaction:
//   - WORK_ITEM_LIFECYCLE: met iff the WorkItem's lifecycle equals the expected one;
//   - TRACKED_ITEM_STEP_COMPLETED: met iff the exact expected step's state is
//     COMPLETED (SKIPPED does not satisfy it).
//
// An unavailable WorkItem/TrackedItem returns ErrWorkItemUnavailable /
// ErrTrackedItemNotFound so the caller can treat the expectation as inapplicable.
func (s *Service) EvaluateExpectationTx(ctx context.Context, tx *sql.Tx, monitor *MonitorPoint) (ExpectationEvaluation, error) {
	switch monitor.Expected.Kind {
	case ExpectWorkItemLifecycle:
		item, err := s.workItems.GetByIDTx(ctx, tx, monitor.WorkItemID)
		if err != nil {
			if errors.Is(err, workitem.ErrNotFound) {
				return ExpectationEvaluation{}, fmt.Errorf("%w: %s", ErrWorkItemUnavailable, monitor.WorkItemID)
			}
			return ExpectationEvaluation{}, err
		}
		if monitor.Expected.WorkItemState == nil {
			return ExpectationEvaluation{}, fmt.Errorf("%w: monitor %s has no expected lifecycle", ErrInconsistentState, monitor.ID)
		}
		actual := item.Lifecycle
		return ExpectationEvaluation{Met: actual == *monitor.Expected.WorkItemState, ActualWorkItemLifecycle: &actual}, nil
	case ExpectStepCompleted:
		if monitor.Expected.StepID == nil {
			return ExpectationEvaluation{}, fmt.Errorf("%w: monitor %s has no expected step", ErrInconsistentState, monitor.ID)
		}
		repo := NewRepository(tx)
		item, err := repo.GetItem(ctx, monitor.Subject.ID)
		if err != nil {
			return ExpectationEvaluation{}, err
		}
		step, err := repo.getStep(ctx, *monitor.Expected.StepID)
		if err != nil {
			return ExpectationEvaluation{}, err
		}
		state, err := repo.getStepState(ctx, item.ID, step.ID)
		if err != nil {
			return ExpectationEvaluation{}, err
		}
		actual := state.State
		return ExpectationEvaluation{Met: actual == StepCompleted, SubjectTitle: item.Title, StepTitle: step.Title, ActualStepState: &actual}, nil
	default:
		return ExpectationEvaluation{}, fmt.Errorf("%w: unsupported expectation kind %q", ErrInconsistentState, monitor.Expected.Kind)
	}
}
