package readcap_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/readcap"
)

func TestListDescriptorsIsStableAndDeterministic(t *testing.T) {
	f := newFixture(t)

	first := f.readcap.ListDescriptors()
	second := f.readcap.ListDescriptors()

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("ListDescriptors is not deterministic: %+v vs %+v", first, second)
	}

	wantIDs := []string{
		readcap.CapabilityWorkItemRead,
		readcap.CapabilityResolveExternal,
		readcap.CapabilitySearch,
		readcap.CapabilityAttentionReadForWorkItem,
		readcap.CapabilityHistoryReadForWorkItem,
		readcap.CapabilityWorkItemList,
	}
	if len(first) != len(wantIDs) {
		t.Fatalf("expected exactly %d descriptors, got %d", len(wantIDs), len(first))
	}
	for i, want := range wantIDs {
		if first[i].ID != want {
			t.Fatalf("descriptor %d: expected ID %q, got %q", i, want, first[i].ID)
		}
		if first[i].Origin != readcap.OriginCore {
			t.Fatalf("descriptor %q: expected CORE origin, got %q", first[i].ID, first[i].Origin)
		}
		if first[i].Description == "" || first[i].InputSemantics == "" || first[i].ResultSemantics == "" {
			t.Fatalf("descriptor %q has an empty semantics field: %+v", first[i].ID, first[i])
		}
	}
}

func TestCoreDescriptorsExplainSelectionResultsAndLimitations(t *testing.T) {
	f := newFixture(t)
	byID := make(map[string]readcap.Descriptor)
	for _, descriptor := range f.readcap.ListDescriptors() {
		byID[descriptor.ID] = descriptor
	}

	expect := map[string][]string{
		readcap.CapabilityWorkItemRead:             {"dependencies", "Workflow current step and Tracked Item progression are independent", "does not read Planner Todos"},
		readcap.CapabilityResolveExternal:          {"exact external-system", "ambiguity fails closed", "later domain read", "proposed name/title of a new entity being created"},
		readcap.CapabilitySearch:                   {"candidate Work Items or Planner Todos", "identity hints", "authoritative negative evidence"},
		readcap.CapabilityAttentionReadForWorkItem: {"Attention is derived state", "valid empty Issues list", "does not read or mutate"},
		readcap.CapabilityHistoryReadForWorkItem:   {"History is explanatory", "at most 10", "must not be treated"},
		readcap.CapabilityWorkItemList:             {"provides no particular title or ID", "Total reports", "Follow with workitem.read"},
	}
	for capabilityID, markers := range expect {
		descriptor, ok := byID[capabilityID]
		if !ok {
			t.Fatalf("missing descriptor %q", capabilityID)
		}
		text := descriptor.Description + " " + descriptor.InputSemantics + " " + descriptor.ResultSemantics
		for _, marker := range markers {
			if !strings.Contains(text, marker) {
				t.Errorf("descriptor %q does not explain %q", capabilityID, marker)
			}
		}
	}
}

func TestListDescriptorsReturnsACopy(t *testing.T) {
	f := newFixture(t)

	got := f.readcap.ListDescriptors()
	got[0].ID = "mutated"

	again := f.readcap.ListDescriptors()
	if again[0].ID == "mutated" {
		t.Fatal("mutating a returned Descriptor slice affected the package's own registry")
	}
}

func TestExecuteRejectsUnknownCapability(t *testing.T) {
	f := newFixture(t)

	_, err := f.readcap.Execute(f.ctx, readcap.Request{CapabilityID: "does.not.exist"})
	if !errors.Is(err, readcap.ErrUnknownCapability) {
		t.Fatalf("expected ErrUnknownCapability, got %v", err)
	}
}

func TestExecuteRejectsWrongShapedSerializedInputBeforeAnyDomainRead(t *testing.T) {
	f := newFixture(t)

	// A caller (M16) sends plain JSON, not a concrete Go input type. A
	// SearchInput-shaped JSON object decoded against workitem.read leaves
	// WorkItemID at its zero value (unknown JSON fields are simply ignored,
	// same as any other Go JSON decode); that must still be caught by
	// workitem.read's own field validation before any domain read runs, and
	// must never panic on the missing WorkItemID field.
	_, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityWorkItemRead,
		Input:        mustEncode(t, readcap.SearchInput{Query: "x"}),
	})
	if !errors.Is(err, readcap.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}

	_, err = f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityResolveExternal,
		Input:        mustEncode(t, readcap.WorkItemReadInput{WorkItemID: "x"}),
	})
	if !errors.Is(err, readcap.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}

	// Empty input.
	_, err = f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilitySearch,
		Input:        nil,
	})
	if !errors.Is(err, readcap.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}

	// Malformed JSON.
	_, err = f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilitySearch,
		Input:        []byte(`{"query":`),
	})
	if !errors.Is(err, readcap.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for malformed JSON, got %v", err)
	}

	// Trailing content after a valid JSON value.
	_, err = f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilitySearch,
		Input:        []byte(`{"query":"x"}{}`),
	})
	if !errors.Is(err, readcap.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for trailing JSON content, got %v", err)
	}
}
