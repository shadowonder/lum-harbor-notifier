//go:build plugincompat

package plugincompat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/aiadapter"
	"lum-harbor-notifier/internal/plugininstall"
)

const (
	aiOrchestratorExtensionID         = "lh-plugin-ai-orchestrator"
	aiOrchestratorArtifactEnvironment = "LUM_HARBOR_AI_ORCHESTRATOR_PLUGIN_COMPAT_ARTIFACT"
)

func TestActualAIOrchestratorPluginProtocolAndPipeline(t *testing.T) {
	artifactPath := findAIOrchestratorArtifact(t)
	var stageMu sync.Mutex
	var stages []string
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		stage := request.Header.Get("X-Lum-Harbor-Pipeline-Stage")
		stageMu.Lock()
		stages = append(stages, stage)
		stageMu.Unlock()
		content := stage + " completed"
		if stage == "synthesize" {
			content = `{"version":1,"kind":"ANSWER","text":"Core received one orchestrated response.","candidate":null}`
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}},
			"usage":   map[string]any{"prompt_tokens": 6, "completion_tokens": 4, "total_tokens": 10},
		})
	}))
	defer upstream.Close()

	pluginRoot := t.TempDir()
	installer, err := plugininstall.New(pluginRoot)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := installer.Install(artifactPath)
	if err != nil {
		t.Fatalf("install orchestrator: %v", err)
	}
	if installed.ExtensionID != aiOrchestratorExtensionID || !installed.RestartRequired {
		t.Fatalf("install result = %+v", installed)
	}
	registry, failures, err := plugin.Discover(pluginRoot)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover orchestrator: err=%v failures=%+v", err, failures)
	}
	extension, err := registry.Find(aiOrchestratorExtensionID)
	if err != nil {
		t.Fatal(err)
	}
	if extension.Manifest.Name != "Lum Harbor AI Orchestrator" || strings.Contains(strings.ToLower(extension.Manifest.Name), "sample") {
		t.Fatalf("display name = %q", extension.Manifest.Name)
	}
	if len(extension.Manifest.Bindings) != 1 || extension.Manifest.Bindings[0].Type != plugin.BindingAIProvider || extension.Manifest.Bindings[0].Capability != plugin.CapabilityAIProvider {
		t.Fatalf("bindings = %+v", extension.Manifest.Bindings)
	}

	var logs bytes.Buffer
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout: 3 * time.Second, RequestTimeout: 8 * time.Second, ShutdownTimeout: 2 * time.Second,
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := host.Start(ctx, aiOrchestratorExtensionID); err != nil {
		t.Fatalf("start orchestrator: %v", err)
	}
	defer func() { _ = host.Shutdown(ctx) }()

	instance := plugin.Instance{
		ID: "orchestrator-provider", ExtensionID: aiOrchestratorExtensionID, CapabilityID: plugin.CapabilityAIProvider,
		Config: json.RawMessage(`{"base_url":` + strconvQuote(upstream.URL+"/v1") + `,"model":"compat-model","timeout_seconds":5,"enable_retrieval":false}`),
	}
	provider := aiadapter.NewProvider(host, aiOrchestratorExtensionID, &instance)
	response, err := provider.Execute(ctx, ai.Request{
		RequestID: "orchestrator-compat-1",
		Action:    ai.ActionAssistantAsk,
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: "Return the requested canonical answer."},
			{Role: ai.RoleUser, Content: "What is the next bounded action?"},
		},
		OutputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["version","kind","text","candidate"],"properties":{"version":{"const":1},"kind":{"type":"string"},"text":{"type":"string"},"candidate":{"type":["object","null"]}}}`),
	})
	if err != nil {
		t.Fatalf("execute orchestrator: %v", err)
	}
	if response.RequestID != "orchestrator-compat-1" || response.Action != ai.ActionAssistantAsk || !bytes.Contains(response.Output, []byte("one orchestrated response")) {
		t.Fatalf("response = %+v", response)
	}
	if response.Metadata["pipeline_model_calls"] != "3" || response.Metadata["provider"] != aiOrchestratorExtensionID {
		t.Fatalf("metadata = %+v", response.Metadata)
	}
	stageMu.Lock()
	gotStages := append([]string(nil), stages...)
	stageMu.Unlock()
	if len(gotStages) != 3 || !stringSliceContains(gotStages, "analysis.intent") || !stringSliceContains(gotStages, "analysis.risk") || !stringSliceContains(gotStages, "synthesize") {
		t.Fatalf("upstream stages = %v", gotStages)
	}
	if err := host.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown orchestrator: %v", err)
	}
	if !strings.Contains(logs.String(), aiOrchestratorExtensionID+": started") || !strings.Contains(logs.String(), "stage=synthesize") || !strings.Contains(logs.String(), "shutdown accepted") {
		t.Fatalf("diagnostics = %s", logs.String())
	}
}

func findAIOrchestratorArtifact(t *testing.T) string {
	t.Helper()
	configured := strings.TrimSpace(os.Getenv(aiOrchestratorArtifactEnvironment))
	if configured != "" {
		absolute, err := filepath.Abs(configured)
		if err != nil {
			t.Fatal(err)
		}
		return absolute
	}
	candidates := []string{
		filepath.Join("..", "..", "lh-plugin-ai-orchestrator", "dist", "lh-plugin-ai-orchestrator.lhplugin"),
		filepath.Join("..", "..", "..", "lh-plugin-ai-orchestrator", "dist", "lh-plugin-ai-orchestrator.lhplugin"),
		filepath.Join("..", "lh-plugin-ai-orchestrator", "dist", "lh-plugin-ai-orchestrator.lhplugin"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			absolute, err := filepath.Abs(candidate)
			if err == nil {
				return absolute
			}
		}
	}
	t.Skipf("AI orchestrator artifact not found; set %s", aiOrchestratorArtifactEnvironment)
	return ""
}

func stringSliceContains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func strconvQuote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
