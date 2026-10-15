package readcap_test

import (
	"errors"
	"testing"
	"time"

	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

func TestAttentionReadForWorkItem(t *testing.T) {
	f := newFixture(t)
	item := f.workItem(t, "WorkItem With Attention")

	due := time.Now().UTC().Add(-1 * time.Hour)
	expected := workitem.LifecycleCompleted
	_, err := f.tracking.CreateMonitorPoint(f.ctx, tracking.CreateMonitorPointInput{
		WorkItemID: item.ID, Kind: tracking.MonitorFinalTarget, Title: "Final",
		DueAt: due, Timezone: "UTC", ExpectedWorkItemLifecycle: &expected,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Reconcile attention so an open issue exists
	_, err = f.attention.Reconcile(f.ctx, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	// Execute read capability
	result, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityAttentionReadForWorkItem,
		Input:        mustEncode(t, readcap.AttentionReadForWorkItemInput{WorkItemID: item.ID}),
	})
	if err != nil {
		t.Fatal(err)
	}

	output := result.Output.(readcap.AttentionReadForWorkItemOutput)
	if output.WorkItemID != item.ID {
		t.Fatalf("expected workItemID %q, got %q", item.ID, output.WorkItemID)
	}
	if output.OpenIssueCount != 1 {
		t.Fatalf("expected 1 open issue, got %d", output.OpenIssueCount)
	}
	if output.HighestActiveSeverity == nil || *output.HighestActiveSeverity != attention.SeverityWarning {
		t.Fatalf("expected WARNING severity, got %v", output.HighestActiveSeverity)
	}
	if len(output.Issues) != 1 {
		t.Fatalf("expected 1 issue detail, got %d", len(output.Issues))
	}
	if output.Issues[0].Type != attention.IssueMonitorOverdueUnmet {
		t.Fatalf("expected MONITOR_OVERDUE_UNMET, got %v", output.Issues[0].Type)
	}
	if output.Issues[0].Disposition != attention.DispositionNeedsAction {
		t.Fatalf("expected NEEDS_ACTION disposition, got %v", output.Issues[0].Disposition)
	}
}

func TestAttentionReadForWorkItemRejectsEmptyID(t *testing.T) {
	f := newFixture(t)
	_, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityAttentionReadForWorkItem,
		Input:        mustEncode(t, readcap.AttentionReadForWorkItemInput{WorkItemID: "  "}),
	})
	if !errors.Is(err, readcap.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}
