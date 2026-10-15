package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/applicationruntime"
	"lum-harbor-notifier/internal/background"
	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/credential"
	"lum-harbor-notifier/internal/platform"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/plugintest"
	"lum-harbor-notifier/internal/plugininstall"
	"lum-harbor-notifier/internal/plugininstance"
)

// TestRealPluginTakeoverRestartPipelineAcceptance exercises the product path,
// not directly constructed AI adapters: InstallPlugin -> persisted Instance
// mutations -> fresh config/discovery/resolve -> real Host subprocess ->
// production ComposeAI -> App.VerifyAIRuntime.
func TestRealPluginTakeoverRestartPipelineAcceptance(t *testing.T) {
	setupIsolatedConfigHome(t)
	pluginRoot := filepath.Join(t.TempDir(), "installed")
	tracePath := filepath.Join(t.TempDir(), "pipeline.trace")
	seed := config.DefaultConfig()
	seed.Plugins.Directory = pluginRoot
	if err := config.Save(seed); err != nil {
		t.Fatal(err)
	}

	artifactParent := t.TempDir()
	artifact := plugintest.WriteExtension(t, artifactParent, plugintest.ExtensionSpec{
		ID: "m8-ai-acceptance", Name: "M8 AI Acceptance",
		Capabilities: []plugin.CapabilityID{
			plugin.CapabilityAIPreRequest,
			plugin.CapabilityAIProvider,
			plugin.CapabilityAIPostResponse,
		},
		Bindings: []plugin.BindingDeclaration{
			{ID: "verify-context", Type: plugin.BindingAIPre, Capability: plugin.CapabilityAIPreRequest, Actions: []string{string(ai.ActionRuntimeVerify)}, Order: 10},
			{ID: "verify-provider", Type: plugin.BindingAIProvider, Capability: plugin.CapabilityAIProvider, Actions: []string{"*"}, Order: 20},
			{ID: "verify-filter", Type: plugin.BindingAIPost, Capability: plugin.CapabilityAIPostResponse, Actions: []string{string(ai.ActionRuntimeVerify)}, Order: 30},
		},
		Schema: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
			{Key: "tracePath", Label: "Acceptance trace", Type: plugin.ConfigString, Required: true},
			{Key: "remoteError", Label: "Controlled provider failure", Type: plugin.ConfigBoolean},
		}},
	})
	installer, err := plugininstall.New(pluginRoot)
	if err != nil {
		t.Fatal(err)
	}
	installingApp := &App{pluginInstaller: installer}
	installed, err := installingApp.InstallPlugin(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if installed.ExtensionID != "m8-ai-acceptance" || !installed.RestartRequired {
		t.Fatalf("unexpected install result: %+v", installed)
	}

	// This fresh discovery represents the post-install lifetime in which the
	// normal Settings mutation boundary persists desired capability Instances.
	registryForConfiguration := discoverAcceptanceRegistry(t, pluginRoot)
	configurationHost := newInventoryHost(t, registryForConfiguration)
	configurationInventory, err := NewPluginInventoryReader(registryForConfiguration, nil, configurationHost)
	if err != nil {
		t.Fatal(err)
	}
	configuringApp := &App{pluginInventory: configurationInventory}
	instanceConfig := map[string]interface{}{"tracePath": tracePath}
	pre := createAcceptanceInstance(t, configuringApp, plugin.CapabilityAIPreRequest, instanceConfig)
	provider := createAcceptanceInstance(t, configuringApp, plugin.CapabilityAIProvider, instanceConfig)
	post := createAcceptanceInstance(t, configuringApp, plugin.CapabilityAIPostResponse, instanceConfig)
	disabledPre := createAcceptanceInstance(t, configuringApp, plugin.CapabilityAIPreRequest, instanceConfig)
	if result, err := configuringApp.SetPluginInstanceEnabled(disabledPre.InstanceID, false); err != nil || !result.RestartRequired || result.Enabled {
		t.Fatalf("disable extra pre instance: result=%+v err=%v", result, err)
	}

	// Lifetime 2 uses only freshly loaded/discovered/resolved/runtime objects.
	lifetimeTwo := startPluginAcceptanceLifetime(t, pluginRoot, installer)
	runtimeDTO, err := lifetimeTwo.application.GetAIRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if runtimeDTO.Status != applicationruntime.AIRuntimeReady || runtimeDTO.ProviderSource != string(ai.ProviderSourcePlugin) || runtimeDTO.ManagingExtensionID != "m8-ai-acceptance" || runtimeDTO.ManagingProviderInstanceID != provider.InstanceID || runtimeDTO.BuiltInConfigEditable {
		t.Fatalf("plugin takeover truth mismatch: %+v", runtimeDTO)
	}
	if len(runtimeDTO.Stages) != 3 || runtimeDTO.Stages[0].InstanceID != pre.InstanceID || runtimeDTO.Stages[1].InstanceID != provider.InstanceID || runtimeDTO.Stages[2].InstanceID != post.InstanceID {
		t.Fatalf("unexpected configured pipeline: %+v", runtimeDTO.Stages)
	}
	for _, stage := range runtimeDTO.Stages {
		if !stage.Enabled || !stage.Available {
			t.Fatalf("started subprocess stage not enabled: %+v", stage)
		}
	}
	inventoryDTO, err := lifetimeTwo.application.GetPluginInventory()
	if err != nil || len(inventoryDTO.Plugins) != 1 || !inventoryDTO.Plugins[0].Running || !inventoryDTO.Plugins[0].Selected || len(inventoryDTO.Plugins[0].Instances) != 4 {
		t.Fatalf("Extensions/runtime identity mismatch: inventory=%+v err=%v", inventoryDTO, err)
	}
	verification, err := lifetimeTwo.application.VerifyAIRuntime()
	if err != nil || !verification.Success || verification.EffectiveProviderID != runtimeDTO.EffectiveProviderID {
		t.Fatalf("canonical plugin verification failed: result=%+v err=%v", verification, err)
	}
	assertAcceptanceTrace(t, tracePath, []string{
		"pre|action=runtime.verify",
		"provider|action=runtime.verify|contextSeen=true",
		"post|action=runtime.verify|contextSeen=true|providerVisited=true",
	})
	lifetimeTwo.stop(t)

	// A fresh failure lifetime changes only persisted provider config. The real
	// subprocess error must reach the central runtime without invoking/falling
	// back to the built-in provider, and ownership must remain with the Plugin.
	failureConfig, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	for index := range failureConfig.Plugins.Instances {
		if failureConfig.Plugins.Instances[index].ID == provider.InstanceID {
			failureConfig.Plugins.Instances[index].Config = json.RawMessage(`{"tracePath":` + mustJSONQuote(t, tracePath) + `,"remoteError":true}`)
		}
	}
	if err := config.Save(failureConfig); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tracePath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	failureLifetime := startPluginAcceptanceLifetime(t, pluginRoot, installer)
	failureResult, err := failureLifetime.application.VerifyAIRuntime()
	if err != nil || failureResult.Success || failureResult.Message != "AI runtime verification failed in the current pipeline." {
		t.Fatalf("unexpected product failure: result=%+v err=%v", failureResult, err)
	}
	failureRuntime, err := failureLifetime.application.GetAIRuntime()
	if err != nil || failureRuntime.ProviderSource != string(ai.ProviderSourcePlugin) || failureRuntime.ManagingProviderInstanceID != provider.InstanceID {
		t.Fatalf("failure silently changed ownership: runtime=%+v err=%v", failureRuntime, err)
	}
	_, executeErr := failureLifetime.composition.Execute(context.Background(), acceptanceVerifyRequest("failure-proof"))
	if !errors.Is(executeErr, plugin.ErrRemote) || errors.Is(executeErr, applicationruntime.ErrBuiltInAINotConfigured) {
		t.Fatalf("expected real Plugin error without built-in fallback, got %v", executeErr)
	}
	assertAcceptanceTracePrefix(t, tracePath, []string{
		"pre|action=runtime.verify",
		"ai.provider|error",
	})
}

type pluginAcceptanceLifetime struct {
	application *App
	composition *applicationruntime.AIComposition
	runtime     *applicationruntime.Runtime
}

func startPluginAcceptanceLifetime(t *testing.T, pluginRoot string, installer *plugininstall.Installer) *pluginAcceptanceLifetime {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Plugins.Directory != pluginRoot {
		t.Fatalf("fresh config did not preserve plugin root: %q", cfg.Plugins.Directory)
	}
	registry := discoverAcceptanceRegistry(t, pluginRoot)
	instances, err := plugininstance.Resolve(cfg.Plugins.Instances, registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 3 {
		t.Fatalf("disabled instance participated after restart: %+v", instances)
	}
	host := newInventoryHost(t, registry)
	runtime, err := applicationruntime.New(background.NewScheduler(), host, instances, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryOpenAICredentialStore{}
	composition, err := applicationruntime.ComposeAI(context.Background(), registry, instances, host, host, cfg.AI.BuiltInOpenAI, store)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	inventory, err := NewPluginInventoryReader(registry, cfg.Plugins.Instances, host)
	if err != nil {
		t.Fatal(err)
	}
	application := NewApp(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, NewRuntimeConfigDTO(cfg.General, cfg.Dashboard.TodoDisplayLimit, platform.Current(), cfg.Notifications.InApp), inventory, composition, composition, nil, nil, nil, func(bool) credential.OpenAIStore { return store }, cfg.AI.BuiltInOpenAI, installer, runtime)
	application.Startup(context.Background())
	lifetime := &pluginAcceptanceLifetime{application: application, composition: composition, runtime: runtime}
	t.Cleanup(func() { lifetime.stop(t) })
	return lifetime
}

func (l *pluginAcceptanceLifetime) stop(t *testing.T) {
	t.Helper()
	if err := l.runtime.Shutdown(); err != nil {
		t.Errorf("shutdown acceptance lifetime: %v", err)
	}
	if err := l.runtime.Wait(); err != nil {
		t.Errorf("wait acceptance lifetime: %v", err)
	}
}

func discoverAcceptanceRegistry(t *testing.T, root string) *plugin.Registry {
	t.Helper()
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover acceptance plugin: err=%v failures=%v", err, failures)
	}
	return registry
}

func createAcceptanceInstance(t *testing.T, application *App, capability plugin.CapabilityID, values map[string]interface{}) PluginInstanceCreationResultDTO {
	t.Helper()
	result, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "m8-ai-acceptance", CapabilityID: string(capability), Config: values,
	})
	if err != nil || !result.RestartRequired {
		t.Fatalf("create %s instance: result=%+v err=%v", capability, result, err)
	}
	return result
}

func acceptanceVerifyRequest(id string) ai.Request {
	return ai.Request{
		RequestID: id, Action: ai.ActionRuntimeVerify,
		Messages:     []ai.Message{{Role: ai.RoleUser, Content: "Return {\"ok\":true}."}},
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"ok":{"const":true}},"required":["ok"],"additionalProperties":false}`),
	}
}

func assertAcceptanceTrace(t *testing.T, path string, want []string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("pipeline trace mismatch:\n got %q\nwant %q", got, want)
	}
}

func assertAcceptanceTracePrefix(t *testing.T, path string, want []string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(got) < len(want) || strings.Join(got[:len(want)], "\n") != strings.Join(want, "\n") {
		t.Fatalf("pipeline trace prefix mismatch:\n got %q\nwant prefix %q", got, want)
	}
}

func mustJSONQuote(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
