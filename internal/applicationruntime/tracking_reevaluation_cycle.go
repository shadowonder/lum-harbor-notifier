package applicationruntime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"lum-harbor-notifier/internal/background"
)

const TrackingReevaluationCycleJobID = "tracking.reevaluation.cycle"

type TrackingReevaluationCycleResult struct {
	Scanned   int
	Claimed   int
	Completed int
	Failed    int
}

type TrackingReevaluationCycle interface {
	ProcessPendingTrackingReevaluations(context.Context) (TrackingReevaluationCycleResult, error)
}

type DeferredTrackingReevaluationCycle struct {
	mu   sync.RWMutex
	impl TrackingReevaluationCycle
}

func (d *DeferredTrackingReevaluationCycle) Set(impl TrackingReevaluationCycle) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.impl = impl
}

func (d *DeferredTrackingReevaluationCycle) ProcessPendingTrackingReevaluations(ctx context.Context) (TrackingReevaluationCycleResult, error) {
	d.mu.RLock()
	impl := d.impl
	d.mu.RUnlock()
	if impl == nil {
		return TrackingReevaluationCycleResult{}, nil
	}
	return impl.ProcessPendingTrackingReevaluations(ctx)
}

type trackingReevaluationCycleJob struct{ cycle TrackingReevaluationCycle }

func (j trackingReevaluationCycleJob) Run(ctx context.Context) error {
	result, err := j.cycle.ProcessPendingTrackingReevaluations(ctx)
	if err != nil {
		return fmt.Errorf("tracking reevaluation cycle: %w", err)
	}
	if result.Scanned > 0 {
		slog.Info("tracking reevaluation cycle completed", "scanned", result.Scanned, "claimed", result.Claimed, "completed", result.Completed, "failed", result.Failed)
	}
	return nil
}

func RegisterTrackingReevaluationCycle(scheduler *background.Scheduler, cycle TrackingReevaluationCycle, interval, timeout time.Duration) (background.JobDefinition, error) {
	if scheduler == nil || cycle == nil {
		return background.JobDefinition{}, fmt.Errorf("register tracking reevaluation cycle: dependencies must not be nil")
	}
	definition := background.JobDefinition{
		ID: TrackingReevaluationCycleJobID, Interval: interval, Timeout: timeout,
		Enabled: true, OverlapPolicy: background.OverlapPolicySkip,
	}
	if err := scheduler.Register(definition, trackingReevaluationCycleJob{cycle: cycle}); err != nil {
		return background.JobDefinition{}, fmt.Errorf("register tracking reevaluation cycle: %w", err)
	}
	return definition, nil
}
