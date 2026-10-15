package aiadapter

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/airuntime"
	"lum-harbor-notifier/internal/plugin"
)

type runtimeInvokerFunc func(context.Context, plugin.Invocation) (json.RawMessage, error)

func (f runtimeInvokerFunc) Invoke(ctx context.Context, invocation plugin.Invocation) (json.RawMessage, error) {
	return f(ctx, invocation)
}

type runtimeHost struct {
	catalog []airuntime.Capability
	calls   []airuntime.CapabilityCall
}

func (h *runtimeHost) ListCapabilities() []airuntime.Capability {
	return append([]airuntime.Capability(nil), h.catalog...)
}

func (h *runtimeHost) ExecuteReads(_ context.Context, calls []airuntime.CapabilityCall) ([]airuntime.CapabilityResult, error) {
	h.calls = append(h.calls, calls...)
	results := make([]airuntime.CapabilityResult, len(calls))
	for index, call := range calls {
		results[index] = airuntime.CapabilityResult{Index: index, Call: call}
		if call.ID == "core.read" {
			results[index].Output = json.RawMessage(`{"value":"authoritative"}`)
		} else {
			results[index].Error = &airuntime.RuntimeError{Code: airuntime.ErrorCapabilityNotFound, Message: "unknown"}
		}
	}
	return results, nil
}

func runtimeRequest() airuntime.Request {
	return airuntime.Request{RequestID: "request-1", Purpose: ai.ActionAssistantAsk, Instructions: "answer", Input: "question", OutputSchema: json.RawMessage(`{"type":"object"}`), Limits: airuntime.DefaultLimits()}
}

func TestRuntimeResumesWithCoreResultsAndReturnsFinal(t *testing.T) {
	host := &runtimeHost{catalog: []airuntime.Capability{{ID: "core.read", Kind: airuntime.CapabilityRead, Origin: "CORE"}, {ID: "item.propose", Kind: airuntime.CapabilityPropose, Origin: "CORE"}}}
	var phases []string
	invoker := runtimeInvokerFunc(func(_ context.Context, invocation plugin.Invocation) (json.RawMessage, error) {
		if invocation.CapabilityID != plugin.CapabilityAIRuntime {
			t.Fatalf("capability=%s", invocation.CapabilityID)
		}
		var wire runtimeWireRequest
		if err := json.Unmarshal(invocation.Input, &wire); err != nil {
			t.Fatal(err)
		}
		phases = append(phases, wire.Phase)
		if wire.Phase == runtimePhaseStart {
			if len(wire.Capabilities) != 2 || wire.Capabilities[1].Kind != airuntime.CapabilityPropose {
				t.Fatalf("catalog=%+v", wire.Capabilities)
			}
			return json.Marshal(runtimeWireResponse{Status: runtimeNeedCalls, State: json.RawMessage(`{"turn":1}`), Calls: []airuntime.CapabilityCall{{ID: "core.read", Arguments: json.RawMessage(`{}`)}}})
		}
		if len(wire.Results) != 1 || !strings.Contains(string(wire.Results[0].Output), "authoritative") {
			t.Fatalf("resume results=%+v", wire.Results)
		}
		return json.Marshal(runtimeWireResponse{Status: runtimeFinal, Output: json.RawMessage(`{"answer":"ok"}`)})
	})
	runtime, err := NewRuntime(invoker, "runtime-plugin", &plugin.Instance{ID: "instance", ExtensionID: "runtime-plugin", CapabilityID: plugin.CapabilityAIRuntime}, host)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Execute(context.Background(), runtimeRequest())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(phases, ",") != "START,RESUME" || len(host.calls) != 1 || len(result.Evidence) != 1 || string(result.Output) != `{"answer":"ok"}` {
		t.Fatalf("phases=%v calls=%+v result=%+v", phases, host.calls, result)
	}
}

func TestRuntimeFailsClosedOnInvalidCapabilityID(t *testing.T) {
	host := &runtimeHost{}
	invoker := runtimeInvokerFunc(func(_ context.Context, _ plugin.Invocation) (json.RawMessage, error) {
		return json.Marshal(runtimeWireResponse{Status: runtimeNeedCalls, State: json.RawMessage(`{"turn":1}`), Calls: []airuntime.CapabilityCall{{ID: "unknown", Arguments: json.RawMessage(`{}`)}}})
	})
	runtime, _ := NewRuntime(invoker, "runtime-plugin", &plugin.Instance{ID: "instance", ExtensionID: "runtime-plugin", CapabilityID: plugin.CapabilityAIRuntime}, host)
	_, err := runtime.Execute(context.Background(), runtimeRequest())
	if err == nil || !strings.Contains(err.Error(), airuntime.ErrorCapabilityNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestRuntimePropagatesCancellationToInvocation(t *testing.T) {
	invoker := runtimeInvokerFunc(func(ctx context.Context, _ plugin.Invocation) (json.RawMessage, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	runtime, _ := NewRuntime(invoker, "runtime-plugin", &plugin.Instance{ID: "instance", ExtensionID: "runtime-plugin", CapabilityID: plugin.CapabilityAIRuntime}, &runtimeHost{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := runtime.Execute(ctx, runtimeRequest())
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}
