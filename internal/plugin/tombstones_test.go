package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func newTombstoneTestProcess() *pluginProcess {
	return &pluginProcess{
		extension: Extension{Manifest: Manifest{ID: "tombstone-test"}},
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		status:    processRunning,
		pending:   make(map[string]chan callResult),
	}
}

func TestTimeoutCreatesRetiredTombstone(t *testing.T) {
	process := newTombstoneTestProcess()
	process.pending["request-1"] = make(chan callResult, 1)
	if !process.removePending("request-1", true) {
		t.Fatal("expected pending request to be removed")
	}
	process.mu.Lock()
	defer process.mu.Unlock()
	if !process.retired.contains("request-1", time.Now()) || process.retired.len() != 1 {
		t.Fatalf("expected one retained tombstone, got %d", process.retired.len())
	}
}

func TestLateResponseWithinRetentionIsDiscarded(t *testing.T) {
	process := newTombstoneTestProcess()
	now := time.Now()
	process.retired.retire("request-1", now)
	if err := process.handleResponse(rpcResponse{ID: "request-1", Result: json.RawMessage("{}")}, now.Add(time.Second)); err != nil {
		t.Fatalf("late response: %v", err)
	}
	if process.status != processRunning || process.retired.len() != 0 {
		t.Fatalf("late response changed runtime: status=%s retired=%d", process.status, process.retired.len())
	}
}

func TestRetiredTombstoneExpiresAndVeryLateResponseIsProtocolFailure(t *testing.T) {
	process := newTombstoneTestProcess()
	now := time.Now()
	process.retired.retire("request-1", now)
	process.mu.Lock()
	process.retired.cleanup(now.Add(retiredRequestTTL))
	remaining := process.retired.len()
	process.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("expected expired tombstone cleanup, got %d", remaining)
	}
	err := process.handleResponse(rpcResponse{ID: "request-1", Result: json.RawMessage("{}")}, now.Add(retiredRequestTTL+time.Second))
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("expected unknown-ID protocol failure, got %v", err)
	}
	process.fail(err, false)
	if process.status != processUnhealthy {
		t.Fatalf("expected unhealthy process, got %s", process.status)
	}
}

func TestRetiredTombstonesAreBoundedAndEvictOldest(t *testing.T) {
	var tombstones retiredRequestTombstones
	now := time.Now()
	for index := 0; index < maxRetiredRequestEntries+200; index++ {
		tombstones.retire(fmt.Sprintf("request-%05d", index), now.Add(time.Duration(index)*time.Nanosecond))
	}
	if tombstones.len() != maxRetiredRequestEntries {
		t.Fatalf("expected bound %d, got %d", maxRetiredRequestEntries, tombstones.len())
	}
	if tombstones.contains("request-00000", now.Add(time.Second)) {
		t.Fatal("oldest tombstone was not evicted")
	}
	oldestRetained := fmt.Sprintf("request-%05d", 200)
	if !tombstones.contains(oldestRetained, now.Add(time.Second)) {
		t.Fatalf("expected deterministic oldest retained tombstone %q", oldestRetained)
	}
}

func TestManyConcurrentCancellationsRemainBounded(t *testing.T) {
	process := newTombstoneTestProcess()
	const total = maxRetiredRequestEntries + 500
	for index := 0; index < total; index++ {
		process.pending[fmt.Sprintf("request-%05d", index)] = make(chan callResult, 1)
	}
	var wait sync.WaitGroup
	for index := 0; index < total; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			process.removePending(fmt.Sprintf("request-%05d", index), true)
		}(index)
	}
	wait.Wait()
	process.mu.Lock()
	defer process.mu.Unlock()
	if process.retired.len() != maxRetiredRequestEntries {
		t.Fatalf("expected bounded tombstones %d, got %d", maxRetiredRequestEntries, process.retired.len())
	}
}
