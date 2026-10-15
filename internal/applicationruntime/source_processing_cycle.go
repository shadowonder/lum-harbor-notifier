package applicationruntime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"lum-harbor-notifier/internal/background"
)

// SourceProcessingCycleJobID is the static Scheduler identity of the background semantic source cycle.
const SourceProcessingCycleJobID = "source.processing.cycle"

// SourceProcessingCycleResult tracks the counts of sources processed during a cycle.
type SourceProcessingCycleResult struct {
	Scanned   int
	Claimed   int
	Completed int
	Failed    int
}

// SourceProcessingCycle is the interface driving background semantic source processing.
type SourceProcessingCycle interface {
	ProcessPendingSources(ctx context.Context) (SourceProcessingCycleResult, error)
}

// DeferredSourceProcessingCycle allows late binding of the implementation
// after the Scheduler has been initialized and started.
type DeferredSourceProcessingCycle struct {
	mu   sync.RWMutex
	impl SourceProcessingCycle
}

func (d *DeferredSourceProcessingCycle) Set(impl SourceProcessingCycle) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.impl = impl
}

func (d *DeferredSourceProcessingCycle) ProcessPendingSources(ctx context.Context) (SourceProcessingCycleResult, error) {
	d.mu.RLock()
	impl := d.impl
	d.mu.RUnlock()
	if impl == nil {
		return SourceProcessingCycleResult{}, nil
	}
	return impl.ProcessPendingSources(ctx)
}

type sourceProcessingCycleJob struct {
	cycle SourceProcessingCycle
}

func (j sourceProcessingCycleJob) Run(ctx context.Context) error {
	result, err := j.cycle.ProcessPendingSources(ctx)
	if err != nil {
		return fmt.Errorf("source processing cycle: %w", err)
	}
	if result.Scanned > 0 {
		slog.Info("source processing cycle completed",
			"scanned", result.Scanned, "claimed", result.Claimed,
			"completed", result.Completed, "failed", result.Failed)
	}
	return nil
}

// RegisterSourceProcessingCycle statically registers the background semantic source processing cycle
// with the existing Scheduler.
func RegisterSourceProcessingCycle(
	scheduler *background.Scheduler,
	cycle SourceProcessingCycle,
	interval, timeout time.Duration,
) (background.JobDefinition, error) {
	if scheduler == nil {
		return background.JobDefinition{}, fmt.Errorf("register source processing cycle: scheduler must not be nil")
	}
	if cycle == nil {
		return background.JobDefinition{}, fmt.Errorf("register source processing cycle: cycle must not be nil")
	}
	definition := background.JobDefinition{
		ID:            SourceProcessingCycleJobID,
		Interval:      interval,
		Timeout:       timeout,
		Enabled:       true,
		OverlapPolicy: background.OverlapPolicySkip,
	}
	if err := scheduler.Register(definition, sourceProcessingCycleJob{cycle: cycle}); err != nil {
		return background.JobDefinition{}, fmt.Errorf("register source processing cycle: %w", err)
	}
	return definition, nil
}
