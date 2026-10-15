package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type providerFunc func(context.Context, Request) (Response, error)

func (f providerFunc) Execute(ctx context.Context, request Request) (Response, error) {
	return f(ctx, request)
}

type preMiddlewareFunc func(context.Context, Request) (PreRequestResult, error)

func (f preMiddlewareFunc) Process(ctx context.Context, request Request) (PreRequestResult, error) {
	return f(ctx, request)
}

type postMiddlewareFunc func(context.Context, Response) (PostResponseResult, error)

func (f postMiddlewareFunc) Process(ctx context.Context, response Response) (PostResponseResult, error) {
	return f(ctx, response)
}

type validatorFunc func(context.Context, Request, Response) error

func (f validatorFunc) Validate(ctx context.Context, request Request, response Response) error {
	return f(ctx, request, response)
}

func testRequest(id string, action Action) Request {
	return Request{
		RequestID:    id,
		Action:       action,
		Messages:     []Message{{Role: RoleUser, Content: "original"}},
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		Metadata:     map[string]string{"source": "test"},
	}
}

func responseFor(request Request, output string) Response {
	return Response{RequestID: request.RequestID, Action: request.Action, Output: json.RawMessage(output)}
}

func newTestService(t *testing.T, provider Provider, configure func(*Config)) *Service {
	t.Helper()
	config := Config{
		BuiltInProvider: ProviderRegistration{ID: "builtin-test", Name: "Built-in Test", Provider: provider},
	}
	if configure != nil {
		configure(&config)
	}
	service, err := NewService(config)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

func TestServiceBasicProviderExecutionAndIdentityStamp(t *testing.T) {
	var providerCalls atomic.Int32
	provider := providerFunc(func(_ context.Context, request Request) (Response, error) {
		providerCalls.Add(1)
		response := responseFor(request, `{"ok":true}`)
		fakeProvider := ProviderInfo{ID: "forged", Name: "Forged", Source: ProviderSourcePlugin}
		response.Provenance = ResponseProvenance{
			ProviderInvoked: true,
			ActualProvider:  &fakeProvider,
			Origin:          ResponseOrigin{Kind: ResponseOriginProvider, ID: "forged", Name: "Forged"},
		}
		return response, nil
	})
	service := newTestService(t, provider, nil)

	response, err := service.Execute(context.Background(), testRequest("request-1", ActionPlannerProposal))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if string(response.Output) != `{"ok":true}` {
		t.Fatalf("unexpected output: %s", response.Output)
	}
	expected := ProviderInfo{ID: "builtin-test", Name: "Built-in Test", Source: ProviderSourceBuiltIn}
	if providerCalls.Load() != 1 {
		t.Fatalf("expected provider called once, got %d", providerCalls.Load())
	}
	if !response.Provenance.ProviderInvoked || response.Provenance.ActualProvider == nil || *response.Provenance.ActualProvider != expected {
		t.Fatalf("expected actual provider %+v, got %+v", expected, response.Provenance)
	}
	expectedOrigin := ResponseOrigin{Kind: ResponseOriginProvider, ID: "builtin-test", Name: "Built-in Test"}
	if response.Provenance.Origin != expectedOrigin {
		t.Fatalf("expected provider origin %+v, got %+v", expectedOrigin, response.Provenance.Origin)
	}

	status := service.RuntimeStatus()
	if status.Mode != RuntimeModeBuiltIn || status.ProviderSource != ProviderSourceBuiltIn || !status.BuiltInConfigEditable {
		t.Fatalf("unexpected built-in status: %+v", status)
	}
	if status.EffectiveProviderID != "builtin-test" || status.EffectiveProviderName != "Built-in Test" {
		t.Fatalf("unexpected effective provider: %+v", status)
	}
}

func TestActionAwareAndAllActionsMiddleware(t *testing.T) {
	var selectedCalls atomic.Int32
	var allCalls atomic.Int32
	selected := preMiddlewareFunc(func(_ context.Context, _ Request) (PreRequestResult, error) {
		selectedCalls.Add(1)
		return PreRequestResult{Decision: DecisionContinue}, nil
	})
	all := preMiddlewareFunc(func(_ context.Context, _ Request) (PreRequestResult, error) {
		allCalls.Add(1)
		return PreRequestResult{Decision: DecisionContinue}, nil
	})
	provider := providerFunc(func(_ context.Context, request Request) (Response, error) {
		return responseFor(request, `{}`), nil
	})
	service := newTestService(t, provider, func(config *Config) {
		config.PreMiddleware = []PreMiddlewareBinding{
			{ID: "selected", Actions: []Action{ActionNotificationEvaluate}, Middleware: selected},
			{ID: "all", AllActions: true, Middleware: all},
		}
	})

	if _, err := service.Execute(context.Background(), testRequest("one", ActionPlannerProposal)); err != nil {
		t.Fatalf("planner Execute: %v", err)
	}
	if _, err := service.Execute(context.Background(), testRequest("two", ActionNotificationEvaluate)); err != nil {
		t.Fatalf("notification Execute: %v", err)
	}
	if selectedCalls.Load() != 1 || allCalls.Load() != 2 {
		t.Fatalf("unexpected middleware calls: selected=%d all=%d", selectedCalls.Load(), allCalls.Load())
	}
}

func TestPreMiddlewareOrderReplaceAndContinue(t *testing.T) {
	var mu sync.Mutex
	var order []string
	record := func(value string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, value)
	}
	preA := preMiddlewareFunc(func(_ context.Context, request Request) (PreRequestResult, error) {
		record("A")
		request.Messages[0].Content = "replaced"
		request.Metadata["pre"] = "A"
		return PreRequestResult{Decision: DecisionReplace, Request: request}, nil
	})
	preB := preMiddlewareFunc(func(_ context.Context, request Request) (PreRequestResult, error) {
		record("B")
		if request.Messages[0].Content != "replaced" || request.Metadata["pre"] != "A" {
			return PreRequestResult{}, fmt.Errorf("replacement was not propagated")
		}
		// Mutation accompanying CONTINUE must not affect the provider payload.
		request.Messages[0].Content = "must-be-ignored"
		return PreRequestResult{Decision: DecisionContinue, Request: request}, nil
	})
	provider := providerFunc(func(_ context.Context, request Request) (Response, error) {
		record("provider")
		if request.Messages[0].Content != "replaced" {
			return Response{}, fmt.Errorf("provider saw %q", request.Messages[0].Content)
		}
		return responseFor(request, `{}`), nil
	})
	service := newTestService(t, provider, func(config *Config) {
		config.PreMiddleware = []PreMiddlewareBinding{
			{ID: "A", AllActions: true, Middleware: preA},
			{ID: "B", AllActions: true, Middleware: preB},
		}
	})

	if _, err := service.Execute(context.Background(), testRequest("order", ActionPlannerProposal)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Join(order, ",") != "A,B,provider" {
		t.Fatalf("unexpected order: %v", order)
	}
}

func TestPreStopSkipsProviderButRunsPostMiddleware(t *testing.T) {
	var providerCalls atomic.Int32
	var postCalls atomic.Int32
	provider := providerFunc(func(_ context.Context, request Request) (Response, error) {
		providerCalls.Add(1)
		return responseFor(request, `{}`), nil
	})
	pre := preMiddlewareFunc(func(_ context.Context, request Request) (PreRequestResult, error) {
		response := responseFor(request, `{"short":true}`)
		fakeProvider := ProviderInfo{ID: "fake-provider", Name: "Fake", Source: ProviderSourcePlugin}
		response.Provenance = ResponseProvenance{
			ProviderInvoked: true,
			ActualProvider:  &fakeProvider,
			Origin:          ResponseOrigin{Kind: ResponseOriginProvider, ID: "fake-provider"},
		}
		return PreRequestResult{Decision: DecisionStop, Response: response}, nil
	})
	post := postMiddlewareFunc(func(_ context.Context, response Response) (PostResponseResult, error) {
		postCalls.Add(1)
		response.Output = json.RawMessage(`{"post":true}`)
		return PostResponseResult{Decision: DecisionReplace, Response: response}, nil
	})
	service := newTestService(t, provider, func(config *Config) {
		config.PreMiddleware = []PreMiddlewareBinding{{ID: "local-cache", Name: "Local Cache", AllActions: true, Middleware: pre}}
		config.PostMiddleware = []PostMiddlewareBinding{{ID: "post", AllActions: true, Middleware: post}}
	})

	response, err := service.Execute(context.Background(), testRequest("stop", ActionSourceInterpret))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if providerCalls.Load() != 0 || postCalls.Load() != 1 || string(response.Output) != `{"post":true}` {
		t.Fatalf("unexpected stop behavior: provider=%d post=%d output=%s", providerCalls.Load(), postCalls.Load(), response.Output)
	}
	if response.Provenance.ProviderInvoked || response.Provenance.ActualProvider != nil {
		t.Fatalf("pre STOP must not claim provider invocation: %+v", response.Provenance)
	}
	expectedOrigin := ResponseOrigin{Kind: ResponseOriginPreMiddleware, ID: "local-cache", Name: "Local Cache"}
	if response.Provenance.Origin != expectedOrigin {
		t.Fatalf("expected pre middleware origin %+v, got %+v", expectedOrigin, response.Provenance.Origin)
	}
}

func TestPreStopPostContinuePreservesOrigin(t *testing.T) {
	provider := providerFunc(func(_ context.Context, request Request) (Response, error) {
		return responseFor(request, `{}`), nil
	})
	pre := preMiddlewareFunc(func(_ context.Context, request Request) (PreRequestResult, error) {
		return PreRequestResult{Decision: DecisionStop, Response: responseFor(request, `{"cached":true}`)}, nil
	})
	post := postMiddlewareFunc(func(_ context.Context, response Response) (PostResponseResult, error) {
		response.Provenance.Origin.ID = "forged"
		return PostResponseResult{Decision: DecisionContinue, Response: response}, nil
	})
	service := newTestService(t, provider, func(config *Config) {
		config.PreMiddleware = []PreMiddlewareBinding{{ID: "cache", AllActions: true, Middleware: pre}}
		config.PostMiddleware = []PostMiddlewareBinding{{ID: "observer", AllActions: true, Middleware: post}}
	})

	response, err := service.Execute(context.Background(), testRequest("pre-continue", ActionSourceInterpret))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if response.Provenance.ProviderInvoked || response.Provenance.ActualProvider != nil || response.Provenance.Origin.ID != "cache" {
		t.Fatalf("CONTINUE changed pre STOP provenance: %+v", response.Provenance)
	}
}

func TestPostMiddlewareOrderReplaceContinueAndStop(t *testing.T) {
	var order []string
	provider := providerFunc(func(_ context.Context, request Request) (Response, error) {
		return responseFor(request, `{"step":0}`), nil
	})
	postA := postMiddlewareFunc(func(_ context.Context, response Response) (PostResponseResult, error) {
		order = append(order, "A")
		response.Output = json.RawMessage(`{"step":1}`)
		return PostResponseResult{Decision: DecisionReplace, Response: response}, nil
	})
	postB := postMiddlewareFunc(func(_ context.Context, response Response) (PostResponseResult, error) {
		order = append(order, "B")
		if string(response.Output) != `{"step":1}` {
			return PostResponseResult{}, fmt.Errorf("B saw %s", response.Output)
		}
		response.Output = json.RawMessage(`{"ignored":true}`)
		return PostResponseResult{Decision: DecisionContinue, Response: response}, nil
	})
	postC := postMiddlewareFunc(func(_ context.Context, response Response) (PostResponseResult, error) {
		order = append(order, "C")
		if string(response.Output) != `{"step":1}` {
			return PostResponseResult{}, fmt.Errorf("C saw %s", response.Output)
		}
		response.Output = json.RawMessage(`{"final":true}`)
		return PostResponseResult{Decision: DecisionStop, Response: response}, nil
	})
	postD := postMiddlewareFunc(func(_ context.Context, response Response) (PostResponseResult, error) {
		order = append(order, "D")
		return PostResponseResult{Decision: DecisionContinue}, nil
	})
	service := newTestService(t, provider, func(config *Config) {
		config.PostMiddleware = []PostMiddlewareBinding{
			{ID: "A", AllActions: true, Middleware: postA},
			{ID: "B", AllActions: true, Middleware: postB},
			{ID: "C", AllActions: true, Middleware: postC},
			{ID: "D", AllActions: true, Middleware: postD},
		}
	})

	response, err := service.Execute(context.Background(), testRequest("post", ActionPlannerProposal))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Join(order, ",") != "A,B,C" || string(response.Output) != `{"final":true}` {
		t.Fatalf("unexpected post pipeline: order=%v output=%s", order, response.Output)
	}
	if !response.Provenance.ProviderInvoked || response.Provenance.ActualProvider == nil || response.Provenance.ActualProvider.ID != "builtin-test" || response.Provenance.Origin.ID != "builtin-test" {
		t.Fatalf("post transformations changed provider provenance: %+v", response.Provenance)
	}
}

func TestPreReplacementCannotChangeRequestInvariants(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Request)
		want   string
	}{
		{name: "request ID", mutate: func(request *Request) { request.RequestID = "other" }, want: "request ID changed"},
		{name: "action", mutate: func(request *Request) { request.Action = ActionNotificationEvaluate }, want: "action changed"},
		{name: "output schema", mutate: func(request *Request) { request.OutputSchema = json.RawMessage(`{"type":"array"}`) }, want: "output schema changed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pre := preMiddlewareFunc(func(_ context.Context, request Request) (PreRequestResult, error) {
				test.mutate(&request)
				return PreRequestResult{Decision: DecisionReplace, Request: request}, nil
			})
			provider := providerFunc(func(_ context.Context, request Request) (Response, error) {
				return responseFor(request, `{}`), nil
			})
			service := newTestService(t, provider, func(config *Config) {
				config.PreMiddleware = []PreMiddlewareBinding{{ID: "breaker", AllActions: true, Middleware: pre}}
			})
			_, err := service.Execute(context.Background(), testRequest("invariant", ActionPlannerProposal))
			if !errors.Is(err, ErrMiddleware) || !strings.Contains(err.Error(), "pre middleware \"breaker\"") || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected contextual invariant error containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestPostReplacementCannotChangeResponseInvariants(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Response)
		want   string
	}{
		{name: "request ID", mutate: func(response *Response) { response.RequestID = "other" }, want: "request ID"},
		{name: "action", mutate: func(response *Response) { response.Action = ActionNotificationEvaluate }, want: "action"},
		{name: "origin", mutate: func(response *Response) { response.Provenance.Origin.ID = "imposter" }, want: "response provenance changed"},
		{name: "provider invocation", mutate: func(response *Response) { response.Provenance.ProviderInvoked = false }, want: "response provenance changed"},
		{name: "actual provider", mutate: func(response *Response) { response.Provenance.ActualProvider.ID = "imposter" }, want: "response provenance changed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := providerFunc(func(_ context.Context, request Request) (Response, error) {
				return responseFor(request, `{}`), nil
			})
			post := postMiddlewareFunc(func(_ context.Context, response Response) (PostResponseResult, error) {
				test.mutate(&response)
				return PostResponseResult{Decision: DecisionReplace, Response: response}, nil
			})
			service := newTestService(t, provider, func(config *Config) {
				config.PostMiddleware = []PostMiddlewareBinding{{ID: "breaker", AllActions: true, Middleware: post}}
			})
			_, err := service.Execute(context.Background(), testRequest("invariant", ActionPlannerProposal))
			if !errors.Is(err, ErrMiddleware) || !strings.Contains(err.Error(), "post middleware \"breaker\"") || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected contextual invariant error containing %q, got %v", test.want, err)
			}
		})
	}
}

func TestGlobalReplacementProviderAppliesToAllActions(t *testing.T) {
	var builtInCalls atomic.Int32
	var replacementCalls atomic.Int32
	builtIn := providerFunc(func(_ context.Context, request Request) (Response, error) {
		builtInCalls.Add(1)
		return responseFor(request, `{"provider":"builtin"}`), nil
	})
	replacement := providerFunc(func(_ context.Context, request Request) (Response, error) {
		replacementCalls.Add(1)
		return responseFor(request, `{"provider":"plugin"}`), nil
	})
	service := newTestService(t, builtIn, func(config *Config) {
		config.ReplacementProvider = &ProviderRegistration{ID: "company-ai", Name: "Company AI", Provider: replacement}
	})

	for index, action := range []Action{ActionNotificationEvaluate, ActionSourceInterpret, ActionPlannerProposal} {
		response, err := service.Execute(context.Background(), testRequest(fmt.Sprintf("replace-%d", index), action))
		if err != nil {
			t.Fatalf("Execute %q: %v", action, err)
		}
		if !response.Provenance.ProviderInvoked || response.Provenance.ActualProvider == nil || response.Provenance.ActualProvider.ID != "company-ai" || response.Provenance.ActualProvider.Source != ProviderSourcePlugin {
			t.Fatalf("unexpected actual provider for %q: %+v", action, response.Provenance)
		}
		if response.Provenance.Origin.Kind != ResponseOriginProvider || response.Provenance.Origin.ID != "company-ai" {
			t.Fatalf("unexpected replacement origin for %q: %+v", action, response.Provenance.Origin)
		}
	}
	if builtInCalls.Load() != 0 || replacementCalls.Load() != 3 {
		t.Fatalf("unexpected calls: built-in=%d replacement=%d", builtInCalls.Load(), replacementCalls.Load())
	}
	status := service.RuntimeStatus()
	if status.Mode != RuntimeModePluginProvider || status.BuiltInConfigEditable || status.EffectiveProviderID != "company-ai" {
		t.Fatalf("unexpected replacement status: %+v", status)
	}
}

func TestReplacementProviderFailureDoesNotFallback(t *testing.T) {
	var builtInCalls atomic.Int32
	var replacementCalls atomic.Int32
	replacementErr := errors.New("replacement unavailable")
	builtIn := providerFunc(func(_ context.Context, request Request) (Response, error) {
		builtInCalls.Add(1)
		return responseFor(request, `{}`), nil
	})
	replacement := providerFunc(func(_ context.Context, _ Request) (Response, error) {
		replacementCalls.Add(1)
		return Response{}, replacementErr
	})
	service := newTestService(t, builtIn, func(config *Config) {
		config.ReplacementProvider = &ProviderRegistration{ID: "private-ai", Name: "Private AI", Provider: replacement}
	})

	_, err := service.Execute(context.Background(), testRequest("no-fallback", ActionPlannerProposal))
	if !errors.Is(err, ErrProvider) || !errors.Is(err, replacementErr) || !strings.Contains(err.Error(), "private-ai") {
		t.Fatalf("unexpected replacement error: %v", err)
	}
	if builtInCalls.Load() != 0 || replacementCalls.Load() != 1 {
		t.Fatalf("silent fallback detected: built-in=%d replacement=%d", builtInCalls.Load(), replacementCalls.Load())
	}
}

func TestFinalStructuredOutputValidation(t *testing.T) {
	t.Run("provider invalid JSON", func(t *testing.T) {
		provider := providerFunc(func(_ context.Context, request Request) (Response, error) {
			return responseFor(request, `{invalid`), nil
		})
		service := newTestService(t, provider, nil)
		_, err := service.Execute(context.Background(), testRequest("invalid-provider", ActionPlannerProposal))
		if !errors.Is(err, ErrFinalValidation) {
			t.Fatalf("expected final validation error, got %v", err)
		}
	})

	t.Run("post invalid JSON", func(t *testing.T) {
		provider := providerFunc(func(_ context.Context, request Request) (Response, error) {
			return responseFor(request, `{}`), nil
		})
		post := postMiddlewareFunc(func(_ context.Context, response Response) (PostResponseResult, error) {
			response.Output = json.RawMessage(`not-json`)
			return PostResponseResult{Decision: DecisionReplace, Response: response}, nil
		})
		service := newTestService(t, provider, func(config *Config) {
			config.PostMiddleware = []PostMiddlewareBinding{{ID: "invalidator", AllActions: true, Middleware: post}}
		})
		_, err := service.Execute(context.Background(), testRequest("invalid-post", ActionPlannerProposal))
		if !errors.Is(err, ErrFinalValidation) {
			t.Fatalf("expected final validation error, got %v", err)
		}
	})

	t.Run("validator sees final post output", func(t *testing.T) {
		provider := providerFunc(func(_ context.Context, request Request) (Response, error) {
			return responseFor(request, `{"value":"raw"}`), nil
		})
		filter := postMiddlewareFunc(func(_ context.Context, response Response) (PostResponseResult, error) {
			response.Output = json.RawMessage(`{"value":"filtered"}`)
			return PostResponseResult{Decision: DecisionReplace, Response: response}, nil
		})
		validator := validatorFunc(func(_ context.Context, request Request, response Response) error {
			if request.Action != ActionPlannerProposal || string(response.Output) != `{"value":"filtered"}` {
				return fmt.Errorf("validator did not receive final output")
			}
			return nil
		})
		service := newTestService(t, provider, func(config *Config) {
			config.PostMiddleware = []PostMiddlewareBinding{{ID: "filter", AllActions: true, Middleware: filter}}
			config.Validator = validator
		})
		response, err := service.Execute(context.Background(), testRequest("valid-post", ActionPlannerProposal))
		if err != nil || string(response.Output) != `{"value":"filtered"}` {
			t.Fatalf("expected valid filtered response, response=%s err=%v", response.Output, err)
		}
	})
}

func TestProviderAndMiddlewareErrorsRetainContext(t *testing.T) {
	t.Run("provider", func(t *testing.T) {
		cause := errors.New("network down")
		provider := providerFunc(func(context.Context, Request) (Response, error) { return Response{}, cause })
		service := newTestService(t, provider, nil)
		_, err := service.Execute(context.Background(), testRequest("provider-error", ActionSourceInterpret))
		if !errors.Is(err, ErrProvider) || !errors.Is(err, cause) || !strings.Contains(err.Error(), string(ActionSourceInterpret)) || !strings.Contains(err.Error(), "builtin-test") {
			t.Fatalf("provider error lost context: %v", err)
		}
	})

	t.Run("middleware", func(t *testing.T) {
		cause := errors.New("policy failed")
		provider := providerFunc(func(_ context.Context, request Request) (Response, error) { return responseFor(request, `{}`), nil })
		pre := preMiddlewareFunc(func(context.Context, Request) (PreRequestResult, error) { return PreRequestResult{}, cause })
		service := newTestService(t, provider, func(config *Config) {
			config.PreMiddleware = []PreMiddlewareBinding{{ID: "policy", AllActions: true, Middleware: pre}}
		})
		_, err := service.Execute(context.Background(), testRequest("middleware-error", ActionNotificationEvaluate))
		if !errors.Is(err, ErrMiddleware) || !errors.Is(err, cause) || !strings.Contains(err.Error(), "policy") || !strings.Contains(err.Error(), "pre middleware") {
			t.Fatalf("middleware error lost context: %v", err)
		}
	})
}

func TestInvalidProviderResponseCorrelation(t *testing.T) {
	provider := providerFunc(func(_ context.Context, request Request) (Response, error) {
		response := responseFor(request, `{}`)
		response.RequestID = "wrong"
		return response, nil
	})
	service := newTestService(t, provider, nil)
	_, err := service.Execute(context.Background(), testRequest("expected", ActionPlannerProposal))
	if !errors.Is(err, ErrInvalidProviderResponse) || !strings.Contains(err.Error(), "builtin-test") {
		t.Fatalf("expected contextual invalid provider response, got %v", err)
	}
}

func TestContextCancellationPropagatesToProvider(t *testing.T) {
	started := make(chan struct{})
	provider := providerFunc(func(ctx context.Context, _ Request) (Response, error) {
		close(started)
		<-ctx.Done()
		return Response{}, ctx.Err()
	})
	service := newTestService(t, provider, nil)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := service.Execute(ctx, testRequest("cancelled", ActionPlannerProposal))
		result <- err
	}()
	<-started
	cancel()
	err := <-result
	if !errors.Is(err, context.Canceled) || !errors.Is(err, ErrProvider) {
		t.Fatalf("expected provider cancellation, got %v", err)
	}
}

func TestServiceCopiesConfiguration(t *testing.T) {
	var middlewareCalls atomic.Int32
	middleware := preMiddlewareFunc(func(_ context.Context, _ Request) (PreRequestResult, error) {
		middlewareCalls.Add(1)
		return PreRequestResult{Decision: DecisionContinue}, nil
	})
	actions := []Action{ActionPlannerProposal}
	bindings := []PreMiddlewareBinding{{ID: "scoped", Actions: actions, Middleware: middleware}}
	provider := providerFunc(func(_ context.Context, request Request) (Response, error) { return responseFor(request, `{}`), nil })
	service := newTestService(t, provider, func(config *Config) { config.PreMiddleware = bindings })

	actions[0] = ActionNotificationEvaluate
	bindings[0].Actions[0] = ActionNotificationEvaluate
	if _, err := service.Execute(context.Background(), testRequest("copied", ActionPlannerProposal)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if middlewareCalls.Load() != 1 {
		t.Fatalf("configuration mutation changed service plan")
	}
}

func TestConcurrentExecuteKeepsRequestsIsolated(t *testing.T) {
	const executions = 50
	provider := providerFunc(func(_ context.Context, request Request) (Response, error) {
		request.Metadata["provider-mutation"] = request.RequestID
		return responseFor(request, fmt.Sprintf(`{"requestID":%q}`, request.RequestID)), nil
	})
	pre := preMiddlewareFunc(func(_ context.Context, request Request) (PreRequestResult, error) {
		request.Messages[0].Content = request.RequestID
		request.Metadata["pre"] = request.RequestID
		return PreRequestResult{Decision: DecisionReplace, Request: request}, nil
	})
	post := postMiddlewareFunc(func(_ context.Context, response Response) (PostResponseResult, error) {
		response.Metadata = map[string]string{"post": response.RequestID}
		return PostResponseResult{Decision: DecisionReplace, Response: response}, nil
	})
	service := newTestService(t, provider, func(config *Config) {
		config.PreMiddleware = []PreMiddlewareBinding{{ID: "pre", AllActions: true, Middleware: pre}}
		config.PostMiddleware = []PostMiddlewareBinding{{ID: "post", AllActions: true, Middleware: post}}
	})

	var wg sync.WaitGroup
	errorsCh := make(chan error, executions)
	for index := 0; index < executions; index++ {
		id := fmt.Sprintf("concurrent-%d", index)
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, err := service.Execute(context.Background(), testRequest(id, ActionPlannerProposal))
			if err != nil {
				errorsCh <- err
				return
			}
			var output struct {
				RequestID string `json:"requestID"`
			}
			if err := json.Unmarshal(response.Output, &output); err != nil {
				errorsCh <- err
				return
			}
			if response.RequestID != id || output.RequestID != id || response.Metadata["post"] != id || !response.Provenance.ProviderInvoked || response.Provenance.ActualProvider == nil || response.Provenance.ActualProvider.ID != "builtin-test" || response.Provenance.Origin.ID != "builtin-test" {
				errorsCh <- fmt.Errorf("request %q received mixed response %+v output=%+v", id, response, output)
			}
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
}
