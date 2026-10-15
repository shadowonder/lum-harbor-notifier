package applicationruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/credential"
	"lum-harbor-notifier/internal/plugin"
)

type aiRuntimeTestInvoker struct{}

func (aiRuntimeTestInvoker) Invoke(context.Context, plugin.Invocation) (json.RawMessage, error) {
	return nil, errors.New("not invoked by composition tests")
}

type aiRuntimeTestSnapshotter struct {
	snapshot plugin.RuntimeSnapshot
}

type aiRuntimeTestCredentialStore struct {
	value string
	err   error
}

func (s *aiRuntimeTestCredentialStore) Set(context.Context, string) error { return s.err }
func (s *aiRuntimeTestCredentialStore) Exists(context.Context) (bool, error) {
	return s.value != "", s.err
}
func (s *aiRuntimeTestCredentialStore) Delete(context.Context) error { return s.err }
func (s *aiRuntimeTestCredentialStore) Resolve(context.Context) (string, bool, error) {
	return s.value, s.value != "", s.err
}

func composeAITest(registry *plugin.Registry, instances []plugin.Instance, invoker plugin.Invoker, runtime AIRuntimeSnapshotter) (*AIComposition, error) {
	var store credential.OpenAIStore = &aiRuntimeTestCredentialStore{}
	return ComposeAI(context.Background(), registry, instances, invoker, runtime, config.DefaultConfig().AI.BuiltInOpenAI, store)
}

func (s aiRuntimeTestSnapshotter) Snapshot() plugin.RuntimeSnapshot { return s.snapshot }

func TestComposeAINoSelectedProviderIsHonestUnavailableBuiltIn(t *testing.T) {
	registry := mustAIRegistry(t, aiTestExtension("company-ai", "Company AI", true))
	composition, err := composeAITest(registry, nil, aiRuntimeTestInvoker{}, aiRuntimeTestSnapshotter{})
	if err != nil {
		t.Fatal(err)
	}
	projection := composition.Projection()
	if composition.Service == nil {
		t.Fatal("expected central AI service even while built-in execution is unavailable")
	}
	if projection.Status != AIRuntimeUnavailable || projection.ProviderSource != ai.ProviderSourceBuiltIn || !projection.BuiltInConfigEditable {
		t.Fatalf("unexpected projection: %+v", projection)
	}
	if len(projection.Stages) != 1 || projection.Stages[0].Enabled {
		t.Fatalf("unconfigured built-in stage must be present but disabled: %+v", projection.Stages)
	}
	_, err = composition.Service.Execute(context.Background(), ai.Request{
		RequestID: "request-1", Action: ai.ActionSourceClean,
		Messages:     []ai.Message{{Role: ai.RoleUser, Content: "input"}},
		OutputSchema: json.RawMessage(`{"type":"object"}`),
	})
	if !errors.Is(err, ErrBuiltInAINotConfigured) {
		t.Fatalf("expected honest not-configured execution error, got %v", err)
	}
}

func TestComposeAIReconstructsReadyBuiltInProviderFromSavedConfigAndCredential(t *testing.T) {
	registry := mustAIRegistry(t)
	saved := config.BuiltInOpenAIConfig{
		Endpoint: "https://api.openai.com/v1", Model: "gpt-test", RequestTimeoutSeconds: 30,
	}
	store := &aiRuntimeTestCredentialStore{value: "sk-test-only"}
	composition, err := ComposeAI(context.Background(), registry, nil, aiRuntimeTestInvoker{}, aiRuntimeTestSnapshotter{}, saved, store)
	if err != nil {
		t.Fatal(err)
	}
	projection := composition.Projection()
	if composition.Service == nil || projection.Status != AIRuntimeReady || projection.ProviderSource != ai.ProviderSourceBuiltIn {
		t.Fatalf("saved built-in state did not reconstruct a ready runtime: %+v", projection)
	}
	if len(projection.Stages) != 1 || !projection.Stages[0].Enabled || !projection.Stages[0].Available {
		t.Fatalf("ready built-in stage not projected: %+v", projection.Stages)
	}
}

func TestComposeAIStoredCredentialFailureIsUnavailableAndRedacted(t *testing.T) {
	registry := mustAIRegistry(t)
	secret := "sk-never-project-this"
	store := &aiRuntimeTestCredentialStore{err: fmt.Errorf("cannot read %s", secret)}
	composition, err := ComposeAI(context.Background(), registry, nil, aiRuntimeTestInvoker{}, aiRuntimeTestSnapshotter{}, config.BuiltInOpenAIConfig{
		Endpoint: "https://api.openai.com/v1", Model: "gpt-test", RequestTimeoutSeconds: 30,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	projection := composition.Projection()
	if projection.Status != AIRuntimeUnavailable || strings.Contains(projection.Issue, secret) {
		t.Fatalf("credential failure was not safely projected: %+v", projection)
	}
}

func TestComposeAIExactlyOneProviderOwnsGlobalSlot(t *testing.T) {
	extension := aiTestExtension("company-ai", "Company AI", true)
	registry := mustAIRegistry(t, extension)
	instance := plugin.Instance{ID: "provider-prod", ExtensionID: extension.Manifest.ID, CapabilityID: plugin.CapabilityAIProvider}
	runtime := aiRuntimeTestSnapshotter{snapshot: runningSnapshot(extension)}
	composition, err := composeAITest(registry, []plugin.Instance{instance}, aiRuntimeTestInvoker{}, runtime)
	if err != nil {
		t.Fatal(err)
	}
	projection := composition.Projection()
	if composition.Service == nil || projection.Status != AIRuntimeReady || projection.ProviderSource != ai.ProviderSourcePlugin {
		t.Fatalf("unexpected plugin projection: %+v", projection)
	}
	if projection.ManagingExtensionID != extension.Manifest.ID || projection.ManagingProviderInstanceID != instance.ID || projection.BuiltInConfigEditable {
		t.Fatalf("plugin ownership was not projected: %+v", projection)
	}
	status := composition.Service.RuntimeStatus()
	if status.ProviderSource != ai.ProviderSourcePlugin || status.EffectiveProviderID != projection.EffectiveProviderID {
		t.Fatalf("service/projection provider mismatch: status=%+v projection=%+v", status, projection)
	}
}

func TestComposeAIDisabledOrInstalledOnlyProviderCannotTakeOver(t *testing.T) {
	extension := aiTestExtension("company-ai", "Company AI", true)
	registry := mustAIRegistry(t, extension)
	// plugininstance.Resolve omits disabled declarations, so both disabled and
	// installed-but-unselected product states arrive here as no runtime Instance.
	composition, err := composeAITest(registry, nil, aiRuntimeTestInvoker{}, aiRuntimeTestSnapshotter{snapshot: runningSnapshot(extension)})
	if err != nil {
		t.Fatal(err)
	}
	projection := composition.Projection()
	if projection.ProviderSource != ai.ProviderSourceBuiltIn || projection.Mode != ai.RuntimeModeBuiltIn {
		t.Fatalf("unselected extension took over AI: %+v", projection)
	}
}

func TestComposeAIMultipleProvidersProducesConflictWithoutService(t *testing.T) {
	first := aiTestExtension("a-provider", "Provider A", true)
	second := aiTestExtension("b-provider", "Provider B", true)
	registry := mustAIRegistry(t, first, second)
	instances := []plugin.Instance{
		{ID: "a-instance", ExtensionID: first.Manifest.ID, CapabilityID: plugin.CapabilityAIProvider},
		{ID: "b-instance", ExtensionID: second.Manifest.ID, CapabilityID: plugin.CapabilityAIProvider},
	}
	composition, err := composeAITest(registry, instances, aiRuntimeTestInvoker{}, aiRuntimeTestSnapshotter{})
	if err != nil {
		t.Fatal(err)
	}
	projection := composition.Projection()
	if projection.Status != AIRuntimeConflict || composition.Service != nil || projection.EffectiveProviderID != "" {
		t.Fatalf("provider conflict silently selected a runtime: %+v service=%v", projection, composition.Service)
	}
}

func TestComposeAIPipelineUsesBindingOrderAndLiveAvailability(t *testing.T) {
	extension := aiTestExtension("pipeline-ai", "Pipeline AI", true)
	extension.Manifest.Capabilities = append(extension.Manifest.Capabilities,
		plugin.CapabilityDeclaration{ID: plugin.CapabilityAIPreRequest},
		plugin.CapabilityDeclaration{ID: plugin.CapabilityAIPostResponse},
	)
	extension.Manifest.Bindings = append(extension.Manifest.Bindings,
		plugin.BindingDeclaration{ID: "pre-late", Type: plugin.BindingAIPre, Capability: plugin.CapabilityAIPreRequest, Actions: []string{"source.clean"}, Order: 20},
		plugin.BindingDeclaration{ID: "pre-early", Type: plugin.BindingAIPre, Capability: plugin.CapabilityAIPreRequest, Actions: []string{"*"}, Order: 10},
		plugin.BindingDeclaration{ID: "post", Type: plugin.BindingAIPost, Capability: plugin.CapabilityAIPostResponse, Actions: []string{"*"}, Order: 30},
	)
	registry := mustAIRegistry(t, extension)
	instances := []plugin.Instance{
		{ID: "provider", ExtensionID: extension.Manifest.ID, CapabilityID: plugin.CapabilityAIProvider},
		{ID: "pre", ExtensionID: extension.Manifest.ID, CapabilityID: plugin.CapabilityAIPreRequest},
		{ID: "post", ExtensionID: extension.Manifest.ID, CapabilityID: plugin.CapabilityAIPostResponse},
	}
	composition, err := composeAITest(registry, instances, aiRuntimeTestInvoker{}, aiRuntimeTestSnapshotter{})
	if err != nil {
		t.Fatal(err)
	}
	projection := composition.Projection()
	if projection.Status != AIRuntimeUnavailable {
		t.Fatalf("stopped provider process must make runtime unavailable: %+v", projection)
	}
	if len(projection.Stages) != 4 {
		t.Fatalf("expected pre/pre/provider/post, got %+v", projection.Stages)
	}
	wantNames := []string{"pre-early", "pre-late", "Pipeline AI", "post"}
	for index, want := range wantNames {
		if projection.Stages[index].Name != want {
			t.Fatalf("stage %d name=%q want %q; stages=%+v", index, projection.Stages[index].Name, want, projection.Stages)
		}
		if projection.Stages[index].Enabled || projection.Stages[index].Available {
			t.Fatalf("unavailable stage must not be enabled: %+v", projection.Stages[index])
		}
	}
}

func TestComposeAIDisabledAndUnsupportedMiddlewareCannotEnterPipeline(t *testing.T) {
	extension := aiTestExtension("provider-only", "Provider Only", true)
	extension.Manifest.Capabilities = append(extension.Manifest.Capabilities,
		plugin.CapabilityDeclaration{ID: plugin.CapabilityAIPreRequest},
		plugin.CapabilityDeclaration{ID: plugin.CapabilityID("ai.unsupported")},
	)
	extension.Manifest.Bindings = append(extension.Manifest.Bindings,
		plugin.BindingDeclaration{ID: "disabled-pre", Type: plugin.BindingAIPre, Capability: plugin.CapabilityAIPreRequest, Actions: []string{"*"}, Order: 10},
	)
	registry := mustAIRegistry(t, extension)
	instances := []plugin.Instance{
		{ID: "provider", ExtensionID: extension.Manifest.ID, CapabilityID: plugin.CapabilityAIProvider},
		// No enabled ai.pre_request Instance exists. Resolve would also reject
		// this unsupported capability from production; ComposeAI defensively
		// ignores it when supplied directly by a test/caller.
		{ID: "unsupported", ExtensionID: extension.Manifest.ID, CapabilityID: plugin.CapabilityID("ai.unsupported")},
	}
	composition, err := composeAITest(registry, instances, aiRuntimeTestInvoker{}, aiRuntimeTestSnapshotter{snapshot: runningSnapshot(extension)})
	if err != nil {
		t.Fatal(err)
	}
	projection := composition.Projection()
	if len(projection.Stages) != 1 || projection.Stages[0].Stage != "PROVIDER" {
		t.Fatalf("disabled/unsupported middleware entered runtime: %+v", projection.Stages)
	}
}

func mustAIRegistry(t *testing.T, extensions ...plugin.Extension) *plugin.Registry {
	t.Helper()
	registry, err := plugin.NewRegistry(extensions)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func aiTestExtension(id, name string, provider bool) plugin.Extension {
	extension := plugin.Extension{Manifest: plugin.Manifest{ID: id, Name: name, Version: "1.0.0"}}
	if provider {
		extension.Manifest.Capabilities = append(extension.Manifest.Capabilities, plugin.CapabilityDeclaration{ID: plugin.CapabilityAIProvider})
		extension.Manifest.Bindings = append(extension.Manifest.Bindings, plugin.BindingDeclaration{
			ID: "provider", Type: plugin.BindingAIProvider, Capability: plugin.CapabilityAIProvider,
			Actions: []string{"*"}, Order: 20,
		})
	}
	return extension
}

func runningSnapshot(extensions ...plugin.Extension) plugin.RuntimeSnapshot {
	result := plugin.RuntimeSnapshot{}
	for _, extension := range extensions {
		result.Extensions = append(result.Extensions, plugin.ExtensionRuntimeSnapshot{ID: extension.Manifest.ID, Running: true, Status: "RUNNING"})
	}
	return result
}

func TestComposeAIInvalidActivePluginProjectsConfigurationErrorAndUnavailable(t *testing.T) {
	extension := plugin.Extension{
		Manifest: plugin.Manifest{
			ID: "lh-plugin-ai-runtime", Name: "AI Runtime Takeover", Version: "0.1.0",
			Capabilities: []plugin.CapabilityDeclaration{{ID: plugin.CapabilityAIProvider}},
			Bindings: []plugin.BindingDeclaration{{
				ID: "provider", Type: plugin.BindingAIProvider, Capability: plugin.CapabilityAIProvider,
				Actions: []string{"*"}, Order: 100,
			}},
		},
	}
	registry := mustAIRegistry(t, extension)
	// Missing base_url and model with mock_mode=false
	instance := plugin.Instance{
		ID: "provider-invalid", ExtensionID: extension.Manifest.ID, CapabilityID: plugin.CapabilityAIProvider,
		Config: json.RawMessage(`{"mock_mode":false}`),
	}
	runtime := aiRuntimeTestSnapshotter{snapshot: runningSnapshot(extension)}
	composition, err := composeAITest(registry, []plugin.Instance{instance}, aiRuntimeTestInvoker{}, runtime)
	if err != nil {
		t.Fatal(err)
	}
	projection := composition.Projection()
	if projection.Status != AIRuntimeUnavailable {
		t.Fatalf("expected status UNAVAILABLE, got %q", projection.Status)
	}
	if !strings.Contains(projection.Issue, "configuration error") {
		t.Fatalf("expected issue to mention configuration error, got %q", projection.Issue)
	}
	if projection.ManagingExtensionID != extension.Manifest.ID || projection.ManagingExtensionName != extension.Manifest.Name {
		t.Fatalf("expected managing extension info preserved: %+v", projection)
	}
}

func TestActualShippedManifestAIProviderTakeoverComposition(t *testing.T) {
	manifestCandidates := []string{
		filepath.Join("..", "..", "..", "lh-plugin-ai-runtime", "plugin.json"),
		filepath.Join("..", "..", "lh-plugin-ai-runtime", "plugin.json"),
		filepath.Join("..", "lh-plugin-ai-runtime", "plugin.json"),
	}
	var data []byte
	var manifestPath string
	for _, candidate := range manifestCandidates {
		if content, err := os.ReadFile(candidate); err == nil {
			data = content
			manifestPath = candidate
			break
		}
	}
	if data == nil {
		t.Fatalf("could not locate lh-plugin-ai-runtime/plugin.json in candidates: %v", manifestCandidates)
	}
	var manifest plugin.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse shipped manifest: %v", err)
	}

	// Prove real shipped manifest declares the high-level AI_RUNTIME binding.
	foundBinding := false
	for _, b := range manifest.Bindings {
		if b.Type == plugin.BindingAIRuntime && b.Capability == plugin.CapabilityAIRuntime {
			foundBinding = true
			break
		}
	}
	if !foundBinding {
		t.Fatalf("shipped manifest %s is missing AI_RUNTIME binding: %+v", manifestPath, manifest.Bindings)
	}

	extension := plugin.Extension{
		Manifest: manifest,
	}
	registry := mustAIRegistry(t, extension)
	instance := plugin.Instance{
		ID:           "test-ai-runtime-instance",
		ExtensionID:  manifest.ID,
		CapabilityID: plugin.CapabilityAIRuntime,
		Config:       json.RawMessage(`{"mock_mode":true}`),
	}
	runtime := aiRuntimeTestSnapshotter{snapshot: runningSnapshot(extension)}
	composition, err := composeAITest(registry, []plugin.Instance{instance}, aiRuntimeTestInvoker{}, runtime)
	if err != nil {
		t.Fatal(err)
	}
	projection := composition.Projection()
	if projection.Mode != ai.RuntimeModePluginRuntime {
		t.Fatalf("expected Mode %q, got %q", ai.RuntimeModePluginRuntime, projection.Mode)
	}
	if projection.ProviderSource != ai.ProviderSourcePlugin {
		t.Fatalf("expected ProviderSource %q, got %q", ai.ProviderSourcePlugin, projection.ProviderSource)
	}
	if projection.ManagingExtensionID != manifest.ID {
		t.Fatalf("expected ManagingExtensionID %q, got %q", manifest.ID, projection.ManagingExtensionID)
	}
	if projection.Status != AIRuntimeReady {
		t.Fatalf("expected Status READY, got %q (issue: %s)", projection.Status, projection.Issue)
	}
}
