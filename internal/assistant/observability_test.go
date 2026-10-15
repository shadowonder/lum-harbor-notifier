package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"

	"lum-harbor-notifier/internal/ai"
)

func TestAssistantExecutionStoryGroupsOrderedStepsAndDebugDetail(t *testing.T) {
	var logs bytes.Buffer
	restore := installTestLogger(&logs, slog.LevelDebug)
	defer restore()

	story := newAssistantExecutionStory(context.Background(), "request-1",
		"Resolve the issue, api_key=topsecret, then prepare a proposal. "+strings.Repeat("context ", 100))
	story.addState("ITERATING")
	story.addStep("read_decision -> READ entity.resolve_external(F123); result -> NOT_FOUND")
	story.addStep("read_decision -> READ workitem.read(W456); result -> 3 facts")
	story.addState("SYNTHESIS")
	story.recordCall(ai.Request{
		Action: ai.ActionAssistantAsk,
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: "Synthesize a result."},
			{Role: ai.RoleUser, Content: "Authorization: Bearer debug-secret, return JSON"},
		},
	}, ai.Response{
		Output: json.RawMessage(`{"access_token":"response-secret","payload":"` + strings.Repeat("detail ", 400) + `"}`),
		Provenance: ai.ResponseProvenance{
			ProviderInvoked: true,
			ActualProvider:  &ai.ProviderInfo{ID: "provider.openai"},
		},
		Usage: &ai.Usage{InputTokens: 20, OutputTokens: 5, TotalTokens: 25},
	}, nil)
	story.addStep("synthesis -> MUTATION_CANDIDATE tracking.workflow_create")
	story.finish(IterationResult{
		Kind:      KindMutationCandidate,
		Candidate: &MutationCandidate{CapabilityID: "tracking.workflow_create"},
	}, nil)

	records := decodeLogRecords(t, logs.String())
	if len(records) != 2 {
		t.Fatalf("expected one execution record and one detail record, got %d: %s", len(records), logs.String())
	}
	info, debug := records[0], records[1]
	if info["level"] != "INFO" || info["msg"] != "AI execution" {
		t.Fatalf("unexpected info record: %#v", info)
	}
	steps := info["steps"].(string)
	first := strings.Index(steps, "1. read_decision")
	second := strings.Index(steps, "2. read_decision")
	third := strings.Index(steps, "3. synthesis")
	if first < 0 || second <= first || third <= second {
		t.Fatalf("steps are not readable and ordered: %q", steps)
	}
	if got := info["state"]; got != "STARTED -> ITERATING -> SYNTHESIS -> PROPOSAL_SELECTED -> SUCCEEDED" {
		t.Fatalf("unexpected state progression: %q", got)
	}
	if info["provider"] != "provider.openai" || info["destination"] != "proposal_dispatch" {
		t.Fatalf("missing provider/destination: %#v", info)
	}
	if strings.Contains(logs.String(), "topsecret") || strings.Contains(logs.String(), "debug-secret") || strings.Contains(logs.String(), "response-secret") {
		t.Fatalf("secret leaked into logs: %s", logs.String())
	}
	if !strings.Contains(logs.String(), "[REDACTED]") {
		t.Fatalf("expected redaction marker: %s", logs.String())
	}
	if debug["level"] != "DEBUG" || debug["msg"] != "AI execution detail" {
		t.Fatalf("unexpected debug record: %#v", debug)
	}
	calls := debug["calls"].(string)
	if !strings.Contains(calls, "assistant.ask request=") || !strings.Contains(calls, "response=") || !strings.Contains(calls, "usage=") {
		t.Fatalf("debug detail is not useful: %q", calls)
	}
	if !strings.Contains(calls, "…(+") {
		t.Fatalf("debug detail was not visibly bounded: %q", calls)
	}
	if len(strings.Split(strings.TrimSpace(logs.String()), "\n")[0]) > 3000 {
		t.Fatalf("INFO execution record is not bounded: %d bytes", len(strings.Split(strings.TrimSpace(logs.String()), "\n")[0]))
	}
}

func TestAssistantExecutionStoryFailureKeepsRedactedRootCause(t *testing.T) {
	var logs bytes.Buffer
	restore := installTestLogger(&logs, slog.LevelInfo)
	defer restore()

	story := newAssistantExecutionStory(context.Background(), "request-2", "Try the provider")
	story.addStep("read_decision -> FAILED")
	story.finish(IterationResult{}, errors.New("provider HTTP status 429; Authorization: Bearer failure-secret"))

	records := decodeLogRecords(t, logs.String())
	if len(records) != 1 {
		t.Fatalf("expected one cohesive failure record, got %d: %s", len(records), logs.String())
	}
	record := records[0]
	if record["level"] != "WARN" || !strings.Contains(record["result"].(string), "HTTP status 429") {
		t.Fatalf("useful root cause missing: %#v", record)
	}
	if record["state"] != "STARTED -> FAILED" {
		t.Fatalf("unexpected failure state: %q", record["state"])
	}
	if strings.Contains(logs.String(), "failure-secret") || !strings.Contains(logs.String(), "[REDACTED]") {
		t.Fatalf("failure was not safely redacted: %s", logs.String())
	}
}

func TestSafeLogPreviewIsRedactedAndRuneBounded(t *testing.T) {
	detail := safeLogPreview("password=never-log credential=also-secret eyJabc.def.ghi sk-abcdefghijklmnop -----BEGIN PRIVATE KEY-----pem-secret-----END PRIVATE KEY----- "+strings.Repeat("界", maxInfoResultRunes+100), maxInfoResultRunes)
	for _, secret := range []string{"never-log", "also-secret", "eyJabc.def.ghi", "sk-abcdefghijklmnop", "pem-secret"} {
		if strings.Contains(detail, secret) {
			t.Fatalf("secret %q was not redacted: %q", secret, detail)
		}
	}
	if !strings.Contains(detail, "[REDACTED]") {
		t.Fatalf("secret was not redacted: %q", detail)
	}
	if utf8.RuneCountInString(detail) > maxInfoResultRunes+20 {
		t.Fatalf("detail was not bounded: %d runes", utf8.RuneCountInString(detail))
	}
	if !strings.Contains(detail, "…(+") {
		t.Fatalf("truncation was not made visible: %q", detail)
	}
}

func installTestLogger(buffer *bytes.Buffer, level slog.Level) func() {
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buffer, &slog.HandlerOptions{Level: level})))
	return func() { slog.SetDefault(previous) }
}

func decodeLogRecords(t *testing.T, output string) []map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	records := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log record %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}
