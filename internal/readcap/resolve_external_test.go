package readcap_test

import (
	"errors"
	"testing"

	"lum-harbor-notifier/internal/readcap"
)

func TestResolveExternalExactIdentityMatch(t *testing.T) {
	f := newFixture(t)
	item := f.externalPlannerItem(t, "APPLE_REMINDER:instance-1", "reminder-abc", "Buy milk")

	result, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityResolveExternal,
		Input:        mustEncode(t, readcap.ResolveExternalInput{System: "APPLE_REMINDER:instance-1", ExternalID: "reminder-abc"}),
	})
	if err != nil {
		t.Fatal(err)
	}

	output := result.Output.(readcap.ResolveExternalOutput)
	if output.EntityID != item.ID {
		t.Fatalf("expected entity ID %q, got %q", item.ID, output.EntityID)
	}
	if output.EntityType != "PLANNER_ITEM" {
		t.Fatalf("expected entity type PLANNER_ITEM, got %q", output.EntityType)
	}
}

func TestResolveExternalSystemIsolation(t *testing.T) {
	f := newFixture(t)
	f.externalPlannerItem(t, "APPLE_REMINDER:instance-1", "reminder-abc", "Buy milk")

	// Same external ID, different system (e.g. a different Reminders
	// Instance) must not resolve to instance-1's entity.
	_, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityResolveExternal,
		Input:        mustEncode(t, readcap.ResolveExternalInput{System: "APPLE_REMINDER:instance-2", ExternalID: "reminder-abc"}),
	})
	if !errors.Is(err, readcap.ErrEntityNotFound) {
		t.Fatalf("expected ErrEntityNotFound for a different system, got %v", err)
	}
}

func TestResolveExternalNotFoundNeverFallsBackToSearch(t *testing.T) {
	f := newFixture(t)

	_, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityResolveExternal,
		Input:        mustEncode(t, readcap.ResolveExternalInput{System: "APPLE_REMINDER:instance-1", ExternalID: "no-such-id"}),
	})
	if !errors.Is(err, readcap.ErrEntityNotFound) {
		t.Fatalf("expected ErrEntityNotFound, got %v", err)
	}
}

func TestResolveExternalRejectsEmptyInput(t *testing.T) {
	f := newFixture(t)

	cases := []readcap.ResolveExternalInput{
		{System: "x", ExternalID: ""},
		{System: "  ", ExternalID: "  "},
		{System: "", ExternalID: ""},
	}
	for _, input := range cases {
		_, err := f.readcap.Execute(f.ctx, readcap.Request{CapabilityID: readcap.CapabilityResolveExternal, Input: mustEncode(t, input)})
		if !errors.Is(err, readcap.ErrInvalidInput) {
			t.Fatalf("input %+v: expected ErrInvalidInput, got %v", input, err)
		}
	}
}

func TestResolveExternalNoSystemUniqueMatch(t *testing.T) {
	f := newFixture(t)
	item := f.externalPlannerItem(t, "APPLE_REMINDER:instance-1", "reminder-unique", "Buy groceries")

	result, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityResolveExternal,
		Input:        mustEncode(t, readcap.ResolveExternalInput{ExternalID: "reminder-unique"}),
	})
	if err != nil {
		t.Fatal(err)
	}

	output := result.Output.(readcap.ResolveExternalOutput)
	if output.EntityID != item.ID {
		t.Fatalf("expected entity ID %q, got %q", item.ID, output.EntityID)
	}
	if output.EntityType != "PLANNER_ITEM" {
		t.Fatalf("expected entity type PLANNER_ITEM, got %q", output.EntityType)
	}
}

func TestResolveExternalNoSystemAmbiguousMatchFailsClosed(t *testing.T) {
	f := newFixture(t)
	f.externalPlannerItem(t, "SYSTEM_A", "shared-ext-id", "Item A")
	f.externalPlannerItem(t, "SYSTEM_B", "shared-ext-id", "Item B")

	_, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityResolveExternal,
		Input:        mustEncode(t, readcap.ResolveExternalInput{ExternalID: "shared-ext-id"}),
	})
	if !errors.Is(err, readcap.ErrAmbiguousExternalID) {
		t.Fatalf("expected ErrAmbiguousExternalID, got %v", err)
	}
}

func TestResolveExternalCreatesNoMutation(t *testing.T) {
	f := newFixture(t)
	f.externalPlannerItem(t, "APPLE_REMINDER:instance-1", "reminder-abc", "Buy milk")

	beforeEntities := f.count(t, "SELECT COUNT(*) FROM entities")
	beforeRefs := f.count(t, "SELECT COUNT(*) FROM external_refs")
	beforeRevisions := f.count(t, "SELECT COUNT(*) FROM revisions")

	if _, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityResolveExternal,
		Input:        mustEncode(t, readcap.ResolveExternalInput{System: "APPLE_REMINDER:instance-1", ExternalID: "reminder-abc"}),
	}); err != nil {
		t.Fatal(err)
	}
	// Also exercise the not-found path, which still must not mutate.
	if _, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityResolveExternal,
		Input:        mustEncode(t, readcap.ResolveExternalInput{System: "APPLE_REMINDER:instance-1", ExternalID: "missing"}),
	}); err == nil {
		t.Fatal("expected an error for a missing identity")
	}

	if got := f.count(t, "SELECT COUNT(*) FROM entities"); got != beforeEntities {
		t.Fatalf("expected no new entities, before=%d after=%d", beforeEntities, got)
	}
	if got := f.count(t, "SELECT COUNT(*) FROM external_refs"); got != beforeRefs {
		t.Fatalf("expected no new external_refs, before=%d after=%d", beforeRefs, got)
	}
	if got := f.count(t, "SELECT COUNT(*) FROM revisions"); got != beforeRevisions {
		t.Fatalf("expected no new revisions, before=%d after=%d", beforeRevisions, got)
	}
}
