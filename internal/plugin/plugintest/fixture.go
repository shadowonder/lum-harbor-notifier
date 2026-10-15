// Package plugintest provides subprocess fixtures used only by plugin runtime tests.
package plugintest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"lum-harbor-notifier/internal/plugin"
)

const FixtureEnvironment = "LUM_HARBOR_PLUGIN_FIXTURE_MODE"

type ExtensionSpec struct {
	ID      string
	Name    string
	Version string
	Mode    string
	// Capabilities declares bare capability IDs with no semantic metadata.
	// Ignored when CapabilityDeclarations is non-empty.
	Capabilities []plugin.CapabilityID
	// CapabilityDeclarations declares full capability entries, including
	// optional Description/InputSemantics/ResultSemantics metadata (M15
	// Slice 2). Takes precedence over Capabilities when non-empty.
	CapabilityDeclarations []plugin.CapabilityDeclaration
	Bindings               []plugin.BindingDeclaration
	Schema                 plugin.PluginConfigSchema
}

func WriteExtension(t testing.TB, root string, spec ExtensionSpec) string {
	t.Helper()
	if spec.ID == "" {
		spec.ID = "test-extension"
	}
	if spec.Name == "" {
		spec.Name = "Test Extension"
	}
	if spec.Version == "" {
		spec.Version = "1.0.0"
	}
	if spec.Mode == "" {
		spec.Mode = "normal"
	}
	if spec.Schema.Version == 0 {
		spec.Schema.Version = 1
	}
	directory := filepath.Join(root, spec.ID)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("test executable: %v", err)
	}
	script := filepath.Join(directory, "fixture.sh")
	contents := fmt.Sprintf("#!/bin/sh\n%s='%s' exec '%s' -test.run '^TestPluginFixtureProcess$'\n", FixtureEnvironment, shellQuote(spec.Mode), shellQuote(testBinary))
	if err := os.WriteFile(script, []byte(contents), 0o755); err != nil {
		t.Fatalf("write fixture script: %v", err)
	}
	manifest := plugin.Manifest{
		ManifestVersion: plugin.SupportedManifestVersion,
		ID:              spec.ID, Name: spec.Name, Version: spec.Version,
		ProtocolVersion: plugin.SupportedProtocolVersion,
		Executable:      "fixture.sh", Bindings: spec.Bindings, Configuration: spec.Schema,
	}
	if len(spec.CapabilityDeclarations) > 0 {
		manifest.Capabilities = append(manifest.Capabilities, spec.CapabilityDeclarations...)
	} else {
		for _, capability := range spec.Capabilities {
			manifest.Capabilities = append(manifest.Capabilities, plugin.CapabilityDeclaration{ID: capability})
		}
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, plugin.ManifestFileName), data, 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return directory
}

func shellQuote(value string) string {
	return strings.ReplaceAll(value, "'", "'\\''")
}

type requestEnvelope struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type responseEnvelope struct {
	ID     string           `json:"id"`
	Result any              `json:"result,omitempty"`
	Error  *plugin.RPCError `json:"error,omitempty"`
}

func RunIfRequested() bool {
	mode := os.Getenv(FixtureEnvironment)
	if mode == "" {
		return false
	}
	runFixture(mode)
	os.Exit(0)
	return true
}

func runFixture(mode string) {
	scanner := bufio.NewScanner(os.Stdin)
	var writeMu sync.Mutex
	write := func(value any) {
		writeMu.Lock()
		defer writeMu.Unlock()
		data, _ := json.Marshal(value)
		_, _ = os.Stdout.Write(append(data, '\n'))
	}
	var workers sync.WaitGroup
	for scanner.Scan() {
		var request requestEnvelope
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			return
		}
		switch request.Method {
		case plugin.MethodHandshake:
			var handshake plugin.HandshakeRequest
			_ = json.Unmarshal(request.Params, &handshake)
			result := plugin.HandshakeResult{
				ProtocolVersion: handshake.ProtocolVersion, ExtensionID: handshake.ExtensionID,
				ExtensionVersion: handshake.ExtensionVersion, Capabilities: handshake.Capabilities,
			}
			switch mode {
			case "handshake_id":
				result.ExtensionID = "another-extension"
			case "handshake_version":
				result.ExtensionVersion = "9.9.9"
			case "handshake_protocol":
				result.ProtocolVersion++
			case "handshake_capabilities":
				result.Capabilities = append(result.Capabilities, plugin.CapabilityID("extra.magic"))
			}
			write(responseEnvelope{ID: request.ID, Result: result})
		case plugin.MethodShutdown:
			if mode == "hang_shutdown" {
				select {}
			}
			write(responseEnvelope{ID: request.ID, Result: map[string]bool{"accepted": true}})
			workers.Wait()
			return
		case plugin.MethodCapabilityInvoke:
			workers.Add(1)
			go func(request requestEnvelope) {
				defer workers.Done()
				handleInvocation(mode, request, write)
			}(request)
		}
	}
	workers.Wait()
}

func handleInvocation(mode string, request requestEnvelope, write func(any)) {
	if mode == "exit" {
		os.Exit(7)
	}
	if mode == "malformed" {
		_, _ = os.Stdout.WriteString("not-json\n")
		return
	}
	var params plugin.CapabilityInvokeParams
	_ = json.Unmarshal(request.Params, &params)
	var delayCarrier struct {
		DelayMillis int `json:"delayMillis"`
	}
	_ = json.Unmarshal(params.Input, &delayCarrier)
	var config map[string]any
	_ = json.Unmarshal(params.Config, &config)
	if delayCarrier.DelayMillis == 0 {
		if configured, ok := config["delayMillis"].(float64); ok {
			delayCarrier.DelayMillis = int(configured)
		}
	}
	if delayCarrier.DelayMillis > 0 {
		time.Sleep(time.Duration(delayCarrier.DelayMillis) * time.Millisecond)
	}
	responseID := request.ID
	if mode == "unknown_id" {
		responseID += "-unknown"
	}
	if mode == "both" {
		write(map[string]any{"id": responseID, "result": map[string]bool{"ok": true}, "error": map[string]string{"code": "bad", "message": "both"}})
		return
	}
	if config["remoteError"] == true {
		recordInvocation(config, fmt.Sprintf("%s|error", params.CapabilityID))
		write(responseEnvelope{ID: responseID, Error: &plugin.RPCError{Code: "fixture_error", Message: "requested failure"}})
		return
	}
	result := capabilityResult(params, config)
	response := responseEnvelope{ID: responseID, Result: result}
	write(response)
	if mode == "duplicate" {
		write(response)
	}
}

func capabilityResult(params plugin.CapabilityInvokeParams, config map[string]any) any {
	switch params.CapabilityID {
	case plugin.CapabilityAIPreRequest:
		var value struct {
			RequestID    string            `json:"requestId"`
			Action       string            `json:"action"`
			Messages     []map[string]any  `json:"messages"`
			OutputSchema json.RawMessage   `json:"outputSchema"`
			Metadata     map[string]string `json:"metadata"`
		}
		_ = json.Unmarshal(params.Input, &value)
		value.Messages = append([]map[string]any{{"role": "system", "content": "TEST PLUGIN CONTEXT"}}, value.Messages...)
		recordInvocation(config, fmt.Sprintf("pre|action=%s", value.Action))
		return map[string]any{"decision": "REPLACE", "request": value}
	case plugin.CapabilityAIProvider:
		var value struct {
			RequestID    string           `json:"requestId"`
			Action       string           `json:"action"`
			Messages     []map[string]any `json:"messages"`
			OutputSchema json.RawMessage  `json:"outputSchema"`
		}
		_ = json.Unmarshal(params.Input, &value)
		contextSeen := false
		for _, message := range value.Messages {
			if message["content"] == "TEST PLUGIN CONTEXT" {
				contextSeen = true
			}
		}
		output := any(map[string]any{"value": "plugin", "debugField": "remove-me", "contextSeen": contextSeen})
		if value.Action == "runtime.verify" {
			output = map[string]any{"ok": true, "debugField": "remove-me", "contextSeen": contextSeen, "providerVisited": true}
		}
		if strings.Contains(string(value.OutputSchema), "SourceCleanOutputV1") {
			output = map[string]any{"version": "source.clean.v1", "normalizedDocument": map[string]any{"fixture": "clean"}}
		}
		if strings.Contains(string(value.OutputSchema), "SourceInterpretOutputV1") {
			output = map[string]any{"version": "source.interpret.v1", "summary": "plugin interpretation", "interpretationDocument": map[string]any{"fixture": "interpret"}}
		}
		recordInvocation(config, fmt.Sprintf("provider|action=%s|contextSeen=%t", value.Action, contextSeen))
		return map[string]any{
			"requestId": value.RequestID, "action": value.Action,
			"output": output,
		}
	case plugin.CapabilityAIPostResponse:
		var value struct {
			RequestID string            `json:"requestId"`
			Action    string            `json:"action"`
			Output    map[string]any    `json:"output"`
			Metadata  map[string]string `json:"metadata"`
		}
		_ = json.Unmarshal(params.Input, &value)
		contextSeen, _ := value.Output["contextSeen"].(bool)
		providerVisited, _ := value.Output["providerVisited"].(bool)
		recordInvocation(config, fmt.Sprintf("post|action=%s|contextSeen=%t|providerVisited=%t", value.Action, contextSeen, providerVisited))
		delete(value.Output, "debugField")
		if value.Action == "runtime.verify" {
			delete(value.Output, "contextSeen")
			delete(value.Output, "providerVisited")
		}
		return map[string]any{"decision": "REPLACE", "response": value}
	case plugin.CapabilitySourceFetch:
		var config map[string]any
		_ = json.Unmarshal(params.Config, &config)
		team, _ := config["team"].(string)
		if team == "" {
			team = "default"
		}
		var request plugin.SourceFetchRequest
		_ = json.Unmarshal(params.Input, &request)
		payloadVersion := "X"
		observedAt := "2026-09-17T12:00:00Z"
		sourceUpdatedAt := "2026-09-17T11:59:00Z"
		cursor := fmt.Sprintf("%s-cursor-1", team)
		if request.Cursor != nil {
			cursor = fmt.Sprintf("%s-cursor-2", team)
			observedAt = "2026-09-17T12:01:00Z"
		}
		if request.Cursor != nil && *request.Cursor == fmt.Sprintf("%s-cursor-2", team) {
			payloadVersion = "Y"
			cursor = fmt.Sprintf("%s-cursor-3", team)
			observedAt = "2026-09-17T12:02:00Z"
			sourceUpdatedAt = "2026-09-17T12:02:00Z"
		}
		return map[string]any{
			"records": []any{map[string]any{
				"externalId": team + "-item", "sourceKind": "dummy.todo",
				"observedAt": observedAt, "sourceUpdatedAt": sourceUpdatedAt,
				"payload": map[string]any{"team": team, "instanceId": params.InstanceID, "version": payloadVersion},
			}},
			"nextCursor": cursor,
		}
	case plugin.CapabilityReadQuery:
		var config map[string]any
		_ = json.Unmarshal(params.Config, &config)
		var request plugin.ReadQueryRequest
		_ = json.Unmarshal(params.Input, &request)
		label, _ := config["label"].(string)
		switch behavior, _ := config["readQueryBehavior"].(string); behavior {
		case "malformed":
			return "not-an-object"
		case "oversized_summary":
			return map[string]any{"summary": strings.Repeat("x", plugin.MaxReadSummaryRunes+1), "facts": []any{}}
		case "too_many_facts":
			facts := make([]any, plugin.MaxReadFactCount+1)
			for i := range facts {
				facts[i] = map[string]any{"name": fmt.Sprintf("fact-%d", i), "value": "v"}
			}
			return map[string]any{"summary": "too many facts", "facts": facts}
		case "invalid_fact_name":
			return map[string]any{"summary": "invalid fact", "facts": []any{map[string]any{"name": "", "value": "v"}}}
		default:
			summary := fmt.Sprintf("query=%s instance=%s", request.Query, params.InstanceID)
			if label != "" {
				summary = label + ": " + summary
			}
			return map[string]any{
				"summary": summary,
				"facts": []any{
					map[string]any{"name": "instanceId", "value": params.InstanceID},
					map[string]any{"name": "query", "value": request.Query},
				},
			}
		}
	default:
		return map[string]any{"echo": json.RawMessage(params.Input)}
	}
}

func recordInvocation(config map[string]any, entry string) {
	path, _ := config["tracePath"].(string)
	if strings.TrimSpace(path) == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.WriteString(entry + "\n")
	_ = file.Close()
}
