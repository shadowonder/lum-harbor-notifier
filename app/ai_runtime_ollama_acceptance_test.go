package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/appdir"
	"lum-harbor-notifier/internal/applicationruntime"
	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/credential"
	"lum-harbor-notifier/internal/platform"
	"lum-harbor-notifier/internal/plugin"
)

// TestAIRuntimeOllamaAcceptance tests the real Lum Harbor production AI path
// against a live local Ollama instance (qwen3:8b) using the built-in
// OpenAI-compatible provider with zero production-code changes.
//
// Gated via LUM_HARBOR_OLLAMA_ACCEPTANCE=1.
// If the environment variable is not set, the test is skipped.
// If set and Ollama is unavailable, the test fails explicitly.
func TestAIRuntimeOllamaAcceptance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real Ollama acceptance test in -short mode")
	}
	if os.Getenv("LUM_HARBOR_OLLAMA_ACCEPTANCE") != "1" && strings.ToLower(os.Getenv("LUM_HARBOR_OLLAMA_ACCEPTANCE")) != "true" {
		t.Skip("skipping real Ollama acceptance test; set LUM_HARBOR_OLLAMA_ACCEPTANCE=1 to run")
	}

	const (
		ollamaBaseURL    = "http://localhost:11434"
		ollamaV1Endpoint = "http://localhost:11434/v1"
		targetModel      = "qwen3:8b"
		dummyCredential  = "ollama"
		timeoutSeconds   = 120
	)

	// Preflight check: verify real Ollama container availability and model presence.
	preflightOllama(t, ollamaBaseURL, targetModel)

	// Step 1: Establish strict test isolation.
	// Redirect HOME and XDG_CONFIG_HOME to a temporary test directory so
	// config.json, application databases, and credential files are written
	// exclusively to t.TempDir().
	setupIsolatedConfigHome(t)

	// Step 2: Seed isolated configuration with UseSystemCredentialManager = false.
	// This guarantees macOS Keychain is never accessed or modified, using
	// Lum Harbor's isolated application-managed fallback credential store instead.
	cfg := config.DefaultConfig()
	cfg.AI.BuiltInOpenAI = config.BuiltInOpenAIConfig{
		Endpoint:                   ollamaV1Endpoint,
		Model:                      targetModel,
		RequestTimeoutSeconds:      timeoutSeconds,
		UseSystemCredentialManager: false,
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("save isolated config: %v", err)
	}

	// Step 3: Store the harmless dummy credential ("ollama") in the fallback store.
	// Ollama ignores bearer tokens, but Lum Harbor's built-in provider contract
	// requires a non-empty credential for readiness.
	store := credential.NewOpenAIStore(false)
	ctx := context.Background()
	if err := store.Set(ctx, dummyCredential); err != nil {
		t.Fatalf("set dummy credential in isolated fallback store: %v", err)
	}

	// Verify the credential file was stored inside the isolated temp directory.
	configDir, err := appdir.ConfigDir()
	if err != nil {
		t.Fatalf("resolve isolated config directory: %v", err)
	}
	credFilePath := filepath.Join(configDir, "credentials", "builtin-openai-credential.secret")
	if _, err := os.Stat(credFilePath); err != nil {
		t.Fatalf("expected credential file in isolated directory %q, got error: %v", credFilePath, err)
	}

	// Step 4: Assemble the production composition root.
	// Follows the exact main.go path:
	// plugin registry -> ComposeAI -> NewApp -> GetAIRuntime -> VerifyAIRuntime.
	registry, err := plugin.NewRegistry(nil)
	if err != nil {
		t.Fatalf("create empty plugin registry: %v", err)
	}
	snapshotter := restartTestSnapshotter{}
	invoker := restartTestInvoker{}

	composition, err := applicationruntime.ComposeAI(
		ctx,
		registry,
		nil, // no plugin instances; testing pure built-in provider
		invoker,
		snapshotter,
		cfg.AI.BuiltInOpenAI,
		store,
	)
	if err != nil {
		t.Fatalf("production ComposeAI failed: %v", err)
	}

	application := NewApp(
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		NewRuntimeConfigDTO(cfg.General, cfg.Dashboard.TodoDisplayLimit, platform.Current(), cfg.Notifications.InApp),
		nil,
		composition,
		composition,
		nil,
		nil,
		nil,
		credential.NewOpenAIStore,
		cfg.AI.BuiltInOpenAI,
		nil,
		nil,
	)

	// Step 5: Assert GetAIRuntime() reflects READY Built-in Provider state.
	runtimeDTO, err := application.GetAIRuntime()
	if err != nil {
		t.Fatalf("GetAIRuntime failed: %v", err)
	}
	if runtimeDTO.Status != applicationruntime.AIRuntimeReady {
		t.Fatalf("expected status READY, got %q (issue: %q)", runtimeDTO.Status, runtimeDTO.Issue)
	}
	if runtimeDTO.EffectiveProviderID != "builtin-openai" {
		t.Fatalf("expected provider ID %q, got %q", "builtin-openai", runtimeDTO.EffectiveProviderID)
	}
	if runtimeDTO.EffectiveProviderName != "Built-in OpenAI" {
		t.Fatalf("expected provider name %q, got %q", "Built-in OpenAI", runtimeDTO.EffectiveProviderName)
	}
	if runtimeDTO.ProviderSource != string(ai.ProviderSourceBuiltIn) {
		t.Fatalf("expected provider source %q, got %q", ai.ProviderSourceBuiltIn, runtimeDTO.ProviderSource)
	}
	if !runtimeDTO.BuiltInConfigEditable {
		t.Fatalf("expected BuiltInConfigEditable true")
	}
	if runtimeDTO.ManagingExtensionID != "" || runtimeDTO.ManagingProviderInstanceID != "" {
		t.Fatalf("unexpected plugin takeover in built-in test: %+v", runtimeDTO)
	}
	if len(runtimeDTO.Stages) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(runtimeDTO.Stages))
	}
	stage := runtimeDTO.Stages[0]
	if stage.ID != "builtin-openai" || stage.Stage != "PROVIDER" || !stage.Enabled || !stage.Available {
		t.Fatalf("unexpected stage projection: %+v", stage)
	}

	// Step 6: Assert GetBuiltInOpenAISettings() reflects desired isolated configuration.
	settingsDTO, err := application.GetBuiltInOpenAISettings()
	if err != nil {
		t.Fatalf("GetBuiltInOpenAISettings failed: %v", err)
	}
	if settingsDTO.Endpoint != ollamaV1Endpoint {
		t.Fatalf("expected endpoint %q, got %q", ollamaV1Endpoint, settingsDTO.Endpoint)
	}
	if settingsDTO.Model != targetModel {
		t.Fatalf("expected model %q, got %q", targetModel, settingsDTO.Model)
	}
	if settingsDTO.RequestTimeoutSeconds != timeoutSeconds {
		t.Fatalf("expected timeout %d, got %d", timeoutSeconds, settingsDTO.RequestTimeoutSeconds)
	}
	if settingsDTO.UseSystemCredentialManager {
		t.Fatalf("expected UseSystemCredentialManager false")
	}
	if !settingsDTO.CredentialConfigured {
		t.Fatalf("expected CredentialConfigured true")
	}

	// Step 7: Execute VerifyAIRuntime() — real network roundtrip to Ollama.
	// This exercises:
	// App.VerifyAIRuntime -> AIComposition.Execute -> ai.Service.Execute ->
	// internal/openai.Provider -> real Ollama /v1/chat/completions ->
	// JSON schema structured output validation -> App.VerifyAIRuntime JSON unmarshal.
	t.Logf("Executing App.VerifyAIRuntime against real Ollama (%s at %s)...", targetModel, ollamaV1Endpoint)
	started := time.Now()
	verification, err := application.VerifyAIRuntime()
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("VerifyAIRuntime returned unexpected Go error: %v", err)
	}

	t.Logf("VerifyAIRuntime finished in %s (reported elapsed: %d ms, success: %v, message: %q)",
		elapsed, verification.ElapsedMilliseconds, verification.Success, verification.Message)

	if !verification.Success {
		t.Fatalf("VerifyAIRuntime failed: message=%q, elapsed=%d ms", verification.Message, verification.ElapsedMilliseconds)
	}
	if verification.EffectiveProviderID != "builtin-openai" {
		t.Fatalf("expected effective provider ID %q, got %q", "builtin-openai", verification.EffectiveProviderID)
	}
	if verification.EffectiveProviderName != "Built-in OpenAI" {
		t.Fatalf("expected effective provider name %q, got %q", "Built-in OpenAI", verification.EffectiveProviderName)
	}
	if verification.Message != "Current AI runtime verified successfully." {
		t.Fatalf("unexpected verification message: %q", verification.Message)
	}
}

// preflightOllama performs an HTTP health check against Ollama's native API
// and verifies that the required model is available locally.
func preflightOllama(t *testing.T, baseURL, requiredModel string) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/api/tags", baseURL))
	if err != nil {
		t.Fatalf("Ollama acceptance requested but Ollama is unavailable at %s: %v", baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Ollama /api/tags returned status %d", resp.StatusCode)
	}

	var tagsResp struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		t.Fatalf("decode Ollama /api/tags response: %v", err)
	}

	found := false
	available := make([]string, 0, len(tagsResp.Models))
	for _, m := range tagsResp.Models {
		available = append(available, m.Name)
		if m.Name == requiredModel || strings.HasPrefix(m.Name, requiredModel+":") {
			found = true
		}
	}
	if !found {
		t.Fatalf("required Ollama model %q not found; available models: %v", requiredModel, available)
	}
}
