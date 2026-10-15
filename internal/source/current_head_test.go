package source

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func testBatchOfKind(instanceID, sourceKind, externalID string, observedAt time.Time, sourceUpdatedAt *time.Time, payload string, cursor *string) FetchBatch {
	return FetchBatch{
		ExtensionID: "dummy", InstanceID: instanceID, CapabilityID: "source.fetch", NextCursor: cursor,
		Records: []Observation{{ExternalID: externalID, SourceKind: sourceKind, ObservedAt: observedAt, SourceUpdatedAt: sourceUpdatedAt, Payload: json.RawMessage(payload)}},
	}
}

func TestListCurrentHeadsBySourceKindReturnsOnlyRequestedKind(t *testing.T) {
	service, _ := newTestService(t)
	cursorA := "cursor-a"
	cursorB := "cursor-b"
	if _, err := ingestCurrent(service, testBatchOfKind("team-a", "reminder", "item-1", observedOne, nil, `{"title":"one"}`, &cursorA)); err != nil {
		t.Fatal(err)
	}
	if _, err := ingestCurrent(service, testBatchOfKind("team-a", "other.kind", "item-2", observedOne, nil, `{"x":1}`, &cursorB)); err != nil {
		t.Fatal(err)
	}

	heads, err := service.ListCurrentHeadsBySourceKind(context.Background(), "reminder")
	if err != nil {
		t.Fatal(err)
	}
	if len(heads) != 1 {
		t.Fatalf("expected exactly 1 head of kind reminder, got %d: %+v", len(heads), heads)
	}
	if heads[0].SourceKind != "reminder" || heads[0].ExternalID != "item-1" {
		t.Fatalf("unexpected head: %+v", heads[0])
	}
}

func TestListCurrentHeadsBySourceKindReturnsMultipleRecordsWithCurrentPayload(t *testing.T) {
	service, _ := newTestService(t)
	cursor := "cursor-1"
	if _, err := ingestCurrent(service, testBatchOfKind("team-a", "reminder", "item-1", observedOne, nil, `{"title":"first"}`, &cursor)); err != nil {
		t.Fatal(err)
	}
	if _, err := ingestCurrent(service, testBatchOfKind("team-a", "reminder", "item-2", observedOne, nil, `{"title":"second"}`, &cursor)); err != nil {
		t.Fatal(err)
	}

	heads, err := service.ListCurrentHeadsBySourceKind(context.Background(), "reminder")
	if err != nil {
		t.Fatal(err)
	}
	if len(heads) != 2 {
		t.Fatalf("expected 2 heads, got %d", len(heads))
	}
	byExternalID := map[string]CurrentSourceHead{}
	for _, h := range heads {
		byExternalID[h.ExternalID] = h
	}
	var decoded map[string]any
	if err := json.Unmarshal(byExternalID["item-1"].Payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["title"] != "first" {
		t.Fatalf("unexpected current payload for item-1: %s", byExternalID["item-1"].Payload)
	}
	if byExternalID["item-1"].CurrentRevisionID == "" || byExternalID["item-2"].CurrentRevisionID == "" {
		t.Fatalf("expected non-empty current revision IDs: %+v", byExternalID)
	}
}

func TestListCurrentHeadsBySourceKindReflectsLatestRevisionAsHead(t *testing.T) {
	service, _ := newTestService(t)
	cursor := "cursor-1"
	firstResult, err := ingestCurrent(service, testBatchOfKind("team-a", "reminder", "item-1", observedOne, nil, `{"title":"v1"}`, &cursor))
	if err != nil {
		t.Fatal(err)
	}
	firstRevisionID := firstResult.NewlyChangedRevisionIDs[0]

	secondResult, err := ingestCurrent(service, testBatchOfKind("team-a", "reminder", "item-1", observedTwo, nil, `{"title":"v2"}`, &cursor))
	if err != nil {
		t.Fatal(err)
	}
	secondRevisionID := secondResult.NewlyChangedRevisionIDs[0]
	if secondRevisionID == firstRevisionID {
		t.Fatal("expected a new revision for changed content")
	}

	heads, err := service.ListCurrentHeadsBySourceKind(context.Background(), "reminder")
	if err != nil {
		t.Fatal(err)
	}
	if len(heads) != 1 {
		t.Fatalf("expected exactly 1 head (same identity, updated), got %d", len(heads))
	}
	if heads[0].CurrentRevisionID != secondRevisionID {
		t.Fatalf("expected current head to be the latest revision %s, got %s", secondRevisionID, heads[0].CurrentRevisionID)
	}
	var decoded map[string]any
	if err := json.Unmarshal(heads[0].Payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["title"] != "v2" {
		t.Fatalf("expected current payload to reflect the latest revision, got %s", heads[0].Payload)
	}
}

func TestListCurrentHeadsBySourceKindEmptyForUnknownKind(t *testing.T) {
	service, _ := newTestService(t)
	cursor := "cursor-1"
	if _, err := ingestCurrent(service, testBatchOfKind("team-a", "reminder", "item-1", observedOne, nil, `{}`, &cursor)); err != nil {
		t.Fatal(err)
	}
	heads, err := service.ListCurrentHeadsBySourceKind(context.Background(), "no-such-kind")
	if err != nil {
		t.Fatal(err)
	}
	if len(heads) != 0 {
		t.Fatalf("expected no heads for an unused source kind, got %d", len(heads))
	}
}
