package sourceprocessing

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/ailineage"
	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/aiadapter"
	"lum-harbor-notifier/internal/plugin/plugintest"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/testsupport"
)

const (
	validClean     = `{"version":"source.clean.v1","normalizedDocument":{"title":"normalized"}}`
	validInterpret = `{"version":"source.interpret.v1","summary":"important","interpretationDocument":{"meaning":"follow up"}}`
)

type providerFunc func(context.Context, ai.Request) (ai.Response, error)

func (f providerFunc) Execute(ctx context.Context, request ai.Request) (ai.Response, error) {
	return f(ctx, request)
}

type preFunc func(context.Context, ai.Request) (ai.PreRequestResult, error)

func (f preFunc) Process(ctx context.Context, request ai.Request) (ai.PreRequestResult, error) {
	return f(ctx, request)
}

func TestPluginFixtureProcess(t *testing.T) {
	if plugintest.RunIfRequested() {
		return
	}
}

func TestExactOlderRevisionSuccessAndDurableLineage(t *testing.T) {
	db := testsupport.OpenTestDatabase(t).DB
	sourceService := source.NewService(db)
	first := ingest(t, sourceService, "item-1", `{"title":"Revision A"}`)
	second := ingest(t, sourceService, "item-1", `{"title":"Revision B"}`)
	oldRevisionID := first.NewlyChangedRevisionIDs[0]
	newRevisionID := second.NewlyChangedRevisionIDs[0]

	var requestsMu sync.Mutex
	var requests []ai.Request
	provider := providerFunc(func(_ context.Context, request ai.Request) (ai.Response, error) {
		requestsMu.Lock()
		requests = append(requests, request)
		requestsMu.Unlock()
		output := validClean
		if request.Action == ai.ActionSourceInterpret {
			output = validInterpret
		}
		return ai.Response{RequestID: request.RequestID, Action: request.Action, Output: json.RawMessage(output)}, nil
	})
	processor := newProcessor(t, db, provider, nil)
	result, err := processor.ProcessRevision(context.Background(), ProcessRequest{
		SourceRevisionID:      oldRevisionID,
		InterpretationContext: json.RawMessage(` { "team" : "harbor", "priority" : 2 } `),
	})
	if err != nil {
		t.Fatalf("ProcessRevision: %v", err)
	}
	if result.SourceRevisionID != oldRevisionID || result.ProcessingID == "" || result.CleanRunID == result.InterpretRunID || result.InterpretResult.Summary != "important" {
		t.Fatalf("unexpected result: %+v", result)
	}
	requestsMu.Lock()
	defer requestsMu.Unlock()
	if len(requests) != 2 || requests[0].RequestID == requests[1].RequestID {
		t.Fatalf("expected two distinct requests: %+v", requests)
	}
	if !strings.Contains(requests[0].Messages[1].Content, "Revision A") || strings.Contains(requests[0].Messages[1].Content, "Revision B") {
		t.Fatalf("clean did not consume exact old revision: %s", requests[0].Messages[1].Content)
	}

	repository := ailineage.NewRepository(db)
	runs, err := repository.ListRunsForProcessing(context.Background(), result.ProcessingID)
	if err != nil || len(runs) != 2 {
		t.Fatalf("processing runs: %+v err=%v", runs, err)
	}
	cleanRun := runByAction(t, runs, ai.ActionSourceClean)
	interpretRun := runByAction(t, runs, ai.ActionSourceInterpret)
	if cleanRun.Status != ailineage.StatusSucceeded || interpretRun.Status != ailineage.StatusSucceeded || interpretRun.UpstreamRunID == nil || *interpretRun.UpstreamRunID != cleanRun.ID {
		t.Fatalf("incorrect run lineage: clean=%+v interpret=%+v", cleanRun, interpretRun)
	}
	historyEvents, err := history.NewRepository(db).ListByCorrelation(context.Background(), history.ProcessingCorrelation(result.ProcessingID), 10)
	if err != nil || len(historyEvents) != 2 {
		t.Fatalf("processing History: %+v err=%v", historyEvents, err)
	}
	byRun := map[string]history.Event{}
	for _, event := range historyEvents {
		if event.AIRunID == nil || event.SourceRevisionID == nil || *event.SourceRevisionID != oldRevisionID || event.RevisionID != nil || event.ProposalID != nil {
			t.Fatalf("incorrect exact lineage links: %+v", event)
		}
		byRun[*event.AIRunID] = event
	}
	cleanEvent, interpretEvent := byRun[cleanRun.ID], byRun[interpretRun.ID]
	if cleanEvent.Type != history.EventAIRunSucceeded || cleanEvent.CausationEventID != nil || interpretEvent.Type != history.EventAIRunSucceeded || interpretEvent.CausationEventID == nil || *interpretEvent.CausationEventID != cleanEvent.ID {
		t.Fatalf("incorrect clean→interpret causation: clean=%+v interpret=%+v", cleanEvent, interpretEvent)
	}
	if cleanRun.Provenance == nil || !cleanRun.Provenance.ProviderInvoked || cleanRun.Provenance.ActualProvider == nil || cleanRun.Provenance.ActualProvider.ID != "test-provider" {
		t.Fatalf("actual provenance not persisted: %+v", cleanRun.Provenance)
	}
	for _, run := range runs {
		linked, err := repository.ListRunsForSourceRevision(context.Background(), oldRevisionID)
		if err != nil || !containsRun(linked, run.ID) {
			t.Fatalf("run %s lacks exact source link: %+v err=%v", run.ID, linked, err)
		}
	}
	if !strings.Contains(string(interpretRun.CanonicalRequest), `\"interpretationContext\":{\"priority\":2,\"team\":\"harbor\"}`) {
		t.Fatalf("canonical context snapshot not persisted: %s", interpretRun.CanonicalRequest)
	}
	record, err := source.NewRepository(db).GetRecord(context.Background(), mustRevision(t, db, newRevisionID).SourceRecordID)
	if err != nil || record.CurrentRevisionID != newRevisionID {
		t.Fatalf("processing changed source head: record=%+v err=%v", record, err)
	}
	assertCount(t, db, "source_revisions", 2)
	assertCount(t, db, "revisions", 0)
	assertCount(t, db, "planner_items", 0)
}

func TestSourceIngestionChangedRevisionFeedsProcessorDirectly(t *testing.T) {
	db := testsupport.OpenTestDatabase(t).DB
	changed := ingest(t, source.NewService(db), "handoff", `{"value":1}`)
	processor := newProcessor(t, db, successfulProvider(), nil)
	result, err := processor.ProcessRevision(context.Background(), ProcessRequest{SourceRevisionID: changed.NewlyChangedRevisionIDs[0]})
	if err != nil || result.SourceRevisionID != changed.NewlyChangedRevisionIDs[0] {
		t.Fatalf("direct changed revision handoff failed: result=%+v err=%v", result, err)
	}
}

func TestPluginBackedProviderPersistsPipelineLineage(t *testing.T) {
	db, revisionID := databaseWithRevision(t)
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "processing-provider", Name: "Processing Provider",
		Capabilities: []plugin.CapabilityID{plugin.CapabilityAIProvider},
		Bindings:     []plugin.BindingDeclaration{{ID: "provider", Type: plugin.BindingAIProvider, Capability: plugin.CapabilityAIProvider, Actions: []string{"*"}, Order: 1}},
	})
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("Discover: err=%v failures=%v", err, failures)
	}
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout: 3 * time.Second, RequestTimeout: time.Second, ShutdownTimeout: time.Second,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(context.Background(), "processing-provider"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	extension, _ := registry.Find("processing-provider")
	replacement, err := aiadapter.ProviderRegistration(extension, host, nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := ai.NewService(ai.Config{
		BuiltInProvider: ai.ProviderRegistration{ID: "unused", Name: "Unused", Provider: providerFunc(func(context.Context, ai.Request) (ai.Response, error) {
			return ai.Response{}, errors.New("built-in provider must not run")
		})},
		ReplacementProvider: &replacement,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewProcessor(db, service).ProcessRevision(context.Background(), ProcessRequest{SourceRevisionID: revisionID})
	if err != nil {
		t.Fatalf("plugin pipeline: %v", err)
	}
	if result.CleanResult.Version != CleanOutputVersionV1 || result.InterpretResult.Summary != "plugin interpretation" {
		t.Fatalf("unexpected plugin result: %+v", result)
	}
	for _, run := range runsForRevision(t, db, revisionID) {
		if run.Status != ailineage.StatusSucceeded || run.Provenance == nil || run.Provenance.ActualProvider == nil || run.Provenance.ActualProvider.ID != "processing-provider" || run.Provenance.ActualProvider.Source != ai.ProviderSourcePlugin {
			t.Fatalf("plugin provenance not persisted: %+v", run)
		}
	}
}

func TestCleanFailurePersistsFailedRunAndSkipsInterpret(t *testing.T) {
	db, revisionID := databaseWithRevision(t)
	var calls atomic.Int32
	cause := errors.New("provider unavailable")
	processor := newProcessor(t, db, providerFunc(func(_ context.Context, request ai.Request) (ai.Response, error) {
		calls.Add(1)
		return ai.Response{}, cause
	}), nil)
	_, err := processor.ProcessRevision(context.Background(), ProcessRequest{SourceRevisionID: revisionID})
	if !errors.Is(err, cause) || calls.Load() != 1 {
		t.Fatalf("clean failure: calls=%d err=%v", calls.Load(), err)
	}
	runs := runsForRevision(t, db, revisionID)
	if len(runs) != 1 || runs[0].Action != ai.ActionSourceClean || runs[0].Status != ailineage.StatusFailed || runs[0].Provenance != nil {
		t.Fatalf("unexpected clean failure lineage: %+v", runs)
	}
	events, historyErr := history.NewRepository(db).ListByCorrelation(context.Background(), history.ProcessingCorrelation(runs[0].ProcessingID), 10)
	if historyErr != nil || len(events) != 1 || events[0].Type != history.EventAIRunFailed || events[0].Actor != history.ActorSystem || events[0].CausationEventID != nil {
		t.Fatalf("clean failure History: %+v err=%v", events, historyErr)
	}
}

func TestRunningIsCommittedBeforeAICallWithoutHoldingTransaction(t *testing.T) {
	db, revisionID := databaseWithRevision(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	processor := newProcessor(t, db, providerFunc(func(ctx context.Context, request ai.Request) (ai.Response, error) {
		if request.Action == ai.ActionSourceClean {
			once.Do(func() { close(entered) })
			select {
			case <-release:
			case <-ctx.Done():
				return ai.Response{}, ctx.Err()
			}
			return response(request, validClean), nil
		}
		return response(request, validInterpret), nil
	}), nil)
	done := make(chan error, 1)
	go func() {
		_, err := processor.ProcessRevision(context.Background(), ProcessRequest{SourceRevisionID: revisionID})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("AI provider was not entered")
	}
	queryContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	runs, err := ailineage.NewRepository(db).ListRunsForSourceRevision(queryContext, revisionID)
	if err != nil || len(runs) != 1 || runs[0].Status != ailineage.StatusRunning {
		t.Fatalf("RUNNING was not durably queryable during AI call: runs=%+v err=%v", runs, err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestInterpretFailurePreservesCleanSuccess(t *testing.T) {
	db, revisionID := databaseWithRevision(t)
	cause := errors.New("interpret failed")
	processor := newProcessor(t, db, providerFunc(func(_ context.Context, request ai.Request) (ai.Response, error) {
		if request.Action == ai.ActionSourceInterpret {
			return ai.Response{}, cause
		}
		return response(request, validClean), nil
	}), nil)
	_, err := processor.ProcessRevision(context.Background(), ProcessRequest{SourceRevisionID: revisionID})
	if !errors.Is(err, cause) {
		t.Fatalf("expected interpret error, got %v", err)
	}
	runs := runsForRevision(t, db, revisionID)
	if len(runs) != 2 || runByAction(t, runs, ai.ActionSourceClean).Status != ailineage.StatusSucceeded || runByAction(t, runs, ai.ActionSourceInterpret).Status != ailineage.StatusFailed {
		t.Fatalf("unexpected interpret failure lineage: %+v", runs)
	}
	cleanRun, interpretRun := runByAction(t, runs, ai.ActionSourceClean), runByAction(t, runs, ai.ActionSourceInterpret)
	events, historyErr := history.NewRepository(db).ListByCorrelation(context.Background(), history.ProcessingCorrelation(cleanRun.ProcessingID), 10)
	if historyErr != nil || len(events) != 2 {
		t.Fatalf("interpret failure History: %+v err=%v", events, historyErr)
	}
	byRun := map[string]history.Event{}
	for _, event := range events {
		byRun[*event.AIRunID] = event
	}
	if byRun[interpretRun.ID].Type != history.EventAIRunFailed || byRun[interpretRun.ID].CausationEventID == nil || *byRun[interpretRun.ID].CausationEventID != byRun[cleanRun.ID].ID {
		t.Fatalf("interpret failure cause: %+v", events)
	}
	assertCount(t, db, "source_revisions", 1)
}

func TestInvalidTypedOutputsBecomeFailedRuns(t *testing.T) {
	tests := []struct {
		name      string
		clean     string
		interpret string
		wantRuns  int
		failed    ai.Action
	}{
		{name: "clean", clean: `{"version":"source.clean.v1"}`, interpret: validInterpret, wantRuns: 1, failed: ai.ActionSourceClean},
		{name: "interpret", clean: validClean, interpret: `{"version":"source.interpret.v1","summary":"missing document"}`, wantRuns: 2, failed: ai.ActionSourceInterpret},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, revisionID := databaseWithRevision(t)
			processor := newProcessor(t, db, providerFunc(func(_ context.Context, request ai.Request) (ai.Response, error) {
				if request.Action == ai.ActionSourceClean {
					return response(request, test.clean), nil
				}
				return response(request, test.interpret), nil
			}), nil)
			if _, err := processor.ProcessRevision(context.Background(), ProcessRequest{SourceRevisionID: revisionID}); err == nil {
				t.Fatal("expected typed output validation failure")
			}
			runs := runsForRevision(t, db, revisionID)
			if len(runs) != test.wantRuns || runByAction(t, runs, test.failed).Status != ailineage.StatusFailed || runByAction(t, runs, test.failed).Provenance == nil {
				t.Fatalf("unexpected invalid output lineage: %+v", runs)
			}
			failedRun := runByAction(t, runs, test.failed)
			events, err := history.NewRepository(db).ListByCorrelation(context.Background(), history.ProcessingCorrelation(failedRun.ProcessingID), 10)
			if err != nil || len(events) != test.wantRuns || events[len(events)-1].Type != history.EventAIRunFailed {
				t.Fatalf("typed validation failure History: %+v err=%v", events, err)
			}
		})
	}
}

func TestPreMiddlewareStopProvenanceIsPersisted(t *testing.T) {
	db, revisionID := databaseWithRevision(t)
	var providerCalls atomic.Int32
	pre := preFunc(func(_ context.Context, request ai.Request) (ai.PreRequestResult, error) {
		return ai.PreRequestResult{Decision: ai.DecisionStop, Response: response(request, validClean)}, nil
	})
	processor := newProcessor(t, db, providerFunc(func(_ context.Context, request ai.Request) (ai.Response, error) {
		providerCalls.Add(1)
		return response(request, validInterpret), nil
	}), func(config *ai.Config) {
		config.PreMiddleware = []ai.PreMiddlewareBinding{{ID: "clean-cache", Name: "Clean Cache", Actions: []ai.Action{ai.ActionSourceClean}, Middleware: pre}}
	})
	if _, err := processor.ProcessRevision(context.Background(), ProcessRequest{SourceRevisionID: revisionID}); err != nil {
		t.Fatal(err)
	}
	if providerCalls.Load() != 1 {
		t.Fatalf("provider calls: got %d want interpret only", providerCalls.Load())
	}
	clean := runByAction(t, runsForRevision(t, db, revisionID), ai.ActionSourceClean)
	if clean.Provenance == nil || clean.Provenance.ProviderInvoked || clean.Provenance.ActualProvider != nil || clean.Provenance.Origin.Kind != ai.ResponseOriginPreMiddleware || clean.Provenance.Origin.ID != "clean-cache" {
		t.Fatalf("incorrect STOP provenance: %+v", clean.Provenance)
	}
}

func TestReprocessingAndConcurrentProcessingCreateIndependentAttempts(t *testing.T) {
	db, revisionID := databaseWithRevision(t)
	processor := newProcessor(t, db, successfulProvider(), nil)
	first, err := processor.ProcessRevision(context.Background(), ProcessRequest{SourceRevisionID: revisionID})
	if err != nil {
		t.Fatal(err)
	}
	second, err := processor.ProcessRevision(context.Background(), ProcessRequest{SourceRevisionID: revisionID})
	if err != nil {
		t.Fatal(err)
	}
	if first.ProcessingID == second.ProcessingID || first.CleanRunID == second.CleanRunID {
		t.Fatal("explicit reprocessing reused attempt identity")
	}

	results := make(chan ProcessingResult, 2)
	errorsCh := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := processor.ProcessRevision(context.Background(), ProcessRequest{SourceRevisionID: revisionID, InterpretationContext: json.RawMessage(`{}`)})
			if err != nil {
				errorsCh <- err
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
	close(results)
	seen := map[string]struct{}{first.ProcessingID: {}, second.ProcessingID: {}}
	for result := range results {
		if _, duplicate := seen[result.ProcessingID]; duplicate {
			t.Errorf("duplicate processing ID %s", result.ProcessingID)
		}
		seen[result.ProcessingID] = struct{}{}
	}
	if len(seen) != 4 || len(runsForRevision(t, db, revisionID)) != 8 {
		t.Fatalf("independent attempts: processing=%d runs=%d", len(seen), len(runsForRevision(t, db, revisionID)))
	}
	for processingID := range seen {
		events, err := history.NewRepository(db).ListByCorrelation(context.Background(), history.ProcessingCorrelation(processingID), 10)
		if err != nil || len(events) != 2 || events[0].SourceRevisionID == nil || *events[0].SourceRevisionID != revisionID || events[1].CausationEventID == nil || *events[1].CausationEventID != events[0].ID {
			t.Fatalf("isolated processing trace %s: %+v err=%v", processingID, events, err)
		}
	}
}

func TestInterpretationContextMustBeOneJSONObject(t *testing.T) {
	db, revisionID := databaseWithRevision(t)
	processor := newProcessor(t, db, successfulProvider(), nil)
	for _, invalid := range []string{`[]`, `null`, `{broken`, `{} {}`} {
		if _, err := processor.ProcessRevision(context.Background(), ProcessRequest{SourceRevisionID: revisionID, InterpretationContext: json.RawMessage(invalid)}); err == nil {
			t.Fatalf("expected context %q to fail", invalid)
		}
	}
	if runs := runsForRevision(t, db, revisionID); len(runs) != 0 {
		t.Fatalf("invalid context created runs: %+v", runs)
	}
}

func TestSourceAndAIHistorySurviveDatabaseRestart(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	opened, err := database.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ingested := ingest(t, source.NewService(opened.DB), "restart-item", `{"value":1}`)
	revisionID := ingested.NewlyChangedRevisionIDs[0]
	result, err := newProcessor(t, opened.DB, successfulProvider(), nil).ProcessRevision(context.Background(), ProcessRequest{SourceRevisionID: revisionID})
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.DB.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.DB.Close()
	sourceEvents, err := history.NewRepository(reopened.DB).ListByCorrelation(context.Background(), history.SourceRevisionCorrelation(revisionID), 10)
	if err != nil || len(sourceEvents) != 1 || sourceEvents[0].SourceRevisionID == nil || *sourceEvents[0].SourceRevisionID != revisionID {
		t.Fatalf("Source History after restart: %+v err=%v", sourceEvents, err)
	}
	aiEvents, err := history.NewRepository(reopened.DB).ListByCorrelation(context.Background(), history.ProcessingCorrelation(result.ProcessingID), 10)
	if err != nil || len(aiEvents) != 2 || aiEvents[1].CausationEventID == nil || *aiEvents[1].CausationEventID != aiEvents[0].ID {
		t.Fatalf("AI History after restart: %+v err=%v", aiEvents, err)
	}
	runs, err := ailineage.NewRepository(reopened.DB).ListRunsForProcessing(context.Background(), result.ProcessingID)
	if err != nil || len(runs) != 2 || runs[0].Status != ailineage.StatusSucceeded || runs[1].Status != ailineage.StatusSucceeded {
		t.Fatalf("AI current lineage after restart: %+v err=%v", runs, err)
	}
	if _, err := source.NewRepository(reopened.DB).GetRevision(context.Background(), revisionID); err != nil {
		t.Fatalf("Source truth after restart: %v", err)
	}
}

func successfulProvider() ai.Provider {
	return providerFunc(func(_ context.Context, request ai.Request) (ai.Response, error) {
		if request.Action == ai.ActionSourceClean {
			return response(request, validClean), nil
		}
		return response(request, validInterpret), nil
	})
}

func newProcessor(t *testing.T, db *sql.DB, provider ai.Provider, configure func(*ai.Config)) *Processor {
	t.Helper()
	config := ai.Config{BuiltInProvider: ai.ProviderRegistration{ID: "test-provider", Name: "Test Provider", Provider: provider}}
	if configure != nil {
		configure(&config)
	}
	service, err := ai.NewService(config)
	if err != nil {
		t.Fatal(err)
	}
	return NewProcessor(db, service)
}

func databaseWithRevision(t *testing.T) (*sql.DB, string) {
	t.Helper()
	db := testsupport.OpenTestDatabase(t).DB
	result := ingest(t, source.NewService(db), "item", `{"value":1}`)
	return db, result.NewlyChangedRevisionIDs[0]
}

func ingest(t *testing.T, service *source.Service, externalID, payload string) source.IngestionResult {
	t.Helper()
	checkpoint, err := service.PrepareFetch(context.Background(), "test-extension", "test-instance", "source.fetch")
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.IngestFetchResult(context.Background(), source.FetchBatch{
		ExtensionID: "test-extension", InstanceID: "test-instance", CapabilityID: "source.fetch", Checkpoint: checkpoint,
		Records: []source.Observation{{ExternalID: externalID, SourceKind: "test.item", ObservedAt: time.Now().UTC(), Payload: json.RawMessage(payload)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func response(request ai.Request, output string) ai.Response {
	return ai.Response{RequestID: request.RequestID, Action: request.Action, Output: json.RawMessage(output)}
}

func runsForRevision(t *testing.T, db *sql.DB, revisionID string) []ailineage.Run {
	t.Helper()
	runs, err := ailineage.NewRepository(db).ListRunsForSourceRevision(context.Background(), revisionID)
	if err != nil {
		t.Fatal(err)
	}
	return runs
}

func runByAction(t *testing.T, runs []ailineage.Run, action ai.Action) ailineage.Run {
	t.Helper()
	for _, run := range runs {
		if run.Action == action {
			return run
		}
	}
	t.Fatalf("missing action %s in %+v", action, runs)
	return ailineage.Run{}
}

func containsRun(runs []ailineage.Run, id string) bool {
	for _, run := range runs {
		if run.ID == id {
			return true
		}
	}
	return false
}

func mustRevision(t *testing.T, db *sql.DB, id string) *source.SourceRevision {
	t.Helper()
	revision, err := source.NewRepository(db).GetRevision(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return revision
}

func assertCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("%s count: got %d want %d", table, got, want)
	}
}
