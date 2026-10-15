package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/ai"
)

func askMutation(t *testing.T, candidatePayload string) (Result, *fakeExecutor, *fakeReader, error) {
	t.Helper()
	p2Output := `{"version":1,"kind":"MUTATION_CANDIDATE","text":"Rename it.","candidate":` + candidatePayload + `}`
	service, reader, executor := newTestService(t, p2Output)
	result, err := service.Ask(context.Background(), "req-1", "Rename it", locator)
	return result, executor, reader, err
}

func TestMutationIntentRoutesToGenericPrompt2Candidate(t *testing.T) {
	result, executor, reader, err := askMutation(t, candidateJSON("Q4 Launch", `"Ship the Q4 release"`, "clearer name"))
	if err != nil {
		t.Fatal(err)
	}
	if len(executor.requests) != 2 || executor.requests[0].Action != ai.ActionRuntimeCapabilityDecision || executor.requests[1].Action != ai.ActionAssistantAsk {
		t.Fatalf("expected read_decision then ask: %+v", executor.requests)
	}
	if len(reader.reads) != 1 {
		t.Fatalf("both AI calls must use the single snapshot read, got %d reads", len(reader.reads))
	}
	if result.Candidate == nil || result.Candidate.CapabilityID != "work_item.update_details" || result.Base == nil || result.Base.ID != "wi-1" || result.ChangeApplied {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !strings.Contains(string(result.Candidate.Arguments), "Q4 Launch") {
		t.Fatalf("arguments must carry the model's values verbatim, opaque to this package: %s", result.Candidate.Arguments)
	}
	if result.Candidate.Reason == nil || *result.Candidate.Reason != "clearer name" {
		t.Fatalf("reason lost: %+v", result.Candidate)
	}
}

func TestPrompt2RequestUsesSameAuthoritativeContextAndSeparatesUserMessage(t *testing.T) {
	_, executor, _, _ := askMutation(t, candidateJSON("Q4 Launch", `null`, ""))
	p1, p2 := executor.requests[0], executor.requests[1]
	if p1.Messages[1].Content != p2.Messages[1].Content {
		t.Fatal("Prompt 1 and Prompt 2 must receive the identical authoritative context")
	}
	last := p2.Messages[len(p2.Messages)-1]
	if last.Role != ai.RoleUser || last.Content != "Rename it" {
		t.Fatalf("user message must stay a separate user message: %+v", last)
	}
	if !strings.Contains(string(p2.OutputSchema), `"additionalProperties":false`) {
		t.Fatal("candidate schema must be closed")
	}
}

// TestPrompt2SchemaIsGeneric proves the candidate schema and system prompt
// carry no product capability vocabulary: no operation enum, no WorkItem/
// TrackedItem/Planner-specific field names hardcoded anywhere in this
// package.
func TestPrompt2SchemaIsGeneric(t *testing.T) {
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(prompt2Schema, &schema); err != nil {
		t.Fatal(err)
	}
	candidateSchema, ok := schema.Properties["candidate"]
	if !ok {
		t.Fatal("schema missing candidate property")
	}
	for _, forbidden := range []string{
		"work_item", "tracked_item", "planner", "workitem", "operation", "proposed_title",
		"proposed_description", "target_item_title", "stage_title", "lifecycle",
	} {
		if strings.Contains(string(candidateSchema), forbidden) {
			t.Fatalf("candidate schema must not hardcode %q: %s", forbidden, candidateSchema)
		}
	}
	for _, forbidden := range []string{"work_item.", "tracked_item.", "planner.", "workitem."} {
		if strings.Contains(prompt2SystemPrompt, forbidden) {
			t.Fatalf("system prompt must not hardcode a capability ID prefix %q", forbidden)
		}
	}
	snap := snapshot()
	req, err := BuildPrompt2Request("r", "m", &snap, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range req.Messages {
		if strings.Contains(m.Content, "lastRevision") {
			t.Fatal("revision evidence must not be sent to the model")
		}
	}
}

func TestPromptGuidanceSeparatesCanonicalIntentFromDerivedEffects(t *testing.T) {
	for name, marker := range map[string]string{
		"workflow and tracked item independence": "Changing one never automatically changes the other",
		"due is not reminder":                    "A due date or due time is not itself a reminder",
		"reminder derives attention":             "background processing derives Attention and Notification",
		"monitor is not task":                    "Monitor Points are canonical expectations, not tasks",
		"notification is derived":                "Notification is a durable communication/presentation record derived",
		"proposal is review first":               "creates no domain truth until a human applies it",
	} {
		if !strings.Contains(ProductModelGuide, marker) {
			t.Errorf("global product guide is missing %s semantics", name)
		}
	}
	for name, marker := range map[string]string{
		"one proposal":               "Return at most one Proposal for the turn",
		"todo reminder ownership":    "one Planner Todo carrying due and reminder configuration",
		"no duplicate derived truth": "Do not separately create Attention or Notification",
		"one plan for many changes":  "propose them as one plan of ordered operations (one Proposal, applied all-or-nothing)",
		"never multiple proposals":   "Never emit multiple Proposals for one request",
		"no invented operations":     "do not invent an operation for it",
	} {
		if !strings.Contains(SynthesisGuidance, marker) {
			t.Errorf("synthesis guide is missing %s semantics", name)
		}
	}
}

// TestPrompt2RendersArbitraryCapabilityWithoutCodeChange proves that a
// capability this package has never seen before is rendered and accepted
// purely from backend-supplied descriptor data - adding capability #11
// requires no change to this package.
func TestPrompt2RendersArbitraryCapabilityWithoutCodeChange(t *testing.T) {
	descriptors := (&fakePropose{}).ListDescriptors()
	req, err := BuildPrompt2Request("r", "m", nil, nil, descriptors)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.Messages[0].Content, "planner.item.create") {
		t.Fatal("expected the dynamically supplied capability ID to appear in the rendered prompt")
	}

	cand, err := DecodeCandidate([]byte(`{"version":1,"capability_id":"planner.item.create","arguments":{"title":"x"},"reason":""}`))
	if err != nil {
		t.Fatal(err)
	}
	if cand.CapabilityID != "planner.item.create" {
		t.Fatalf("unexpected capability_id: %s", cand.CapabilityID)
	}
}

func TestNonMutationKindsNeverProduceCandidate(t *testing.T) {
	for _, kind := range []string{"ANSWER", "UNSUPPORTED"} {
		service, _, executor := newTestService(t, `{"version":1,"kind":"`+kind+`","text":"x","candidate":null}`)
		r, err := service.Ask(context.Background(), "r", "hi", locator)
		if err != nil || r.Candidate != nil || len(executor.requests) != 2 {
			t.Fatalf("%s: %+v err=%v requests=%d", kind, r, err, len(executor.requests))
		}
	}
}

func TestInvalidCandidatesAreRejected(t *testing.T) {
	for name, output := range map[string]string{
		"not json":              `nope`,
		"bad version":           `{"version":2,"capability_id":"work_item.update_details","arguments":{},"reason":""}`,
		"missing version":       `{"capability_id":"work_item.update_details","arguments":{},"reason":""}`,
		"missing capability_id": `{"version":1,"arguments":{},"reason":""}`,
		"blank capability_id":   `{"version":1,"capability_id":"   ","arguments":{},"reason":""}`,
		"arguments not object":  `{"version":1,"capability_id":"work_item.update_details","arguments":"nope","reason":""}`,
		"arguments is array":    `{"version":1,"capability_id":"work_item.update_details","arguments":[],"reason":""}`,
		"trailing json":         candidateJSON("a", `null`, "") + ` {}`,
		"unknown top field":     `{"version":1,"capability_id":"work_item.update_details","arguments":{},"reason":"","extra":true}`,
		"reason too long":       `{"version":1,"capability_id":"work_item.update_details","arguments":{},"reason":"` + strings.Repeat("d", maxCandidateReasonRunes+1) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			r, _, _, err := askMutation(t, output)
			if !errors.Is(err, ErrMalformedResult) || r.Candidate != nil {
				t.Fatalf("expected malformed rejection, got %+v err=%v", r, err)
			}
		})
	}
}

func TestValidCandidateWithEmptyArgumentsIsAccepted(t *testing.T) {
	r, _, _, err := askMutation(t, `{"version":1,"capability_id":"planner.item.create","arguments":{}}`)
	if err != nil {
		t.Fatal(err)
	}
	if r.Candidate == nil || r.Candidate.CapabilityID != "planner.item.create" {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestValidCandidateWithStringEncodedArgumentsIsAccepted(t *testing.T) {
	r, _, _, err := askMutation(t, `{"version":1,"capability_id":"planner.item.create","arguments":"{\"title\":\"Buy milk\",\"date\":\"2026-09-24\"}","reason":null}`)
	if err != nil {
		t.Fatal(err)
	}
	if r.Candidate == nil || r.Candidate.CapabilityID != "planner.item.create" {
		t.Fatalf("unexpected result: %+v", r)
	}
	if !strings.Contains(string(r.Candidate.Arguments), "Buy milk") {
		t.Fatalf("arguments must be unmarshaled into valid json object: %s", r.Candidate.Arguments)
	}
}

func TestPrompt2RuntimeFailureIsRedactedAndProducesNoCandidate(t *testing.T) {
	service, _, executor := newTestService(t, "")
	executor.err = errors.New("secret-provider-detail")
	r, err := service.Ask(context.Background(), "r", "Rename it", locator)
	if !errors.Is(err, ErrRuntime) || strings.Contains(err.Error(), "secret-provider-detail") || r.Candidate != nil {
		t.Fatalf("got %+v err=%v", r, err)
	}
}

func TestAskPromptRoutesUnsupportedRequests(t *testing.T) {
	if !strings.Contains(prompt2SystemPrompt, "UNSUPPORTED") {
		t.Fatal("prompt must instruct returning UNSUPPORTED for unsupported requests")
	}
}
