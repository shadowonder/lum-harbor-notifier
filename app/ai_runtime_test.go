package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/airuntime"
	"lum-harbor-notifier/internal/applicationruntime"
	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/credential"
	"lum-harbor-notifier/internal/openai"
	"lum-harbor-notifier/internal/plugin"
)

type staticAIControlPlane struct {
	projection applicationruntime.AIProjection
}

func (s staticAIControlPlane) Projection() applicationruntime.AIProjection { return s.projection }

type memoryOpenAICredentialStore struct {
	value string
	err   error
}

func (s *memoryOpenAICredentialStore) Set(_ context.Context, value string) error {
	if s.err != nil {
		return s.err
	}
	s.value = value
	return nil
}
func (s *memoryOpenAICredentialStore) Exists(context.Context) (bool, error) {
	return s.value != "", s.err
}
func (s *memoryOpenAICredentialStore) Delete(context.Context) error {
	if s.err != nil {
		return s.err
	}
	s.value = ""
	return nil
}
func (s *memoryOpenAICredentialStore) Resolve(context.Context) (string, bool, error) {
	return s.value, s.value != "", s.err
}

type recordingAIExecutor struct {
	request ai.Request
	output  json.RawMessage
	err     error
}

type recordingHighLevelRuntime struct {
	request airuntime.Request
	output  json.RawMessage
	err     error
}

func (r *recordingHighLevelRuntime) Execute(_ context.Context, request airuntime.Request) (airuntime.Result, error) {
	r.request = request
	return airuntime.Result{RequestID: request.RequestID, Purpose: request.Purpose, Output: r.output}, r.err
}

type restartTestInvoker struct{}

func (restartTestInvoker) Invoke(context.Context, plugin.Invocation) (json.RawMessage, error) {
	return nil, errors.New("not invoked while composing restart test")
}

type restartTestSnapshotter struct{}

func (restartTestSnapshotter) Snapshot() plugin.RuntimeSnapshot { return plugin.RuntimeSnapshot{} }

func (e *recordingAIExecutor) Execute(_ context.Context, request ai.Request) (ai.Response, error) {
	e.request = request
	return ai.Response{RequestID: request.RequestID, Action: request.Action, Output: e.output}, e.err
}

func TestGetAIRuntimeProjectsBackendCompositionWithoutInference(t *testing.T) {
	application := &App{aiControlPlane: staticAIControlPlane{projection: applicationruntime.AIProjection{
		Status: applicationruntime.AIRuntimeReady, Mode: ai.RuntimeModePluginProvider,
		EffectiveProviderID: "company:prod:provider", EffectiveProviderName: "Company AI",
		ProviderSource: ai.ProviderSourcePlugin, BuiltInConfigEditable: false,
		ManagingExtensionID: "company", ManagingExtensionName: "Company Extension",
		ManagingProviderInstanceID: "prod",
		Stages: []applicationruntime.AIStageProjection{{
			ID: "company:prod:provider", Name: "Company AI", Stage: "PROVIDER", Order: 20,
			AllActions: true, Enabled: true, Available: true, ExtensionID: "company",
			ExtensionName: "Company Extension", InstanceID: "prod",
		}},
	}}}
	dto, err := application.GetAIRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if dto.Status != applicationruntime.AIRuntimeReady || dto.ProviderSource != "PLUGIN" || dto.BuiltInConfigEditable {
		t.Fatalf("unexpected DTO: %+v", dto)
	}
	if len(dto.Stages) != 1 || !dto.Stages[0].Enabled || dto.Stages[0].ExtensionID != "company" {
		t.Fatalf("unexpected stages: %+v", dto.Stages)
	}
}

func TestGetAIRuntimeFailsClosedWhenControlPlaneMissing(t *testing.T) {
	dto, err := (&App{}).GetAIRuntime()
	if err == nil {
		t.Fatalf("expected missing control-plane error, got %+v", dto)
	}
	if dto.Status == applicationruntime.AIRuntimeReady {
		t.Fatalf("missing control plane must never return fake READY: %+v", dto)
	}
}

func TestBuiltInOpenAISettingsAndCredentialAreNextStartWriteOnlyState(t *testing.T) {
	setupIsolatedConfigHome(t)
	store := &memoryOpenAICredentialStore{}
	current := staticAIControlPlane{projection: applicationruntime.AIProjection{
		Status: applicationruntime.AIRuntimeUnavailable, Mode: ai.RuntimeModeBuiltIn,
		EffectiveProviderID: "builtin-openai", ProviderSource: ai.ProviderSourceBuiltIn,
		BuiltInConfigEditable: true, Issue: "Built-in OpenAI configuration is missing.",
	}}
	application := &App{
		aiControlPlane: current, aiCredentialStoreResolver: func(bool) credential.OpenAIStore { return store },
		startupAIConfig: config.DefaultConfig().AI.BuiltInOpenAI,
	}

	saved, err := application.SaveBuiltInOpenAISettings(SaveBuiltInOpenAISettingsRequestDTO{
		Endpoint: "https://api.openai.com/v1", Model: "gpt-test", RequestTimeoutSeconds: 45,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.RestartRequired || saved.CredentialConfigured {
		t.Fatalf("unexpected saved state: %+v", saved)
	}
	credentialState, err := application.SetBuiltInOpenAICredential(SetBuiltInOpenAICredentialRequestDTO{Credential: "sk-never-return-me"})
	if err != nil {
		t.Fatal(err)
	}
	if !credentialState.CredentialConfigured || !credentialState.RestartRequired {
		t.Fatalf("unexpected credential state: %+v", credentialState)
	}
	settings, err := application.GetBuiltInOpenAISettings()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "sk-never-return-me") || !settings.CredentialConfigured {
		t.Fatalf("credential leaked or configured state missing: %s", encoded)
	}
	runtime, err := application.GetAIRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Status != applicationruntime.AIRuntimeUnavailable {
		t.Fatalf("saving next-start state hot-reconfigured the current runtime: %+v", runtime)
	}
	persisted, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	persistedJSON, _ := json.Marshal(persisted)
	if strings.Contains(string(persistedJSON), "sk-never-return-me") || persisted.AI.BuiltInOpenAI.Model != "gpt-test" {
		t.Fatalf("secret entered config or non-secret config was lost: %s", persistedJSON)
	}
	deleted, err := application.DeleteBuiltInOpenAICredential()
	if err != nil {
		t.Fatal(err)
	}
	if deleted.CredentialConfigured || store.value != "" {
		t.Fatalf("credential delete failed: state=%+v value=%q", deleted, store.value)
	}
}

func TestBuiltInOpenAIRestartReconstructsFreshReadyRuntime(t *testing.T) {
	setupIsolatedConfigHome(t)
	store := &memoryOpenAICredentialStore{}
	lifetimeOne := &App{
		aiControlPlane: staticAIControlPlane{projection: applicationruntime.AIProjection{
			Status: applicationruntime.AIRuntimeUnavailable, EffectiveProviderID: "builtin-openai",
			ProviderSource: ai.ProviderSourceBuiltIn, BuiltInConfigEditable: true,
		}},
		aiCredentialStoreResolver: func(bool) credential.OpenAIStore { return store },
		startupAIConfig:           config.DefaultConfig().AI.BuiltInOpenAI,
	}
	if _, err := lifetimeOne.SaveBuiltInOpenAISettings(SaveBuiltInOpenAISettingsRequestDTO{
		Endpoint: "https://api.openai.com/v1", Model: "gpt-test", RequestTimeoutSeconds: 30,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := lifetimeOne.SetBuiltInOpenAICredential(SetBuiltInOpenAICredentialRequestDTO{Credential: "restart-test-value"}); err != nil {
		t.Fatal(err)
	}
	current, err := lifetimeOne.GetAIRuntime()
	if err != nil || current.Status != applicationruntime.AIRuntimeUnavailable {
		t.Fatalf("lifetime one runtime changed without restart: runtime=%+v err=%v", current, err)
	}

	loadedAfterRestart, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := plugin.NewRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	lifetimeTwo, err := applicationruntime.ComposeAI(
		context.Background(), registry, nil, restartTestInvoker{}, restartTestSnapshotter{},
		loadedAfterRestart.AI.BuiltInOpenAI, store,
	)
	if err != nil {
		t.Fatal(err)
	}
	if projection := lifetimeTwo.Projection(); projection.Status != applicationruntime.AIRuntimeReady || projection.EffectiveProviderID != "builtin-openai" {
		t.Fatalf("fresh lifetime did not reconstruct built-in runtime: %+v", projection)
	}
}

// TestBuiltInOpenAICredentialStoreSelectionFollowsDesiredStoreNotStartupStore
// exercises the full desired-store selection contract from M8.5 Slice 2:
// Set/Delete/Exists always act on whatever store the currently saved
// UseSystemCredentialManager selects, switching never auto-migrates a
// secret between stores, and Delete only ever touches the currently
// selected store, never the inactive one.
func TestBuiltInOpenAICredentialStoreSelectionFollowsDesiredStoreNotStartupStore(t *testing.T) {
	setupIsolatedConfigHome(t)
	systemStore := &memoryOpenAICredentialStore{}
	fallbackStore := &memoryOpenAICredentialStore{}
	resolver := func(useSystemCredentialManager bool) credential.OpenAIStore {
		if useSystemCredentialManager {
			return systemStore
		}
		return fallbackStore
	}
	application := &App{
		aiCredentialStoreResolver: resolver,
		startupAIConfig:           config.DefaultConfig().AI.BuiltInOpenAI,
	}

	if _, err := application.SaveBuiltInOpenAISettings(SaveBuiltInOpenAISettingsRequestDTO{
		Endpoint: "https://api.openai.com/v1", Model: "gpt-test", RequestTimeoutSeconds: 30,
		UseSystemCredentialManager: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetBuiltInOpenAICredential(SetBuiltInOpenAICredentialRequestDTO{Credential: "system-secret"}); err != nil {
		t.Fatal(err)
	}
	if systemStore.value != "system-secret" || fallbackStore.value != "" {
		t.Fatalf("expected Set to target only the system store: system=%q fallback=%q", systemStore.value, fallbackStore.value)
	}
	settings, err := application.GetBuiltInOpenAISettings()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.UseSystemCredentialManager || !settings.CredentialConfigured {
		t.Fatalf("expected system store credential to be reported configured: %+v", settings)
	}

	// Switch the desired store to the application-managed fallback. No
	// restart has happened, so the fallback genuinely has no credential yet
	// and Settings must honestly report that, even though the system store
	// (no longer selected) still holds its own secret untouched.
	if _, err := application.SaveBuiltInOpenAISettings(SaveBuiltInOpenAISettingsRequestDTO{
		Endpoint: "https://api.openai.com/v1", Model: "gpt-test", RequestTimeoutSeconds: 30,
		UseSystemCredentialManager: false,
	}); err != nil {
		t.Fatal(err)
	}
	settings, err = application.GetBuiltInOpenAISettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.UseSystemCredentialManager || settings.CredentialConfigured {
		t.Fatalf("expected fallback store to be reported as missing, no auto-migration: %+v", settings)
	}
	if systemStore.value != "system-secret" {
		t.Fatalf("expected switching store mode to leave the system store untouched, got %q", systemStore.value)
	}

	if _, err := application.SetBuiltInOpenAICredential(SetBuiltInOpenAICredentialRequestDTO{Credential: "fallback-secret"}); err != nil {
		t.Fatal(err)
	}
	if fallbackStore.value != "fallback-secret" || systemStore.value != "system-secret" {
		t.Fatalf("expected Set to target only the fallback store: system=%q fallback=%q", systemStore.value, fallbackStore.value)
	}

	// Switch back to the system store. Its original secret must still be
	// there (never overwritten by the fallback secret).
	if _, err := application.SaveBuiltInOpenAISettings(SaveBuiltInOpenAISettingsRequestDTO{
		Endpoint: "https://api.openai.com/v1", Model: "gpt-test", RequestTimeoutSeconds: 30,
		UseSystemCredentialManager: true,
	}); err != nil {
		t.Fatal(err)
	}
	settings, err = application.GetBuiltInOpenAISettings()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.UseSystemCredentialManager || !settings.CredentialConfigured {
		t.Fatalf("expected the system store's original credential to reappear: %+v", settings)
	}

	// Delete while the system store is selected must only remove the system
	// store's credential, never the fallback's.
	if _, err := application.DeleteBuiltInOpenAICredential(); err != nil {
		t.Fatal(err)
	}
	if systemStore.value != "" {
		t.Fatalf("expected delete to clear the system store, got %q", systemStore.value)
	}
	if fallbackStore.value != "fallback-secret" {
		t.Fatalf("expected delete to leave the fallback store untouched, got %q", fallbackStore.value)
	}
}

func TestCredentialStoreErrorsDoNotLeakSecrets(t *testing.T) {
	setupIsolatedConfigHome(t)
	secret := "sk-sensitive-store-error"
	store := &memoryOpenAICredentialStore{err: errors.New("failed around " + secret)}
	application := &App{aiCredentialStoreResolver: func(bool) credential.OpenAIStore { return store }}
	_, err := application.SetBuiltInOpenAICredential(SetBuiltInOpenAICredentialRequestDTO{Credential: secret})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("expected redacted store failure, got %v", err)
	}
	_, err = application.DeleteBuiltInOpenAICredential()
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("expected redacted delete failure, got %v", err)
	}
}

func TestVerifyAIRuntimeUsesCanonicalServiceAction(t *testing.T) {
	executor := &recordingAIExecutor{output: json.RawMessage(`{"ok":true}`)}
	application := &App{
		aiControlPlane: staticAIControlPlane{projection: applicationruntime.AIProjection{
			Status: applicationruntime.AIRuntimeReady, EffectiveProviderID: "builtin-openai",
			EffectiveProviderName: "Built-in OpenAI", ProviderSource: ai.ProviderSourceBuiltIn,
		}},
		aiExecutor: executor,
	}
	result, err := application.VerifyAIRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if !result.Success || executor.request.Action != ai.ActionRuntimeVerify || len(executor.request.OutputSchema) == 0 {
		t.Fatalf("verification bypassed canonical request contract: result=%+v request=%+v", result, executor.request)
	}
}

func TestVerifyAIRuntimeUsesSelectedHighLevelRuntime(t *testing.T) {
	runtime := &recordingHighLevelRuntime{output: json.RawMessage(`{"ok":true}`)}
	application := &App{
		aiControlPlane: staticAIControlPlane{projection: applicationruntime.AIProjection{
			Status: applicationruntime.AIRuntimeReady, Mode: ai.RuntimeModePluginRuntime,
			EffectiveProviderName: "Sample AI Runtime Takeover",
		}},
		aiRuntime: runtime,
	}
	result, err := application.VerifyAIRuntime()
	if err != nil || !result.Success {
		t.Fatalf("verification=%+v err=%v", result, err)
	}
	if runtime.request.Purpose != ai.ActionRuntimeVerify || runtime.request.Metadata["caller"] != "settings-verification" || len(runtime.request.OutputSchema) == 0 {
		t.Fatalf("high-level runtime request=%+v", runtime.request)
	}
}

func TestVerifyAIRuntimeFailsClosedAndRedactsProviderFailures(t *testing.T) {
	unavailable := &App{aiControlPlane: staticAIControlPlane{projection: applicationruntime.AIProjection{
		Status: applicationruntime.AIRuntimeUnavailable, Issue: "Credential missing.",
	}}}
	result, err := unavailable.VerifyAIRuntime()
	if err == nil || result.Success {
		t.Fatalf("missing executor must fail closed: result=%+v err=%v", result, err)
	}

	executor := &recordingAIExecutor{err: fmt.Errorf("%w: sk-provider-secret", openai.ErrAuthentication)}
	ready := &App{
		aiControlPlane: staticAIControlPlane{projection: applicationruntime.AIProjection{Status: applicationruntime.AIRuntimeReady}},
		aiExecutor:     executor,
	}
	result, err = ready.VerifyAIRuntime()
	if err != nil || result.Success || strings.Contains(result.Message, "sk-provider-secret") || result.Message != "OpenAI credential was rejected." {
		t.Fatalf("provider failure was not safely projected: result=%+v err=%v", result, err)
	}
}
