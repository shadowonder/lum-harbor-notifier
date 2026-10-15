package tracking

import (
	"errors"
	"testing"
)

func testSteps() []WorkflowStep {
	return []WorkflowStep{
		{ID: "s1", Key: "a", Sequence: 1}, {ID: "s2", Key: "b", Sequence: 2}, {ID: "s3", Key: "c", Sequence: 3}, {ID: "s4", Key: "d", Sequence: 4},
	}
}

func statesOf(values ...StepState) []StepStateRecord {
	out := make([]StepStateRecord, len(values))
	for i, v := range values {
		out[i] = StepStateRecord{WorkflowStepID: testSteps()[i].ID, State: v}
	}
	return out
}

func TestComputeProgression(t *testing.T) {
	cases := []struct {
		name         string
		states       []StepState
		current      string
		currentState StepState
		passed       int
		percent      int
		complete     bool
	}{
		{"all not started", []StepState{StepNotStarted, StepNotStarted, StepNotStarted, StepNotStarted}, "a", StepNotStarted, 0, 0, false},
		{"one in progress", []StepState{StepInProgress, StepNotStarted, StepNotStarted, StepNotStarted}, "a", StepInProgress, 0, 0, false},
		{"completed prefix", []StepState{StepCompleted, StepCompleted, StepNotStarted, StepNotStarted}, "c", StepNotStarted, 2, 50, false},
		{"blocked current is not progress", []StepState{StepCompleted, StepBlocked, StepNotStarted, StepNotStarted}, "b", StepBlocked, 1, 25, false},
		{"waiting current", []StepState{StepCompleted, StepCompleted, StepWaiting, StepNotStarted}, "c", StepWaiting, 2, 50, false},
		{"failed is not passed", []StepState{StepFailed, StepNotStarted, StepNotStarted, StepNotStarted}, "a", StepFailed, 0, 0, false},
		{"unknown is not passed", []StepState{StepCompleted, StepUnknown, StepNotStarted, StepNotStarted}, "b", StepUnknown, 1, 25, false},
		{"skipped passes", []StepState{StepCompleted, StepSkipped, StepInProgress, StepNotStarted}, "c", StepInProgress, 2, 50, false},
		{"first unpassed wins even after a later completed", []StepState{StepCompleted, StepInProgress, StepCompleted, StepNotStarted}, "b", StepInProgress, 2, 50, false},
		{"all complete or skipped", []StepState{StepCompleted, StepSkipped, StepCompleted, StepSkipped}, "", "", 4, 100, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ComputeProgression(testSteps(), statesOf(tc.states...))
			if err != nil {
				t.Fatal(err)
			}
			if got.TotalSteps != 4 || got.PassedCount != tc.passed || got.ProgressPercent != tc.percent || got.Complete != tc.complete {
				t.Fatalf("unexpected progression: %+v", got)
			}
			if tc.complete {
				if got.CurrentStepID != nil || got.CurrentStepKey != nil || got.CurrentState != nil {
					t.Fatalf("complete progression must have no current step: %+v", got)
				}
				return
			}
			if got.CurrentStepKey == nil || *got.CurrentStepKey != tc.current || *got.CurrentState != tc.currentState {
				t.Fatalf("expected current %s/%s, got %+v", tc.current, tc.currentState, got)
			}
		})
	}
}

func TestComputeProgressionIsIndependentOfInputOrderAndDoesNotMutateInputs(t *testing.T) {
	steps := testSteps()
	shuffled := []WorkflowStep{steps[2], steps[0], steps[3], steps[1]}
	states := statesOf(StepCompleted, StepInProgress, StepNotStarted, StepNotStarted)
	a, err := ComputeProgression(steps, states)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ComputeProgression(shuffled, states)
	if err != nil {
		t.Fatal(err)
	}
	if *a.CurrentStepKey != *b.CurrentStepKey || a.PassedCount != b.PassedCount {
		t.Fatalf("progression must depend on sequence, not slice order: %+v vs %+v", a, b)
	}
	if shuffled[0].ID != "s3" {
		t.Fatal("input steps were reordered")
	}
}

func TestComputeProgressionRejectsInconsistentStates(t *testing.T) {
	if _, err := ComputeProgression(testSteps(), statesOf(StepNotStarted, StepNotStarted)); !errors.Is(err, ErrInconsistentState) {
		t.Fatalf("missing states must be rejected, got %v", err)
	}
	dup := statesOf(StepNotStarted, StepNotStarted, StepNotStarted, StepNotStarted)
	dup[3].WorkflowStepID = "s1"
	if _, err := ComputeProgression(testSteps(), dup); !errors.Is(err, ErrInconsistentState) {
		t.Fatalf("duplicate states must be rejected, got %v", err)
	}
}
