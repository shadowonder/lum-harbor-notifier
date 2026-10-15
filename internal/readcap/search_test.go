package readcap_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"lum-harbor-notifier/internal/readcap"
)

func TestSearchExactEntityIDTakesPrecedence(t *testing.T) {
	f := newFixture(t)
	item := f.workItem(t, "Ship the release")
	f.workItem(t, "Ship the release notes") // title also contains the query below

	result, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilitySearch,
		Input:        mustEncode(t, readcap.SearchInput{Query: item.ID}),
	})
	if err != nil {
		t.Fatal(err)
	}
	output := result.Output.(readcap.SearchOutput)
	if len(output.Candidates) != 1 {
		t.Fatalf("expected exactly one exact-ID candidate, got %+v", output.Candidates)
	}
	if output.Candidates[0].EntityID != item.ID || output.Candidates[0].MatchHint != "exact id" {
		t.Fatalf("unexpected candidate: %+v", output.Candidates[0])
	}
}

func TestSearchNormalizedTitleMatchAcrossWorkItemAndPlannerItem(t *testing.T) {
	f := newFixture(t)
	workItem := f.workItem(t, "Plan the Release")
	plannerItem := f.localTodo(t, "release notes")
	f.workItem(t, "Unrelated")

	result, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilitySearch,
		Input:        mustEncode(t, readcap.SearchInput{Query: "release"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	output := result.Output.(readcap.SearchOutput)
	if len(output.Candidates) != 2 {
		t.Fatalf("expected exactly 2 title matches, got %+v", output.Candidates)
	}
	ids := map[string]string{output.Candidates[0].EntityID: output.Candidates[0].EntityKind, output.Candidates[1].EntityID: output.Candidates[1].EntityKind}
	if kind, ok := ids[workItem.ID]; !ok || kind != "WORK_ITEM" {
		t.Fatalf("expected WorkItem %q among candidates: %+v", workItem.ID, output.Candidates)
	}
	if kind, ok := ids[plannerItem.ID]; !ok || kind != "PLANNER_ITEM" {
		t.Fatalf("expected PlannerItem %q among candidates: %+v", plannerItem.ID, output.Candidates)
	}
	for _, c := range output.Candidates {
		if c.MatchHint != "title match" {
			t.Fatalf("expected title match hint, got %+v", c)
		}
	}
}

func TestSearchIsBoundedByLimit(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 5; i++ {
		f.workItem(t, fmt.Sprintf("Bounded search candidate %d", i))
	}

	result, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilitySearch,
		Input:        mustEncode(t, readcap.SearchInput{Query: "Bounded search candidate", Limit: 2}),
	})
	if err != nil {
		t.Fatal(err)
	}
	output := result.Output.(readcap.SearchOutput)
	if len(output.Candidates) != 2 {
		t.Fatalf("expected exactly 2 candidates (limit), got %d: %+v", len(output.Candidates), output.Candidates)
	}
}

func TestSearchLimitIsHardCapped(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 25; i++ {
		f.workItem(t, fmt.Sprintf("Hard cap candidate %d", i))
	}

	result, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilitySearch,
		Input:        mustEncode(t, readcap.SearchInput{Query: "Hard cap candidate", Limit: 1000}),
	})
	if err != nil {
		t.Fatal(err)
	}
	output := result.Output.(readcap.SearchOutput)
	if len(output.Candidates) > 20 {
		t.Fatalf("expected result capped at the hard maximum, got %d candidates", len(output.Candidates))
	}
}

func TestSearchResultOrderingIsDeterministic(t *testing.T) {
	f := newFixture(t)
	f.workItem(t, "Zebra deterministic order")
	f.workItem(t, "Alpha deterministic order")
	f.localTodo(t, "Mango deterministic order")

	req := readcap.Request{CapabilityID: readcap.CapabilitySearch, Input: mustEncode(t, readcap.SearchInput{Query: "deterministic order"})}

	first, err := f.readcap.Execute(f.ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.readcap.Execute(f.ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Output, second.Output) {
		t.Fatalf("expected identical ordering across repeated calls:\n%+v\nvs\n%+v", first.Output, second.Output)
	}

	candidates := first.Output.(readcap.SearchOutput).Candidates
	if len(candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %+v", candidates)
	}
	if candidates[0].DisplayLabel != "Alpha deterministic order" {
		t.Fatalf("expected case-insensitive alphabetical order, got %+v", candidates)
	}
}

func TestSearchRejectsEmptyOrOversizedQuery(t *testing.T) {
	f := newFixture(t)

	_, err := f.readcap.Execute(f.ctx, readcap.Request{CapabilityID: readcap.CapabilitySearch, Input: mustEncode(t, readcap.SearchInput{Query: "   "})})
	if !errors.Is(err, readcap.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty query, got %v", err)
	}

	oversized := make([]byte, 201)
	for i := range oversized {
		oversized[i] = 'a'
	}
	_, err = f.readcap.Execute(f.ctx, readcap.Request{CapabilityID: readcap.CapabilitySearch, Input: mustEncode(t, readcap.SearchInput{Query: string(oversized)})})
	if !errors.Is(err, readcap.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for oversized query, got %v", err)
	}
}

func TestSearchNoMatchIsEmptyNotError(t *testing.T) {
	f := newFixture(t)

	result, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilitySearch,
		Input:        mustEncode(t, readcap.SearchInput{Query: "no such candidate exists anywhere"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	output := result.Output.(readcap.SearchOutput)
	if len(output.Candidates) != 0 {
		t.Fatalf("expected zero candidates, got %+v", output.Candidates)
	}
}

func TestSearchCreatesNoMutation(t *testing.T) {
	f := newFixture(t)
	f.workItem(t, "Read-only search target")

	beforeEntities := f.count(t, "SELECT COUNT(*) FROM entities")
	beforeRevisions := f.count(t, "SELECT COUNT(*) FROM revisions")

	if _, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilitySearch,
		Input:        mustEncode(t, readcap.SearchInput{Query: "search target"}),
	}); err != nil {
		t.Fatal(err)
	}

	if got := f.count(t, "SELECT COUNT(*) FROM entities"); got != beforeEntities {
		t.Fatalf("expected no new entities, before=%d after=%d", beforeEntities, got)
	}
	if got := f.count(t, "SELECT COUNT(*) FROM revisions"); got != beforeRevisions {
		t.Fatalf("expected no new revisions, before=%d after=%d", beforeRevisions, got)
	}
}
