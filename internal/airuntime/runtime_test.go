package airuntime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/proposalcap"
	"lum-harbor-notifier/internal/readcap"
)

type testReads struct {
	descriptors []readcap.Descriptor
	execute     func(context.Context, readcap.Request) (readcap.Result, error)
}

func (f *testReads) ListDescriptors() []readcap.Descriptor {
	return append([]readcap.Descriptor(nil), f.descriptors...)
}
func (f *testReads) Execute(ctx context.Context, req readcap.Request) (readcap.Result, error) {
	return f.execute(ctx, req)
}

type testProposes []proposalcap.Descriptor

func (p testProposes) ListDescriptors() []proposalcap.Descriptor {
	return append([]proposalcap.Descriptor(nil), p...)
}

type modelFunc func(context.Context, ai.Request) (ai.Response, error)

func (f modelFunc) Execute(ctx context.Context, req ai.Request) (ai.Response, error) {
	return f(ctx, req)
}

func TestCapabilityHostComposesCorePluginAndProposeCatalog(t *testing.T) {
	host, err := NewCapabilityHost(&testReads{descriptors: []readcap.Descriptor{
		{ID: "entity.search", Description: "core", InputSemantics: "{}", ResultSemantics: "facts", Origin: readcap.OriginCore},
		{ID: "plugin:rally:feature.read", Description: "plugin", InputSemantics: "{}", ResultSemantics: "feature", Origin: readcap.OriginPlugin},
	}, execute: func(context.Context, readcap.Request) (readcap.Result, error) { return readcap.Result{}, nil }}, testProposes{{ID: "planner.item.create", Summary: "create", UseWhen: "requested"}})
	if err != nil {
		t.Fatal(err)
	}
	got := host.ListCapabilities()
	if len(got) != 3 || got[0].Kind != CapabilityRead || got[1].Origin != "PLUGIN" || got[2].Kind != CapabilityPropose {
		t.Fatalf("catalog = %+v", got)
	}
}

func TestCapabilityHostParallelReadsKeepRequestOrder(t *testing.T) {
	entered := make(chan string, 2)
	release := make(chan struct{})
	reads := &testReads{descriptors: []readcap.Descriptor{
		{ID: "a", Description: "a", InputSemantics: "{}", ResultSemantics: "a", Origin: readcap.OriginCore},
		{ID: "b", Description: "b", InputSemantics: "{}", ResultSemantics: "b", Origin: readcap.OriginPlugin},
	}, execute: func(ctx context.Context, req readcap.Request) (readcap.Result, error) {
		entered <- req.CapabilityID
		select {
		case <-release:
		case <-ctx.Done():
			return readcap.Result{}, ctx.Err()
		}
		return readcap.Result{CapabilityID: req.CapabilityID, Output: map[string]string{"id": req.CapabilityID}}, nil
	}}
	host, _ := NewCapabilityHost(reads, testProposes{})
	done := make(chan []CapabilityResult, 1)
	go func() {
		result, _ := host.ExecuteReads(context.Background(), []CapabilityCall{{ID: "b", Arguments: json.RawMessage(`{}`)}, {ID: "a", Arguments: json.RawMessage(`{}`)}})
		done <- result
	}()
	seen := map[string]bool{}
	for range 2 {
		seen[<-entered] = true
	}
	if !seen["a"] || !seen["b"] {
		t.Fatalf("calls did not overlap: %v", seen)
	}
	close(release)
	result := <-done
	if result[0].Call.ID != "b" || result[1].Call.ID != "a" {
		t.Fatalf("result order = %+v", result)
	}
}

func TestCapabilityHostRejectsUnknownAndProposeExecution(t *testing.T) {
	var calls int
	host, _ := NewCapabilityHost(&testReads{descriptors: []readcap.Descriptor{{ID: "read", Description: "read", InputSemantics: "{}", ResultSemantics: "x", Origin: readcap.OriginCore}}, execute: func(context.Context, readcap.Request) (readcap.Result, error) { calls++; return readcap.Result{}, nil }}, testProposes{{ID: "propose", Summary: "p"}})
	results, err := host.ExecuteReads(context.Background(), []CapabilityCall{{ID: "missing", Arguments: json.RawMessage(`{}`)}, {ID: "propose", Arguments: json.RawMessage(`{}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 0 || results[0].Error.Code != ErrorCapabilityNotFound || results[1].Error.Code != ErrorCapabilityNotAllowed {
		t.Fatalf("results=%+v calls=%d", results, calls)
	}
}

func TestDecisionSchemaUsesClosedStringArguments(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(decisionSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if closed, ok := schema["additionalProperties"].(bool); !ok || closed {
		t.Fatalf("outer schema must set additionalProperties=false: %s", decisionSchema)
	}
	properties := schema["properties"].(map[string]any)
	calls := properties["calls"].(map[string]any)
	items := calls["items"].(map[string]any)
	if closed, ok := items["additionalProperties"].(bool); !ok || closed {
		t.Fatalf("call schema must set additionalProperties=false: %s", decisionSchema)
	}
	callProperties := items["properties"].(map[string]any)
	if callProperties["capability_id"].(map[string]any)["type"] != "string" || callProperties["arguments_json"].(map[string]any)["type"] != "string" {
		t.Fatalf("call schema must use string capability_id and arguments_json: %s", decisionSchema)
	}
	if _, exists := callProperties["arguments"]; exists {
		t.Fatalf("strict schema must not expose an open arguments object: %s", decisionSchema)
	}
}

func TestDecodeDecisionParsesArgumentsJSON(t *testing.T) {
	decision, err := decodeDecision(json.RawMessage(`{"version":1,"decision":"CALL","calls":[{"capability_id":"entity.search","arguments_json":"{\"query\":\"Phoenix\",\"limit\":10}"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.Calls) != 1 || decision.Calls[0].ID != "entity.search" || string(decision.Calls[0].Arguments) != `{"query":"Phoenix","limit":10}` {
		t.Fatalf("decision=%+v", decision)
	}
}

func TestDecodeDecisionRejectsInvalidArgumentsJSON(t *testing.T) {
	tests := map[string]string{
		"malformed":   `{bad`,
		"array":       `[]`,
		"null":        `null`,
		"json string": `"text"`,
	}
	for name, arguments := range tests {
		t.Run(name, func(t *testing.T) {
			encoded, err := json.Marshal(decisionWireEnvelope{Version: 1, Decision: "CALL", Calls: []decisionWireCall{{CapabilityID: "entity.search", ArgumentsJSON: arguments}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeDecision(encoded); err == nil || !errors.Is(err, ErrProtocol) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestDecodeDecisionFinalAndMultipleCalls(t *testing.T) {
	final, err := decodeDecision(json.RawMessage(`{"version":1,"decision":"FINAL","calls":[]}`))
	if err != nil || len(final.Calls) != 0 {
		t.Fatalf("final=%+v err=%v", final, err)
	}
	if _, err := decodeDecision(json.RawMessage(`{"version":1,"decision":"FINAL","calls":[{"capability_id":"a","arguments_json":"{}"}]}`)); err == nil || !errors.Is(err, ErrProtocol) {
		t.Fatalf("FINAL with calls must fail closed: %v", err)
	}
	multiple, err := decodeDecision(json.RawMessage(`{"version":1,"decision":"CALL","calls":[{"capability_id":"a","arguments_json":"{}"},{"capability_id":"b","arguments_json":"{\"id\":\"1\"}"}]}`))
	if err != nil || len(multiple.Calls) != 2 || multiple.Calls[1].ID != "b" || string(multiple.Calls[1].Arguments) != `{"id":"1"}` {
		t.Fatalf("multiple=%+v err=%v", multiple, err)
	}
}

func TestNativeRuntimeInvalidArgumentsStillFailThroughCapabilityHost(t *testing.T) {
	var executed json.RawMessage
	reads := &testReads{descriptors: []readcap.Descriptor{{ID: "state.read", Description: "state", InputSemantics: "{}", ResultSemantics: "state", Origin: readcap.OriginCore}}, execute: func(_ context.Context, req readcap.Request) (readcap.Result, error) {
		executed = append(json.RawMessage(nil), req.Input...)
		return readcap.Result{}, readcap.ErrInvalidInput
	}}
	host, _ := NewCapabilityHost(reads, testProposes{})
	model := modelFunc(func(_ context.Context, req ai.Request) (ai.Response, error) {
		return ai.Response{RequestID: req.RequestID, Action: req.Action, Output: json.RawMessage(`{"version":1,"decision":"CALL","calls":[{"capability_id":"state.read","arguments_json":"{\"unexpected\":true}"}]}`)}, nil
	})
	runtime, _ := NewNative(model, host)
	_, err := runtime.Execute(context.Background(), Request{RequestID: "r", Purpose: ai.ActionAssistantAsk, Instructions: "answer", Input: "x", OutputSchema: json.RawMessage(`{"type":"object"}`)})
	if err == nil || !contains(err.Error(), ErrorCapabilityInput) || string(executed) != `{"unexpected":true}` {
		t.Fatalf("err=%v executed=%s", err, executed)
	}
}

func TestNativeRuntimeOwnsCapabilityLoop(t *testing.T) {
	reads := &testReads{descriptors: []readcap.Descriptor{{ID: "state.read", Description: "state", InputSemantics: "{}", ResultSemantics: "state", Origin: readcap.OriginCore}}, execute: func(_ context.Context, req readcap.Request) (readcap.Result, error) {
		return readcap.Result{CapabilityID: req.CapabilityID, Output: map[string]bool{"current": true}}, nil
	}}
	host, _ := NewCapabilityHost(reads, testProposes{{ID: "item.propose", Summary: "propose"}})
	var mu sync.Mutex
	var requests []ai.Request
	model := modelFunc(func(_ context.Context, req ai.Request) (ai.Response, error) {
		mu.Lock()
		requests = append(requests, req)
		index := len(requests)
		mu.Unlock()
		output := `{"version":1,"decision":"CALL","calls":[{"capability_id":"state.read","arguments_json":"{}"}]}`
		if index == 2 {
			output = `{"version":1,"decision":"FINAL","calls":[]}`
		}
		if req.Action == ai.ActionAssistantAsk {
			output = `{"answer":"current"}`
		}
		return ai.Response{RequestID: req.RequestID, Action: req.Action, Output: json.RawMessage(output)}, nil
	})
	runtime, _ := NewNative(model, host)
	result, err := runtime.Execute(context.Background(), Request{RequestID: "r", Purpose: ai.ActionAssistantAsk, Instructions: "answer", Input: "current?", OutputSchema: json.RawMessage(`{"type":"object"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Evidence) != 1 || string(result.Output) != `{"answer":"current"}` {
		t.Fatalf("result=%+v", result)
	}
	if len(requests) != 3 || requests[0].Action != ai.ActionRuntimeCapabilityDecision || requests[2].Action != ai.ActionAssistantAsk {
		t.Fatalf("requests=%+v", requests)
	}
	if !contains(requests[0].Messages[0].Content, "state.read") || !contains(requests[2].Messages[0].Content, "item.propose") {
		t.Fatal("runtime did not receive unified catalog")
	}
}

func TestNativeRuntimeCancellationDuringRead(t *testing.T) {
	reads := &testReads{descriptors: []readcap.Descriptor{{ID: "slow", Description: "slow", InputSemantics: "{}", ResultSemantics: "x", Origin: readcap.OriginCore}}, execute: func(ctx context.Context, _ readcap.Request) (readcap.Result, error) {
		<-ctx.Done()
		return readcap.Result{}, ctx.Err()
	}}
	host, _ := NewCapabilityHost(reads, testProposes{})
	model := modelFunc(func(_ context.Context, req ai.Request) (ai.Response, error) {
		return ai.Response{RequestID: req.RequestID, Action: req.Action, Output: json.RawMessage(`{"version":1,"decision":"CALL","calls":[{"capability_id":"slow","arguments_json":"{}"}]}`)}, nil
	})
	runtime, _ := NewNative(model, host)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := runtime.Execute(ctx, Request{RequestID: "r", Purpose: ai.ActionAssistantAsk, Instructions: "answer", Input: "x", OutputSchema: json.RawMessage(`{"type":"object"}`)})
	if err == nil || (!errors.Is(err, context.DeadlineExceeded) && !contains(err.Error(), ErrorTimeout)) {
		t.Fatalf("err=%v", err)
	}
}

func contains(value, fragment string) bool { return strings.Contains(value, fragment) }
