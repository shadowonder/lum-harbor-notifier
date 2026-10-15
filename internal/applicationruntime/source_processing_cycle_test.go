package applicationruntime

import (
	"context"
	"testing"
	"time"

	"lum-harbor-notifier/internal/background"
)

type mockSourceCycle struct {
	result SourceProcessingCycleResult
	err    error
	called bool
}

func (m *mockSourceCycle) ProcessPendingSources(ctx context.Context) (SourceProcessingCycleResult, error) {
	m.called = true
	return m.result, m.err
}

func TestRegisterSourceProcessingCycle(t *testing.T) {
	scheduler := background.NewScheduler()
	mock := &mockSourceCycle{
		result: SourceProcessingCycleResult{
			Scanned:   2,
			Claimed:   2,
			Completed: 2,
		},
	}

	def, err := RegisterSourceProcessingCycle(scheduler, mock, 10*time.Millisecond, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error registering cycle: %v", err)
	}
	if def.ID != SourceProcessingCycleJobID {
		t.Fatalf("expected job ID %q, got %q", SourceProcessingCycleJobID, def.ID)
	}

	// Nil scheduler check
	if _, err := RegisterSourceProcessingCycle(nil, mock, 10*time.Millisecond, 50*time.Millisecond); err == nil {
		t.Fatal("expected error on nil scheduler")
	}

	// Nil cycle check
	if _, err := RegisterSourceProcessingCycle(scheduler, nil, 10*time.Millisecond, 50*time.Millisecond); err == nil {
		t.Fatal("expected error on nil cycle")
	}
}

func TestDeferredSourceProcessingCycle(t *testing.T) {
	deferred := &DeferredSourceProcessingCycle{}

	// When impl is nil, should return zero without error
	res, err := deferred.ProcessPendingSources(context.Background())
	if err != nil {
		t.Fatalf("expected nil error when uninitialized, got: %v", err)
	}
	if res.Scanned != 0 {
		t.Fatalf("expected 0 scanned, got %d", res.Scanned)
	}

	// Once impl is set, it delegates properly
	mock := &mockSourceCycle{
		result: SourceProcessingCycleResult{
			Scanned:   5,
			Claimed:   5,
			Completed: 5,
		},
	}
	deferred.Set(mock)

	res, err = deferred.ProcessPendingSources(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if !mock.called {
		t.Fatal("expected mock to be called")
	}
	if res.Scanned != 5 || res.Completed != 5 {
		t.Fatalf("expected 5 scanned and completed, got %+v", res)
	}
}
