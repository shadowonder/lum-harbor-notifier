package background

import (
	"context"
	"testing"
	"time"
)

type pointerJob struct{}

func (*pointerJob) Run(context.Context) error { return nil }

func TestJobDefinitionValidationMatrix(t *testing.T) {
	tests := []JobDefinition{
		{ID: " ", Timeout: time.Second, OverlapPolicy: OverlapPolicySkip},
		{ID: "negative", Interval: -time.Second, Timeout: time.Second, OverlapPolicy: OverlapPolicySkip},
		{ID: "timeout", Timeout: 0, OverlapPolicy: OverlapPolicySkip},
		{ID: "overlap", Timeout: time.Second, OverlapPolicy: "ALLOW"},
	}
	for _, definition := range tests {
		if err := definition.Validate(); err == nil {
			t.Fatalf("expected invalid definition %+v", definition)
		}
	}
}

func TestSchedulerRejectsNilAndTypedNilJobs(t *testing.T) {
	definition := JobDefinition{ID: "nil", Timeout: time.Second, OverlapPolicy: OverlapPolicySkip}
	var typedNil *pointerJob
	for _, job := range []Job{nil, typedNil} {
		scheduler := NewScheduler()
		if err := scheduler.Register(definition, job); err == nil {
			t.Fatalf("expected nil Job rejection for %#v", job)
		}
	}
}

func TestRegisteredDefinitionIsValueCopied(t *testing.T) {
	scheduler := NewScheduler()
	definition := JobDefinition{ID: "copied", Timeout: time.Second, Enabled: true, OverlapPolicy: OverlapPolicySkip}
	if err := scheduler.Register(definition, jobFunc(func(context.Context) error { return nil })); err != nil {
		t.Fatal(err)
	}
	definition.ID = "mutated"
	definition.Enabled = false
	if _, err := scheduler.Snapshot("copied"); err != nil {
		t.Fatalf("registered definition changed: %v", err)
	}
}
