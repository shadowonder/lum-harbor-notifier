package aiadapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/aiadapter"
	"lum-harbor-notifier/internal/plugin/plugintest"
)

func TestPluginFixtureProcess(t *testing.T) {
	if plugintest.RunIfRequested() {
		return
	}
}

type providerFunc func(context.Context, ai.Request) (ai.Response, error)

func (f providerFunc) Execute(ctx context.Context, request ai.Request) (ai.Response, error) {
	return f(ctx, request)
}

func newMultiCapabilityRuntime(t *testing.T) (*plugin.Host, plugin.Extension) {
	t.Helper()
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "company-assistant", Name: "Company Assistant",
		Capabilities: []plugin.CapabilityID{
			plugin.CapabilityAIPreRequest, plugin.CapabilityAIProvider,
			plugin.CapabilityAIPostResponse, plugin.CapabilitySourceFetch,
		},
		Bindings: []plugin.BindingDeclaration{
			{ID: "company-context", Type: plugin.BindingAIPre, Capability: plugin.CapabilityAIPreRequest, Actions: []string{"source.interpret"}, Order: 10},
			{ID: "company-provider", Type: plugin.BindingAIProvider, Capability: plugin.CapabilityAIProvider, Actions: []string{"*"}, Order: 20},
			{ID: "operation-filter", Type: plugin.BindingAIPost, Capability: plugin.CapabilityAIPostResponse, Actions: []string{"*"}, Order: 30},
			{ID: "source-schedule", Type: plugin.BindingSchedule, Capability: plugin.CapabilitySourceFetch, DefaultIntervalSeconds: 300, Order: 40},
		},
		Schema: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
			{Key: "team", Label: "Team", Type: plugin.ConfigString},
			{Key: "remoteError", Label: "Remote error", Type: plugin.ConfigBoolean},
		}},
	})
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("Discover: err=%v failures=%v", err, failures)
	}
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout: 5 * time.Second, RequestTimeout: time.Second, ShutdownTimeout: 200 * time.Millisecond,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(context.Background(), "company-assistant"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	extension, _ := registry.Find("company-assistant")
	return host, extension
}

func aiRequest(id string) ai.Request {
	return ai.Request{
		RequestID: id, Action: ai.ActionSourceInterpret,
		Messages:     []ai.Message{{Role: ai.RoleUser, Content: "interpret"}},
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	}
}

func TestIndividualAIPluginAdapters(t *testing.T) {
	host, _ := newMultiCapabilityRuntime(t)
	pre := aiadapter.NewPreMiddleware(host, "company-assistant", nil)
	preResult, err := pre.Process(context.Background(), aiRequest("pre"))
	if err != nil || preResult.Decision != ai.DecisionReplace || preResult.Request.Messages[0].Content != "TEST PLUGIN CONTEXT" {
		t.Fatalf("pre adapter: %+v %v", preResult, err)
	}
	provider := aiadapter.NewProvider(host, "company-assistant", nil)
	providerResult, err := provider.Execute(context.Background(), preResult.Request)
	if err != nil || providerResult.RequestID != "pre" || providerResult.Action != ai.ActionSourceInterpret {
		t.Fatalf("provider adapter: %+v %v", providerResult, err)
	}
	provenance := ai.ResponseProvenance{ProviderInvoked: true, Origin: ai.ResponseOrigin{Kind: ai.ResponseOriginProvider, ID: "runtime"}}
	providerResult.Provenance = provenance
	post := aiadapter.NewPostMiddleware(host, "company-assistant", nil)
	postResult, err := post.Process(context.Background(), providerResult)
	if err != nil || postResult.Decision != ai.DecisionReplace || postResult.Response.Provenance != provenance {
		t.Fatalf("post adapter: %+v %v", postResult, err)
	}
	var output map[string]any
	if err := json.Unmarshal(postResult.Response.Output, &output); err != nil || output["debugField"] != nil {
		t.Fatalf("post output: %s err=%v", postResult.Response.Output, err)
	}
}

func TestFullAIServicePluginPipelineAndRuntimeProvenance(t *testing.T) {
	host, extension := newMultiCapabilityRuntime(t)
	var builtInCalls atomic.Int32
	builtIn := providerFunc(func(_ context.Context, request ai.Request) (ai.Response, error) {
		builtInCalls.Add(1)
		return ai.Response{RequestID: request.RequestID, Action: request.Action, Output: json.RawMessage(`{"builtIn":true}`)}, nil
	})
	replacement, err := aiadapter.ProviderRegistration(extension, host, nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := ai.NewService(ai.Config{
		BuiltInProvider:     ai.ProviderRegistration{ID: "builtin", Name: "Built-in", Provider: builtIn},
		ReplacementProvider: &replacement,
		PreMiddleware:       aiadapter.PreBindings(extension, host, nil),
		PostMiddleware:      aiadapter.PostBindings(extension, host, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.Execute(context.Background(), aiRequest("e2e"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var output map[string]any
	if err := json.Unmarshal(response.Output, &output); err != nil {
		t.Fatal(err)
	}
	if output["contextSeen"] != true || output["debugField"] != nil || output["value"] != "plugin" {
		t.Fatalf("unexpected transformed output: %v", output)
	}
	if builtInCalls.Load() != 0 {
		t.Fatalf("built-in provider was called %d times", builtInCalls.Load())
	}
	if !response.Provenance.ProviderInvoked || response.Provenance.ActualProvider == nil || response.Provenance.ActualProvider.ID != "company-assistant" || response.Provenance.ActualProvider.Source != ai.ProviderSourcePlugin {
		t.Fatalf("runtime did not own provider provenance: %+v", response.Provenance)
	}
	if response.Provenance.Origin.Kind != ai.ResponseOriginProvider || response.Provenance.Origin.ID != "company-assistant" {
		t.Fatalf("unexpected origin: %+v", response.Provenance.Origin)
	}
	status := service.RuntimeStatus()
	if status.Mode != ai.RuntimeModePluginProvider || status.BuiltInConfigEditable {
		t.Fatalf("unexpected runtime status: %+v", status)
	}
}

func TestPluginProviderFailureHasNoBuiltInFallback(t *testing.T) {
	host, extension := newMultiCapabilityRuntime(t)
	instance := plugin.Instance{
		ID: "provider", ExtensionID: extension.Manifest.ID,
		CapabilityID: plugin.CapabilityAIProvider, Config: json.RawMessage(`{"remoteError":true}`),
	}
	replacement, err := aiadapter.ProviderRegistration(extension, host, &instance)
	if err != nil {
		t.Fatal(err)
	}
	var builtInCalls atomic.Int32
	service, err := ai.NewService(ai.Config{
		BuiltInProvider: ai.ProviderRegistration{ID: "builtin", Name: "Built-in", Provider: providerFunc(func(_ context.Context, request ai.Request) (ai.Response, error) {
			builtInCalls.Add(1)
			return ai.Response{RequestID: request.RequestID, Action: request.Action, Output: json.RawMessage(`{}`)}, nil
		})},
		ReplacementProvider: &replacement,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Execute(context.Background(), aiRequest("failure"))
	if !errors.Is(err, ai.ErrProvider) || !errors.Is(err, plugin.ErrRemote) {
		t.Fatalf("expected provider/remote error, got %v", err)
	}
	if builtInCalls.Load() != 0 {
		t.Fatalf("built-in fallback called %d times", builtInCalls.Load())
	}
}

func TestConcurrentAIAndSourceRequestsDoNotCrossTalk(t *testing.T) {
	host, extension := newMultiCapabilityRuntime(t)
	replacement, _ := aiadapter.ProviderRegistration(extension, host, nil)
	service, err := ai.NewService(ai.Config{
		BuiltInProvider: ai.ProviderRegistration{ID: "builtin", Name: "Built-in", Provider: providerFunc(func(_ context.Context, request ai.Request) (ai.Response, error) {
			return ai.Response{RequestID: request.RequestID, Action: request.Action, Output: json.RawMessage(`{}`)}, nil
		})},
		ReplacementProvider: &replacement,
		PreMiddleware:       aiadapter.PreBindings(extension, host, nil),
		PostMiddleware:      aiadapter.PostBindings(extension, host, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	fetcher := plugin.NewSourceFetcher(host)
	const count = 20
	var wg sync.WaitGroup
	errorsCh := make(chan error, count*2)
	for index := 0; index < count; index++ {
		index := index
		wg.Add(2)
		go func() {
			defer wg.Done()
			id := fmt.Sprintf("ai-%d", index)
			response, err := service.Execute(context.Background(), aiRequest(id))
			if err != nil || response.RequestID != id {
				errorsCh <- fmt.Errorf("AI %s: response=%s err=%v", id, response.RequestID, err)
			}
		}()
		go func() {
			defer wg.Done()
			team := fmt.Sprintf("source-%d", index)
			instance := plugin.Instance{ID: team, ExtensionID: extension.Manifest.ID, CapabilityID: plugin.CapabilitySourceFetch, Config: json.RawMessage(fmt.Sprintf(`{"team":%q}`, team))}
			result, err := fetcher.Fetch(context.Background(), instance, plugin.SourceFetchRequest{})
			if err != nil || len(result.Records) != 1 || result.Records[0].ExternalID != team+"-item" {
				errorsCh <- fmt.Errorf("source %s: result=%+v err=%v", team, result, err)
			}
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}

	timeoutInstance := plugin.Instance{ID: "timeout", ExtensionID: extension.Manifest.ID, CapabilityID: plugin.CapabilitySourceFetch, Config: json.RawMessage(`{"team":"timeout"}`)}
	timeoutContext, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	timeoutResult := make(chan error, 1)
	go func() {
		_, err := host.Invoke(timeoutContext, plugin.Invocation{
			ExtensionID: extension.Manifest.ID, CapabilityID: plugin.CapabilitySourceFetch, Instance: &timeoutInstance,
			Input: json.RawMessage(`{"delayMillis":75}`),
		})
		timeoutResult <- err
	}()
	unaffected, err := service.Execute(context.Background(), aiRequest("after-timeout"))
	if err != nil || unaffected.RequestID != "after-timeout" {
		t.Fatalf("concurrent AI call was corrupted: response=%+v err=%v", unaffected, err)
	}
	if err := <-timeoutResult; !errors.Is(err, plugin.ErrRPCCancelled) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected isolated timeout, got %v", err)
	}
	time.Sleep(90 * time.Millisecond)
	finalSource := timeoutInstance
	finalSource.ID = "after-timeout-source"
	finalSource.Config = json.RawMessage(`{"team":"after-timeout-source"}`)
	result, err := fetcher.Fetch(context.Background(), finalSource, plugin.SourceFetchRequest{})
	if err != nil || result.Records[0].ExternalID != "after-timeout-source-item" {
		t.Fatalf("late response corrupted later source call: result=%+v err=%v", result, err)
	}
}
