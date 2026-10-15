package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/plugintest"
	"lum-harbor-notifier/internal/plugin/readadapter"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// TestRuntimeAdapter_RealPluginSubprocessAcceptance verifies end-to-end execution
// of a real plugin subprocess read capability through the RuntimeAdapter and App.AskAssistant.
func TestRuntimeAdapter_RealPluginSubprocessAcceptance(t *testing.T) {
	root := t.TempDir()
	extensionID := "acceptance-read-ext"
	instanceID := "acceptance-read-inst"

	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID:   extensionID,
		Name: "Acceptance Read Plugin",
		Mode: "normal",
		CapabilityDeclarations: []plugin.CapabilityDeclaration{{
			ID:              plugin.CapabilityReadQuery,
			Description:     "Plugin status query fixture.",
			InputSemantics:  `{"query": string}`,
			ResultSemantics: `{"summary": string, "facts": [{"name": string, "value": string}]}`,
		}},
		Schema: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
			{Key: "label", Label: "Label", Type: plugin.ConfigString},
		}},
	})

	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("Discover: err=%v failures=%v", err, failures)
	}

	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout:  5 * time.Second,
		RequestTimeout:  2 * time.Second,
		ShutdownTimeout: 500 * time.Millisecond,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	if err := host.Start(context.Background(), extensionID); err != nil {
		t.Fatalf("host.Start: %v", err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })

	instances := []plugin.Instance{{
		ID:           instanceID,
		ExtensionID:  extensionID,
		CapabilityID: plugin.CapabilityReadQuery,
		Config:       json.RawMessage(`{"label":"Acceptance"}`),
	}}
	source, err := readadapter.New(host, registry, instances)
	if err != nil {
		t.Fatalf("readadapter.New: %v", err)
	}

	app, service, db := newWorkItemApp(t)
	desc := "WorkItem for plugin test"
	item, err := service.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{
		Title:       "Plugin Target",
		Description: &desc,
	})
	if err != nil {
		t.Fatal(err)
	}

	trackingService := tracking.NewService(db, service)
	plannerService := planner.NewService(db, planner.NewRepository(db))
	readcapService, err := readcap.NewService(service, trackingService, plannerService, db, source, nil, nil)
	if err != nil {
		t.Fatalf("readcap.NewService: %v", err)
	}

	// Verify plugin capability is listed in unified descriptors
	descriptors := readcapService.ListDescriptors()
	var pluginCapID string
	for _, d := range descriptors {
		if d.Origin == readcap.OriginPlugin {
			pluginCapID = d.ID
			break
		}
	}
	if pluginCapID == "" {
		t.Fatalf("expected plugin descriptor in readcap service, got: %+v", descriptors)
	}

	// Scripted AI Executor: Prompt 1 requests the plugin read; Prompt 1 (turn 2) returns NO_READ; Prompt 2 answers using read result
	executor := &scriptedAIExecutor{
		actionOutputs: map[ai.Action][]string{
			ai.ActionRuntimeCapabilityDecision: {
				`{"version":1,"decision":"CALL","calls":[{"capability_id":"` + pluginCapID + `","arguments_json":"{\"query\":\"status\"}"}]}`,
				`{"version":1,"decision":"FINAL","calls":[]}`,
			},
		},
		outputs: map[ai.Action]string{
			ai.ActionAssistantAsk: `{"version":1,"kind":"ANSWER","text":"Subprocess responded with valid query result."}`,
		},
	}

	engine, err := assistant.NewNativeRuntimeAdapter(executor, readcapService, NewProposeRegistry())
	if err != nil {
		t.Fatalf("NewNativeRuntimeAdapter: %v", err)
	}
	app.assistantRuntime = engine

	result, err := app.AskAssistant(AskAssistantRequestDTO{
		Message:    "Check status via plugin",
		Surface:    "WORK_ITEMS",
		WorkItemID: item.ID,
	})
	if err != nil {
		t.Fatalf("AskAssistant failed: %v", err)
	}

	if result.Kind != "ANSWER" || result.Text != "Subprocess responded with valid query result." {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(executor.actions) != 3 || executor.actions[0] != ai.ActionRuntimeCapabilityDecision || executor.actions[1] != ai.ActionRuntimeCapabilityDecision || executor.actions[2] != ai.ActionAssistantAsk {
		t.Fatalf("expected read decisions then ask, got actions: %v", executor.actions)
	}
}
