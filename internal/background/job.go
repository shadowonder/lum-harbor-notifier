package background

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Job interface {
	Run(ctx context.Context) error
}

type OverlapPolicy string

const (
	OverlapPolicySkip OverlapPolicy = "SKIP"
)

// JobDefinition describes how Scheduler should run a registered Job.
//
// JobDefinition contains scheduling policy rather than business logic. This
// separation allows the same Job implementation to remain independent from
// whether it is invoked periodically, manually, or eventually through a
// plugin-backed capability.
//
// An Interval of zero means the Job has no periodic schedule and runs only
// when explicitly triggered. Positive intervals use fixed-delay semantics:
// Scheduler waits for the configured interval after one execution finishes
// before the next periodic execution becomes eligible.
type JobDefinition struct {
	ID            string
	Interval      time.Duration
	Timeout       time.Duration
	Enabled       bool
	OverlapPolicy OverlapPolicy
}

// Validate verifies that a JobDefinition can be safely registered with the
// background Scheduler.
//
// Validation happens before Scheduler starts so invalid runtime configuration
// cannot create partially running background infrastructure.
func (d JobDefinition) Validate() error {
	if strings.TrimSpace(d.ID) == "" {
		return fmt.Errorf("background job ID must not be empty")
	}
	if d.Interval < 0 {
		return fmt.Errorf("background job %q interval must not be negative", d.ID)
	}
	if d.Timeout <= 0 {
		return fmt.Errorf("background job %q timeout must be greater than zero", d.ID)
	}
	if d.OverlapPolicy != OverlapPolicySkip {
		return fmt.Errorf("background job %q has unsupported overlap policy %q", d.ID, d.OverlapPolicy)
	}
	return nil
}
