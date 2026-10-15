package tracking

import (
	"fmt"
	"sort"
)

// Progression is the derived workflow position of one TrackedItem. It is a
// pure function of ordered steps and authoritative step states and is never
// written back to the database.
type Progression struct {
	TotalSteps int
	// CompletedCount counts COMPLETED steps; SkippedCount counts SKIPPED steps.
	CompletedCount int
	SkippedCount   int
	// PassedCount is COMPLETED + SKIPPED: steps the workflow has moved past.
	PassedCount int
	// ProgressPercent is floor(PassedCount*100/TotalSteps).
	ProgressPercent int
	// Complete is true when every step has been passed.
	Complete bool
	// Current* describe the first step (by sequence) not yet passed. All are
	// nil when Complete. CurrentState keeps the step's real state, so BLOCKED,
	// WAITING, FAILED and UNKNOWN are never disguised as progress.
	CurrentStepID  *string
	CurrentStepKey *string
	CurrentState   *StepState
}

// ComputeProgression derives the workflow position. steps must belong to one
// definition; every step must have exactly one state row, otherwise
// ErrInconsistentState is returned rather than guessing.
func ComputeProgression(steps []WorkflowStep, states []StepStateRecord) (Progression, error) {
	ordered := append([]WorkflowStep(nil), steps...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Sequence < ordered[j].Sequence })

	byStep := make(map[string]StepState, len(states))
	for _, record := range states {
		if _, dup := byStep[record.WorkflowStepID]; dup {
			return Progression{}, fmt.Errorf("%w: duplicate state for step %s", ErrInconsistentState, record.WorkflowStepID)
		}
		byStep[record.WorkflowStepID] = record.State
	}
	if len(byStep) != len(ordered) {
		return Progression{}, fmt.Errorf("%w: %d steps, %d states", ErrInconsistentState, len(ordered), len(byStep))
	}

	result := Progression{TotalSteps: len(ordered)}
	for _, step := range ordered {
		state, ok := byStep[step.ID]
		if !ok {
			return Progression{}, fmt.Errorf("%w: missing state for step %s", ErrInconsistentState, step.ID)
		}
		switch state {
		case StepCompleted:
			result.CompletedCount++
			result.PassedCount++
		case StepSkipped:
			result.SkippedCount++
			result.PassedCount++
		default:
			if result.CurrentStepID == nil {
				id, key, current := step.ID, step.Key, state
				result.CurrentStepID, result.CurrentStepKey, result.CurrentState = &id, &key, &current
			}
		}
	}
	result.Complete = result.TotalSteps > 0 && result.PassedCount == result.TotalSteps
	if result.TotalSteps > 0 {
		result.ProgressPercent = result.PassedCount * 100 / result.TotalSteps
	}
	return result, nil
}

// State returns the derived semantic state of this progression:
// "COMPLETED" when Complete, string(*CurrentState) if unpassed step exists, or "NOT_STARTED".
func (p Progression) State() string {
	if p.Complete {
		return string(StepCompleted)
	}
	if p.CurrentState != nil {
		return string(*p.CurrentState)
	}
	return string(StepNotStarted)
}
