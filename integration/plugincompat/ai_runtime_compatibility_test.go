//go:build plugincompat

package plugincompat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/airuntime"
	"lum-harbor-notifier/internal/applicationruntime"
	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/aiadapter"
	"lum-harbor-notifier/internal/plugininstall"
)

const (
	aiRuntimeExtensionID         = "lh-plugin-ai-runtime"
	aiRuntimeArtifactEnvironment = "LUM_HARBOR_AI_RUNTIME_PLUGIN_COMPAT_ARTIFACT"
)

func findAIRuntimeArtifact(t *testing.T) string {
	t.Helper()
	if configured := strings.TrimSpace(os.Getenv(aiRuntimeArtifactEnvironment)); configured != "" {
		absolute, err := filepath.Abs(configured)
		if err != nil {
			t.Fatal(err)
		}
		return absolute
	}
	for _, candidate := range []string{filepath.Join("..", "..", "lh-plugin-ai-runtime", "dist", "lh-plugin-ai-runtime.lhplugin"), filepath.Join("..", "..", "..", "lh-plugin-ai-runtime", "dist", "lh-plugin-ai-runtime.lhplugin")} {
		if _, err := os.Stat(candidate); err == nil {
			absolute, _ := filepath.Abs(candidate)
			return absolute
		}
	}
	t.Skipf("AI runtime plugin artifact not found; set %s", aiRuntimeArtifactEnvironment)
	return ""
}

type compatCapabilityHost struct{ calls []airuntime.CapabilityCall }

func (h *compatCapabilityHost) ListCapabilities() []airuntime.Capability {
	return []airuntime.Capability{
		{ID: "workitem.list", Kind: airuntime.CapabilityRead, Description: "List current WorkItems", InputSemantics: `{"limit":integer}`, ResultSemantics: "items", Origin: "CORE"},
		{ID: "planner.item.create", Kind: airuntime.CapabilityPropose, Description: "Prepare a Todo Proposal", InputSemantics: `{"title":string}`, ResultSemantics: "pending Proposal", Origin: "CORE"},
	}
}
func (h *compatCapabilityHost) ExecuteReads(_ context.Context, calls []airuntime.CapabilityCall) ([]airuntime.CapabilityResult, error) {
	h.calls = append(h.calls, calls...)
	results := make([]airuntime.CapabilityResult, len(calls))
	for index, call := range calls {
		results[index] = airuntime.CapabilityResult{Index: index, Call: call, Output: json.RawMessage(`{"Items":[{"ID":"work-1","Title":"Harbor"}],"Total":1}`)}
	}
	return results, nil
}

type compatMockCredentialStore struct{}

func (compatMockCredentialStore) Set(context.Context, string) error    { return nil }
func (compatMockCredentialStore) Exists(context.Context) (bool, error) { return false, nil }
func (compatMockCredentialStore) Delete(context.Context) error         { return nil }
func (compatMockCredentialStore) Resolve(context.Context) (string, bool, error) {
	return "", false, nil
}

func TestActualAIRuntimePluginStartCapabilityResumeFinalAndComposition(t *testing.T) {
	artifactPath := findAIRuntimeArtifact(t)
	pluginRoot := t.TempDir()
	installer, _ := plugininstall.New(pluginRoot)
	installed, err := installer.Install(artifactPath)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if installed.ExtensionID != aiRuntimeExtensionID || !installed.RestartRequired {
		t.Fatalf("installed=%+v", installed)
	}
	registry, failures, err := plugin.Discover(pluginRoot)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover err=%v failures=%+v", err, failures)
	}
	extension, _ := registry.Find(aiRuntimeExtensionID)
	if len(extension.Manifest.Bindings) != 1 || extension.Manifest.Bindings[0].Type != plugin.BindingAIRuntime || extension.Manifest.Bindings[0].Capability != plugin.CapabilityAIRuntime {
		t.Fatalf("bindings=%+v", extension.Manifest.Bindings)
	}
	var logs bytes.Buffer
	host, err := plugin.NewHost(registry, plugin.HostOptions{StartupTimeout: 3 * time.Second, RequestTimeout: 3 * time.Second, ShutdownTimeout: 2 * time.Second, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := host.Start(ctx, aiRuntimeExtensionID); err != nil {
		t.Fatal(err)
	}
	instance := plugin.Instance{ID: "runtime", ExtensionID: aiRuntimeExtensionID, CapabilityID: plugin.CapabilityAIRuntime, Config: json.RawMessage(`{"mock_mode":true}`)}
	capHost := &compatCapabilityHost{}
	runtime, err := aiadapter.NewRuntime(host, aiRuntimeExtensionID, &instance, capHost)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := runtime.Execute(ctx, airuntime.Request{RequestID: "ask-1", Purpose: ai.ActionAssistantAsk, Instructions: "Answer", Input: "What work exists?", OutputSchema: json.RawMessage(`{"type":"object"}`), Limits: airuntime.DefaultLimits()})
	if err != nil {
		t.Fatalf("execute answer: %v", err)
	}
	if len(capHost.calls) != 1 || capHost.calls[0].ID != "workitem.list" || !bytes.Contains(answer.Output, []byte("ANSWER")) || !bytes.Contains(answer.Output, []byte("work-1")) {
		t.Fatalf("calls=%+v answer=%s", capHost.calls, answer.Output)
	}
	proposal, err := runtime.Execute(ctx, airuntime.Request{RequestID: "ask-2", Purpose: ai.ActionAssistantAsk, Instructions: "Propose", Input: "Create a todo", OutputSchema: json.RawMessage(`{"type":"object"}`), Limits: airuntime.DefaultLimits()})
	if err != nil || !bytes.Contains(proposal.Output, []byte("planner.item.create")) {
		t.Fatalf("proposal=%s err=%v", proposal.Output, err)
	}
	cleaned, err := runtime.Execute(ctx, airuntime.Request{RequestID: "source-clean-1", Purpose: ai.ActionSourceClean, Instructions: "Normalize", Input: "raw source", OutputSchema: json.RawMessage(`{"type":"object"}`), Limits: airuntime.DefaultLimits()})
	if err != nil || !bytes.Contains(cleaned.Output, []byte("source.clean.v1")) {
		t.Fatalf("cleaned=%s err=%v", cleaned.Output, err)
	}
	interpreted, err := runtime.Execute(ctx, airuntime.Request{RequestID: "source-interpret-1", Purpose: ai.ActionSourceInterpret, Instructions: "Interpret", Input: "normalized source", OutputSchema: json.RawMessage(`{"type":"object"}`), Limits: airuntime.DefaultLimits()})
	if err != nil || !bytes.Contains(interpreted.Output, []byte("source.interpret.v1")) || !bytes.Contains(interpreted.Output, []byte("planner.item.create")) {
		t.Fatalf("interpreted=%s err=%v", interpreted.Output, err)
	}
	composed, err := applicationruntime.ComposeAI(ctx, registry, []plugin.Instance{instance}, host, host, config.DefaultConfig().AI.BuiltInOpenAI, compatMockCredentialStore{})
	if err != nil {
		t.Fatal(err)
	}
	projection := composed.Projection()
	if projection.Mode != ai.RuntimeModePluginRuntime || projection.Status != applicationruntime.AIRuntimeReady || projection.ManagingExtensionID != aiRuntimeExtensionID {
		t.Fatalf("projection=%+v", projection)
	}
	if err := host.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "started") || !strings.Contains(logs.String(), "shutdown accepted") {
		t.Fatalf("logs=%s", logs.String())
	}
}
