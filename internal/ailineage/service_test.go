package ailineage

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
	dbgen "lum-harbor-notifier/internal/database/generated"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/testsupport"
)

func TestRunLifecycleIsSingleTransitionAndSourceLinked(t *testing.T) {
	database := testsupport.OpenTestDatabase(t)
	revisionID := createSourceRevision(t, database.DB)
	service := NewService(database.DB)
	startedAt := time.Date(2026, 9, 17, 20, 0, 0, 0, time.UTC)
	start := StartRun{
		ID: "run-1", ProcessingID: "processing-1", RequestID: "request-1",
		Action: ai.ActionSourceClean, CanonicalRequest: json.RawMessage(`{"action":"source.clean"}`),
		SourceRevisionID: revisionID, StartedAt: startedAt,
	}
	if err := service.Start(context.Background(), start); err != nil {
		t.Fatalf("Start: %v", err)
	}
	provenance := ai.ResponseProvenance{
		ProviderInvoked: true,
		ActualProvider:  &ai.ProviderInfo{ID: "provider", Name: "Provider", Source: ai.ProviderSourceBuiltIn},
		Origin:          ai.ResponseOrigin{Kind: ai.ResponseOriginProvider, ID: "provider", Name: "Provider"},
	}
	if err := service.Succeed(context.Background(), start.ID, json.RawMessage(`{"ok":true}`), provenance, startedAt.Add(time.Second)); err != nil {
		t.Fatalf("Succeed: %v", err)
	}
	if err := service.Fail(context.Background(), start.ID, context.Canceled, nil, startedAt.Add(2*time.Second)); err == nil || !strings.Contains(err.Error(), "no longer RUNNING") {
		t.Fatalf("expected terminal transition rejection, got %v", err)
	}

	repository := NewRepository(database.DB)
	run, err := repository.GetRun(context.Background(), start.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != StatusSucceeded || run.Provenance == nil || !run.Provenance.ProviderInvoked || run.CompletedAt == nil {
		t.Fatalf("unexpected stored run: %+v", run)
	}
	if _, err := repository.queries.GetAIRunSource(context.Background(), dbgen.GetAIRunSourceParams{
		AiRunID: start.ID, SourceRevisionID: revisionID, Role: SourceRolePrimary,
	}); err != nil {
		t.Fatalf("source link: %v", err)
	}
}

func TestHistoryFailureRollsBackOnlyTerminalDatabaseFinalization(t *testing.T) {
	database := testsupport.OpenTestDatabase(t)
	revisionID := createSourceRevision(t, database.DB)
	service := NewService(database.DB)
	startedAt := time.Now().UTC()
	start := StartRun{ID: "run-history-failure", ProcessingID: "processing-history-failure", RequestID: "request-history-failure", Action: ai.ActionSourceClean, CanonicalRequest: json.RawMessage(`{"action":"source.clean"}`), SourceRevisionID: revisionID, StartedAt: startedAt}
	if err := service.Start(context.Background(), start); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.Exec(`CREATE TRIGGER fail_ai_history BEFORE INSERT ON history_events WHEN NEW.event_type = 'AI_RUN_SUCCEEDED' BEGIN SELECT RAISE(ABORT, 'forced AI history failure'); END`); err != nil {
		t.Fatal(err)
	}
	provenance := ai.ResponseProvenance{ProviderInvoked: true, ActualProvider: &ai.ProviderInfo{ID: "provider", Name: "Provider", Source: ai.ProviderSourceBuiltIn}, Origin: ai.ResponseOrigin{Kind: ai.ResponseOriginProvider, ID: "provider", Name: "Provider"}}
	if err := service.Succeed(context.Background(), start.ID, json.RawMessage(`{"ok":true}`), provenance, startedAt.Add(time.Second)); err == nil {
		t.Fatal("expected injected History failure")
	}
	run, err := NewRepository(database.DB).GetRun(context.Background(), start.ID)
	if err != nil || run.Status != StatusRunning || run.CompletedAt != nil {
		t.Fatalf("terminal database finalization escaped rollback: %+v err=%v", run, err)
	}
	events, err := history.NewRepository(database.DB).ListByCorrelation(context.Background(), history.ProcessingCorrelation(start.ProcessingID), 10)
	if err != nil || len(events) != 0 {
		t.Fatalf("orphan terminal History: %+v err=%v", events, err)
	}
}

func createSourceRevision(t *testing.T, db *sql.DB) string {
	t.Helper()
	// Kept through Source Service so this test exercises the real foreign-key target.
	service := source.NewService(db)
	checkpoint, err := service.PrepareFetch(context.Background(), "extension", "instance", "source.fetch")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.IngestFetchResult(context.Background(), source.FetchBatch{
		ExtensionID: "extension", InstanceID: "instance", CapabilityID: "source.fetch", Checkpoint: checkpoint,
		Records: []source.Observation{{ExternalID: "item", SourceKind: "test.item", ObservedAt: time.Now().UTC(), Payload: json.RawMessage(`{"value":1}`)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result.NewlyChangedRevisionIDs[0]
}

func TestRunWithoutSourceRevisionSucceedsCleanly(t *testing.T) {
	database := testsupport.OpenTestDatabase(t)
	service := NewService(database.DB)
	startedAt := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	start := StartRun{
		ID: "assistant-run-1", ProcessingID: "assistant-proc-1", RequestID: "assistant-req-1",
		Action: ai.ActionAssistantAsk, CanonicalRequest: json.RawMessage(`{"action":"assistant.ask"}`),
		SourceRevisionID: "", // No source revision for direct Assistant intent
		StartedAt:        startedAt,
	}
	if err := service.Start(context.Background(), start); err != nil {
		t.Fatalf("Start without source revision: %v", err)
	}
	provenance := ai.ResponseProvenance{
		ProviderInvoked: true,
		ActualProvider:  &ai.ProviderInfo{ID: "builtin-openai", Name: "OpenAI Compatible", Source: ai.ProviderSourceBuiltIn},
		Origin:          ai.ResponseOrigin{Kind: ai.ResponseOriginProvider, ID: "builtin-openai", Name: "OpenAI Compatible"},
	}
	if err := service.Succeed(context.Background(), start.ID, json.RawMessage(`{"kind":"MUTATION_INTENT"}`), provenance, startedAt.Add(time.Second)); err != nil {
		t.Fatalf("Succeed without source revision: %v", err)
	}

	repository := NewRepository(database.DB)
	run, err := repository.GetRun(context.Background(), start.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != StatusSucceeded {
		t.Fatalf("expected SUCCEEDED, got %s", run.Status)
	}

	// Verify no source link was created
	sourceRevID, err := repository.GetPrimarySourceRevisionID(context.Background(), start.ID)
	if err != nil {
		t.Fatalf("GetPrimarySourceRevisionID error: %v", err)
	}
	if sourceRevID != "" {
		t.Fatalf("expected empty source revision, got %q", sourceRevID)
	}

	// Verify terminal history was recorded with nil source revision ID
	events, err := history.NewRepository(database.DB).ListByCorrelation(context.Background(), history.ProcessingCorrelation(start.ProcessingID), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 history event, got %d", len(events))
	}
	if events[0].SourceRevisionID != nil {
		t.Fatalf("expected nil SourceRevisionID on history event, got %v", *events[0].SourceRevisionID)
	}
}
