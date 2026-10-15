// Package applicationruntime owns production startup and shutdown ordering for
// the already-defined Background Scheduler and Plugin Host runtimes.
package applicationruntime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"lum-harbor-notifier/internal/background"
	"lum-harbor-notifier/internal/plugin"
)

// PluginHost is the narrow per-extension lifecycle used by production
// composition. It intentionally does not add a global Host start phase.
type PluginHost interface {
	Start(context.Context, string) error
	Shutdown(context.Context) error
}

// Runtime owns application-level lifecycle order without changing the
// Scheduler or Plugin Host contracts.
type Runtime struct {
	scheduler       *background.Scheduler
	host            PluginHost
	extensionIDs    []string
	shutdownTimeout time.Duration

	mu       sync.Mutex
	started  bool
	terminal bool
	result   error
	done     chan struct{}
	once     sync.Once
}

func New(scheduler *background.Scheduler, host PluginHost, instances []plugin.Instance, shutdownTimeout time.Duration) (*Runtime, error) {
	if scheduler == nil {
		return nil, fmt.Errorf("application runtime scheduler must not be nil")
	}
	if host == nil {
		return nil, fmt.Errorf("application runtime plugin host must not be nil")
	}
	if shutdownTimeout <= 0 {
		return nil, fmt.Errorf("application runtime shutdown timeout must be greater than zero")
	}
	return &Runtime{
		scheduler:       scheduler,
		host:            host,
		extensionIDs:    requiredExtensionIDs(instances),
		shutdownTimeout: shutdownTimeout,
		done:            make(chan struct{}),
	}, nil
}

// Start starts each selected extension once, in first-seen instance order,
// before the Scheduler can run dependent work and before Wails enters its
// normal run loop.
func (r *Runtime) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("application runtime start context must not be nil")
	}
	r.mu.Lock()
	if r.started || r.terminal {
		r.mu.Unlock()
		return fmt.Errorf("application runtime cannot start more than once")
	}
	r.started = true
	r.mu.Unlock()

	for _, extensionID := range r.extensionIDs {
		if err := r.host.Start(ctx, extensionID); err != nil {
			cleanupErr := r.cleanupFailedStart()
			return errors.Join(fmt.Errorf("start selected plugin extension %q: %w", extensionID, err), cleanupErr)
		}
	}
	if err := r.scheduler.Start(ctx); err != nil {
		cleanupErr := r.cleanupFailedStart()
		return errors.Join(fmt.Errorf("start application background scheduler: %w", err), cleanupErr)
	}
	return nil
}

func requiredExtensionIDs(instances []plugin.Instance) []string {
	extensionIDs := make([]string, 0, len(instances))
	seen := make(map[string]struct{}, len(instances))
	for _, instance := range instances {
		if _, exists := seen[instance.ExtensionID]; exists {
			continue
		}
		seen[instance.ExtensionID] = struct{}{}
		extensionIDs = append(extensionIDs, instance.ExtensionID)
	}
	return extensionIDs
}

// Shutdown starts the ordered shutdown exactly once and waits no longer than
// the configured application budget. If the budget expires, safe draining
// continues so callers can later Wait without closing Host or DB too early.
func (r *Runtime) Shutdown() error {
	r.initiateShutdown()
	timer := time.NewTimer(r.shutdownTimeout)
	defer timer.Stop()
	select {
	case <-r.done:
		return r.shutdownResult()
	case <-timer.C:
		return fmt.Errorf("application runtime shutdown: %w", context.DeadlineExceeded)
	}
}

// Wait blocks until Scheduler runners have exited and Plugin Host shutdown has
// completed. The composition root calls this after Wails returns and before DB
// close, preserving persistence lifetime even after a bounded callback timeout.
func (r *Runtime) Wait() error {
	r.initiateShutdown()
	<-r.done
	return r.shutdownResult()
}

func (r *Runtime) initiateShutdown() {
	r.once.Do(func() {
		go r.runShutdown()
	})
}

func (r *Runtime) runShutdown() {
	budget, cancel := context.WithTimeout(context.Background(), r.shutdownTimeout)
	defer cancel()

	// Scheduler must stop accepting work and all runners must exit before Host
	// shutdown. A timeout is reported, then an unbounded safety drain preserves
	// Host/DB lifetime rather than pretending an uncooperative Job has stopped.
	schedulerErr := r.scheduler.Shutdown(budget)
	if schedulerErr != nil {
		_ = r.scheduler.Shutdown(context.Background())
	}

	hostContext := budget
	hostCancel := func() {}
	if budget.Err() != nil {
		hostContext, hostCancel = context.WithTimeout(context.Background(), r.shutdownTimeout)
	}
	defer hostCancel()
	hostErr := r.host.Shutdown(hostContext)

	r.finish(errors.Join(schedulerErr, hostErr))
}

func (r *Runtime) cleanupFailedStart() error {
	var cleanupErr error
	r.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), r.shutdownTimeout)
		defer cancel()
		schedulerErr := r.scheduler.Shutdown(ctx)
		hostErr := r.host.Shutdown(ctx)
		cleanupErr = errors.Join(schedulerErr, hostErr)
		r.finish(cleanupErr)
	})
	return cleanupErr
}

func (r *Runtime) finish(result error) {
	r.mu.Lock()
	r.result = result
	r.terminal = true
	close(r.done)
	r.mu.Unlock()
}

func (r *Runtime) shutdownResult() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.result
}
