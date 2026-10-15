package readcap_test

import (
	"errors"
	"testing"
	"time"

	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/readcap"
)

func TestHistoryReadForWorkItem(t *testing.T) {
	f := newFixture(t)
	item := f.workItem(t, "WorkItem With History")

	// Append some history events for this work item
	repo := history.NewRepository(f.db)
	_, err := repo.Append(f.ctx, history.Draft{
		Type:            history.EventWorkItemDetailsChanged,
		Scope:           history.ScopeWorkItem,
		Actor:           history.ActorUser,
		Summary:         "Updated title",
		SubjectEntityID: &item.ID,
		WorkItemID:      &item.ID,
		CorrelationID:   "test-corr-2",
		OccurredAt:      time.Now().UTC().Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}

	// Execute read capability
	result, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityHistoryReadForWorkItem,
		Input:        mustEncode(t, readcap.HistoryReadForWorkItemInput{WorkItemID: item.ID}),
	})
	if err != nil {
		t.Fatal(err)
	}

	output := result.Output.(readcap.HistoryReadForWorkItemOutput)
	if output.WorkItemID != item.ID {
		t.Fatalf("expected workItemID %q, got %q", item.ID, output.WorkItemID)
	}
	if len(output.Events) != 2 {
		t.Fatalf("expected 2 history events, got %d", len(output.Events))
	}
	if output.Events[0].Type != history.EventWorkItemDetailsChanged {
		t.Fatalf("expected newest event first (WORK_ITEM_DETAILS_CHANGED), got %s", output.Events[0].Type)
	}
	if output.Events[1].Type != history.EventWorkItemCreated {
		t.Fatalf("expected second event WORK_ITEM_CREATED, got %s", output.Events[1].Type)
	}
}

func TestHistoryReadForWorkItemRejectsEmptyID(t *testing.T) {
	f := newFixture(t)
	_, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityHistoryReadForWorkItem,
		Input:        mustEncode(t, readcap.HistoryReadForWorkItemInput{WorkItemID: "  "}),
	})
	if !errors.Is(err, readcap.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}
