package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/proposalcap"
	"lum-harbor-notifier/internal/readcap"
)

type fakeReader struct {
	items map[string]WorkItemSnapshot
	reads []string
	err   error
}

func (f *fakeReader) ReadWorkItem(_ context.Context, id string) (WorkItemSnapshot, error) {
	f.reads = append(f.reads, id)
	if f.err != nil {
		return WorkItemSnapshot{}, f.err
	}
	item, ok := f.items[id]
	if !ok {
		return WorkItemSnapshot{}, ErrWorkItemNotFound
	}
	return item, nil
}

type fakeReadSource struct {
	descriptors []readcap.Descriptor
}

func (f *fakeReadSource) ListDescriptors() []readcap.Descriptor { return f.descriptors }
func (f *fakeReadSource) Execute(_ context.Context, _ readcap.Request) (readcap.Result, error) {
	return readcap.Result{}, nil
}

// fakePropose is a minimal proposalcap.Source. work_item.update_details
// requires a base WorkItem (matching production wiring); a second,
// context-free capability exists so context-hint tests need no fixture edits.
type fakePropose struct{}

func (f *fakePropose) ListDescriptors() []proposalcap.Descriptor {
	return []proposalcap.Descriptor{
		{ID: "work_item.update_details", Summary: "Update a Work Item", RequiresWorkItemContext: true,
			Arguments: map[string]proposalcap.ArgumentSpec{
				"proposed_title":       {Type: proposalcap.ArgString, Required: true},
				"proposed_description": {Type: proposalcap.ArgString, Required: false},
			}},
		{ID: "planner.item.create", Summary: "Create a Planner item", RequiresWorkItemContext: false,
			Arguments: map[string]proposalcap.ArgumentSpec{"title": {Type: proposalcap.ArgString, Required: true}}},
	}
}

type fakeExecutor struct {
	requests []ai.Request
	p1Output string
	output   string
	err      error
}

func (f *fakeExecutor) Execute(_ context.Context, request ai.Request) (ai.Response, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return ai.Response{}, f.err
	}
	if request.Action == ai.ActionRuntimeCapabilityDecision {
		out := f.p1Output
		if out == "" {
			out = `{"version":1,"decision":"FINAL","calls":[]}`
		}
		return ai.Response{RequestID: request.RequestID, Action: request.Action, Output: json.RawMessage(out)}, nil
	}
	return ai.Response{RequestID: request.RequestID, Action: request.Action, Output: json.RawMessage(f.output)}, nil
}

func snapshot() WorkItemSnapshot {
	description := "Ship the Q4 release"
	return WorkItemSnapshot{
		ID: "wi-1", Title: "Q4 Release", Description: &description, Lifecycle: "ACTIVE",
		CreatedAt: time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC),
	}
}

func newTestService(t *testing.T, output string) (*Service, *fakeReader, *fakeExecutor) {
	t.Helper()
	reader := &fakeReader{items: map[string]WorkItemSnapshot{"wi-1": snapshot()}}
	executor := &fakeExecutor{output: output}
	engine, err := NewNativeRuntimeAdapter(executor, &fakeReadSource{}, &fakePropose{})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(reader, engine)
	if err != nil {
		t.Fatal(err)
	}
	return service, reader, executor
}

var locator = ContextLocator{Surface: SurfaceWorkItems, WorkItemID: "wi-1"}

func TestAskLoadsAuthoritativeWorkItemByIDAndUsesAssistantAsk(t *testing.T) {
	service, reader, executor := newTestService(t, `{"version":1,"kind":"ANSWER","text":"It is the Q4 release.","candidate":null}`)
	result, err := service.Ask(context.Background(), "req-1", "What is this about?", locator)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindAnswer || result.Text != "It is the Q4 release." || result.ContextLabel != "Q4 Release" || result.ChangeApplied {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(reader.reads) != 1 || reader.reads[0] != "wi-1" {
		t.Fatalf("expected the backend to read the selected ID, got %v", reader.reads)
	}
	if len(executor.requests) != 2 || executor.requests[0].Action != ai.ActionRuntimeCapabilityDecision || executor.requests[1].Action != ai.ActionAssistantAsk {
		t.Fatalf("expected read_decision then ask requests: %+v", executor.requests)
	}
	messages := executor.requests[1].Messages
	if messages[len(messages)-1].Role != ai.RoleUser || messages[len(messages)-1].Content != "What is this about?" {
		t.Fatalf("user message must be its own trailing user message: %+v", messages)
	}
	if !strings.Contains(messages[1].Content, "Q4 Release") || messages[1].Role != ai.RoleSystem {
		t.Fatalf("application context must be a separate system message: %+v", messages[1])
	}
}

func TestContextContainsOnlyRealWorkItemFields(t *testing.T) {
	snap := snapshot()
	request, err := BuildPrompt2Request("r", "hi", &snap, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	context := request.Messages[1].Content
	for _, want := range []string{`"id"`, `"title"`, `"description"`, `"lifecycleState"`, `"createdAt"`, `"updatedAt"`, `"completedAt"`} {
		if !strings.Contains(context, want) {
			t.Fatalf("missing real field %s in %s", want, context)
		}
	}
	for _, forbidden := range []string{"workflow", "branch", "dependenc", "health", "target", "monitor", "attention", "notification", "history"} {
		if strings.Contains(strings.ToLower(context), forbidden) {
			t.Fatalf("future-owned concept %q leaked into context: %s", forbidden, context)
		}
	}
}

func TestAuthoritativeContextStatesCurrentWeekday(t *testing.T) {
	// 2026-09-26 is a Saturday; relative requests such as "next Friday"
	// must not depend on the model deriving the weekday from a bare date.
	now := time.Date(2026, 9, 26, 14, 26, 0, 0, time.FixedZone("CDT", -5*60*60))
	context := ApplicationContext{Now: now}.FormatAuthoritativeContext(nil)
	if !strings.Contains(context, "- Current local weekday: Saturday\n") {
		t.Fatalf("authoritative context must state the weekday: %s", context)
	}
}

func TestSystemPromptForbidsInventingUnavailableDomains(t *testing.T) {
	if !strings.Contains(prompt2SystemPrompt, "Never invent workflow, dependencies, dates, health, or external state") {
		t.Fatal("system prompt must forbid inferring unavailable Lum Harbor state")
	}
}

func TestLocatorCarriesOnlyIdentityNotWorkItemFields(t *testing.T) {
	// Structural: ContextLocator has no title/description/lifecycle fields, so
	// frontend-rendered facts have nowhere to enter backend truth.
	locatorType := map[string]bool{"Surface": true, "WorkItemID": true}
	value, _ := json.Marshal(ContextLocator{})
	var fields map[string]any
	_ = json.Unmarshal(value, &fields)
	for name := range fields {
		if !locatorType[name] {
			t.Fatalf("unexpected locator field %s", name)
		}
	}
}

func TestMutationIntentIsRecognizedWithoutApplyingAnything(t *testing.T) {
	service, _, _ := newTestService(t, `{"version":1,"kind":"MUTATION_CANDIDATE","text":"Rewrite the description.","candidate":`+candidateJSON("Q4 Release", `"Shipped"`, "clearer")+`}`)
	result, err := service.Ask(context.Background(), "r", "Change the description to X", locator)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindMutationIntent || result.ChangeApplied {
		t.Fatalf("mutation intent must never report applied: %+v", result)
	}
}

func candidateJSON(title, description, reason string) string {
	return `{"version":1,"capability_id":"work_item.update_details","arguments":{"proposed_title":"` + title + `","proposed_description":` + description + `},"reason":"` + reason + `"}`
}

func TestGlobalOrContextFreeAskWorksWithoutSelection(t *testing.T) {
	service, reader, _ := newTestService(t, `{"version":1,"kind":"ANSWER","text":"Global answer without selection.","candidate":null}`)
	for _, l := range []ContextLocator{{}, {Surface: SurfaceDashboard}, {Surface: SurfacePlanner}} {
		result, err := service.Ask(context.Background(), "r", "hello", l)
		if err != nil || result.Kind != KindAnswer || result.Text != "Global answer without selection." {
			t.Fatalf("expected ANSWER for %+v, got %+v err=%v", l, result, err)
		}
		if result.Base != nil || result.ContextLabel != "" {
			t.Fatalf("expected nil base and empty context label, got %+v", result)
		}
	}
	if len(reader.reads) != 0 {
		t.Fatal("no workitem read should happen without a workitem ID")
	}
}

func TestMutationCandidateRejectedWhenBaseIsNil(t *testing.T) {
	service, _, _ := newTestService(t, `{"version":1,"kind":"MUTATION_CANDIDATE","text":"Rename something.","candidate":`+candidateJSON("Some Title", `null`, "test")+`}`)
	result, err := service.Ask(context.Background(), "r", "create something", ContextLocator{Surface: SurfaceDashboard})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != KindUnsupported || result.Candidate != nil {
		t.Fatalf("expected candidate without base to be downgraded to UNSUPPORTED, got %+v", result)
	}
}

func TestDeletedWorkItemIsHonest(t *testing.T) {
	service, _, executor := newTestService(t, `{}`)
	result, err := service.Ask(context.Background(), "r", "hi", ContextLocator{Surface: SurfaceWorkItems, WorkItemID: "gone"})
	if err != nil || result.Kind != KindNeedsContext || len(executor.requests) != 0 {
		t.Fatalf("expected honest needs-context, got %+v err=%v", result, err)
	}
}

func TestRuntimeFailureIsAnErrorWithoutLeakingProviderText(t *testing.T) {
	service, _, executor := newTestService(t, ``)
	executor.err = errors.New("secret-provider-detail")
	_, err := service.Ask(context.Background(), "r", "hi", locator)
	if !errors.Is(err, ErrRuntime) || strings.Contains(err.Error(), "secret-provider-detail") {
		t.Fatalf("expected redacted runtime error, got %v", err)
	}
}

func TestMalformedOutputIsRejected(t *testing.T) {
	for name, output := range map[string]string{
		"not json":         `hello`,
		"unknown kind":     `{"version":1,"kind":"CHAT","text":"x"}`,
		"missing text":     `{"version":1,"kind":"ANSWER","text":""}`,
		"bad version":      `{"version":2,"kind":"ANSWER","text":"x"}`,
		"illegal field":    `{"version":1,"kind":"ANSWER","text":"x","proposal":{}}`,
		"trailing data":    `{"version":1,"kind":"ANSWER","text":"x"} {}`,
		"missing version":  `{"kind":"ANSWER","text":"x"}`,
		"mutation no text": `{"version":1,"kind":"MUTATION_INTENT","text":" "}`,
	} {
		t.Run(name, func(t *testing.T) {
			service, _, _ := newTestService(t, output)
			if _, err := service.Ask(context.Background(), "r", "hi", locator); !errors.Is(err, ErrMalformedResult) {
				t.Fatalf("expected malformed rejection, got %v", err)
			}
		})
	}
}

func TestEmptyAndOversizedMessagesRejected(t *testing.T) {
	service, _, _ := newTestService(t, `{}`)
	if _, err := service.Ask(context.Background(), "r", "   ", locator); !errors.Is(err, ErrEmptyMessage) {
		t.Fatalf("got %v", err)
	}
	if _, err := service.Ask(context.Background(), "r", strings.Repeat("a", maxMessageRunes+1), locator); !errors.Is(err, ErrMessageTooLong) {
		t.Fatalf("got %v", err)
	}
}

// The Assistant read path must have no structural route to mutation.
func TestAssistantPackageHasNoMutationOrPersistenceDependencies(t *testing.T) {
	forbidden := []string{
		"lum-harbor-notifier/internal/workitem", "lum-harbor-notifier/internal/proposal",
		"lum-harbor-notifier/internal/planner", "lum-harbor-notifier/internal/database",
		"lum-harbor-notifier/internal/openai", "lum-harbor-notifier/internal/plugin",
		"database/sql",
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range parsed.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			for _, bad := range forbidden {
				if path == bad {
					t.Fatalf("%s imports forbidden %s", file, bad)
				}
			}
		}
		source, _ := os.ReadFile(file)
		if strings.Contains(string(source), "ProposeWorkItemUpdate") {
			t.Fatalf("%s must not reference Proposal production", file)
		}
	}
}
