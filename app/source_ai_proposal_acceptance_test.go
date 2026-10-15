package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/ailineage"
	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/calendar"
	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/plannerview"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/reminder"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/sourceprocessing"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/trackingreevaluation"
	"lum-harbor-notifier/internal/workitem"
	"lum-harbor-notifier/internal/workitemoverview"
)

type round2TestFixture struct {
	t                           *testing.T
	homeDir                     string
	db                          *sql.DB
	app                         *App
	sourceService               *source.Service
	sourceProcessor             *sourceprocessing.Processor
	lineageService              *ailineage.Service
	proposalService             *proposal.Service
	plannerService              *planner.Service
	workitemService             *workitem.Service
	trackingService             *tracking.Service
	trackingReevaluationService *trackingreevaluation.Service
	historyService              *history.Service
	mockAI                      *mockAIService
}

type mockAIService struct {
	mu           sync.Mutex
	cleanOutput  string
	interpretDoc string
	assistantAsk string
	callCounts   map[ai.Action]int
	lastRequests map[ai.Action]ai.Request
	// onInterpret, when set, runs during source.interpret and the call then
	// fails with context.Canceled, as when a cycle deadline cancels it.
	onInterpret func()
}

func newMockAIService() *mockAIService {
	return &mockAIService{
		cleanOutput:  `{"version":"source.clean.v1","normalizedDocument":{"title":"Normalized Local Note","body":"Actionable content"}}`,
		interpretDoc: `{"version":"source.interpret.v1","summary":"Prepare Budget Review","interpretationDocument":{"candidate":{"version":1,"capability_id":"planner.item.create","arguments":{"title":"Prepare Budget Review"},"reason":"The source requests a budget review."}}}`,
		assistantAsk: `{"version":1,"kind":"MUTATION_CANDIDATE","text":"I will propose creating a Planner todo for budget review.","candidate":{"version":1,"capability_id":"planner.item.create","arguments":"{\"title\":\"Review Q4 Budget\",\"due_date\":\"2026-10-15\",\"due_time\":\"15:00\"}","reason":"User requested budget review"}}`,
		callCounts:   make(map[ai.Action]int),
		lastRequests: make(map[ai.Action]ai.Request),
	}
}

func (m *mockAIService) Execute(_ context.Context, req ai.Request) (ai.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callCounts[req.Action]++
	m.lastRequests[req.Action] = req

	output := m.cleanOutput
	switch req.Action {
	case ai.ActionSourceClean:
		output = m.cleanOutput
	case ai.ActionSourceInterpret:
		if m.onInterpret != nil {
			m.onInterpret()
			return ai.Response{}, context.Canceled
		}
		output = m.interpretDoc
	case ai.ActionRuntimeCapabilityDecision:
		output = `{"version":1,"decision":"FINAL","calls":[]}`
	case ai.ActionAssistantAsk:
		output = m.assistantAsk
	}

	return ai.Response{
		RequestID: req.RequestID,
		Action:    req.Action,
		Output:    json.RawMessage(output),
		Provenance: ai.ResponseProvenance{
			ProviderInvoked: true,
			ActualProvider: &ai.ProviderInfo{
				ID:   "mock-ai",
				Name: "Mock AI Provider",
			},
			Origin: ai.ResponseOrigin{
				Kind: ai.ResponseOriginProvider,
				ID:   "mock-ai",
				Name: "Mock AI Provider",
			},
		},
	}, nil
}

func newRound2Fixture(t *testing.T) *round2TestFixture {
	t.Helper()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	dbConn, err := database.Open(context.Background())
	if err != nil {
		t.Fatalf("database.Open: %v", err)
	}
	if err := database.Migrate(dbConn.DB); err != nil {
		t.Fatalf("database.Migrate: %v", err)
	}
	t.Cleanup(func() {
		_ = dbConn.DB.Close()
	})

	mockAI := newMockAIService()
	aiConfig := ai.Config{
		BuiltInProvider: ai.ProviderRegistration{
			ID:       "mock-ai",
			Name:     "Mock AI Provider",
			Provider: mockAI,
		},
	}
	aiSvc, err := ai.NewService(aiConfig)
	if err != nil {
		t.Fatalf("ai.NewService: %v", err)
	}

	f := buildAppWithDB(t, tempHome, dbConn.DB, mockAI, aiSvc)
	return f
}

func buildAppWithDB(t *testing.T, homeDir string, db *sql.DB, mockAI *mockAIService, aiSvc *ai.Service) *round2TestFixture {
	t.Helper()
	sourceSvc := source.NewService(db)
	sourceProc := sourceprocessing.NewProcessor(db, aiSvc)
	lineageSvc := ailineage.NewService(db)

	plannerRepo := planner.NewRepository(db)
	calendarRepo := calendar.NewRepository(db)
	plannerSvc := planner.NewService(db, plannerRepo)
	plannerViewSvc := plannerview.NewService(plannerSvc, calendarRepo)

	workitemRepo := workitem.NewRepository(db)
	workitemSvc := workitem.NewService(db, workitemRepo)
	trackingSvc := tracking.NewService(db, workitemSvc)
	attentionSvc := attention.NewService(db, trackingSvc)
	reminderSvc := reminder.NewService(db)
	trackingReevaluationSvc := trackingreevaluation.NewService(db)
	// Mirrors main.go: completing a Todo satisfies its reminder and resolves
	// its Attention in the completion transaction.
	plannerSvc.AddCompletionHook(func(ctx context.Context, tx *sql.Tx, plannerItemID, revisionID string, now time.Time) error {
		if err := reminderSvc.SatisfyReminderTx(ctx, tx, plannerItemID, revisionID, now); err != nil {
			return err
		}
		if err := attentionSvc.ResolveByPlannerItemTx(ctx, tx, plannerItemID, revisionID, now); err != nil {
			return err
		}
		return trackingReevaluationSvc.RequestForCompletionTx(ctx, tx, plannerItemID, revisionID, now)
	})

	proposalSvc := proposal.NewService(db, workitemSvc)
	proposalSvc.SetTrackingOperator(trackingSvc)
	proposalSvc.SetPlannerOperator(plannerSvc)
	proposalSvc.SetReminderOperator(reminderSvc)

	historySvc := history.NewService(history.NewRepository(db))
	overviewSvc := workitemoverview.NewService(workitemSvc, trackingSvc, attentionSvc, plannerSvc)

	readcapSvc, err := readcap.NewService(workitemSvc, trackingSvc, plannerSvc, db, nil, attentionSvc, historySvc)
	if err != nil {
		t.Fatalf("readcap.NewService: %v", err)
	}

	proposeReg := NewProposeRegistry()
	assistantRuntime, err := assistant.NewNativeRuntimeAdapter(aiSvc, readcapSvc, proposeReg)
	if err != nil {
		t.Fatalf("assistant.NewNativeRuntimeAdapter: %v", err)
	}

	app := &App{
		sourceService:               sourceSvc,
		sourceProcessor:             sourceProc,
		sourceContextReader:         readcapSvc,
		lineageService:              lineageSvc,
		workitemService:             workitemSvc,
		plannerService:              plannerSvc,
		plannerViewService:          plannerViewSvc,
		reminderService:             reminderSvc,
		attentionService:            attentionSvc,
		historyService:              historySvc,
		proposalService:             proposalSvc,
		trackingService:             trackingSvc,
		workItemOverviewService:     overviewSvc,
		assistantRuntime:            assistantRuntime,
		proposeRegistry:             proposeReg,
		trackingReevaluationService: trackingReevaluationSvc,
	}

	return &round2TestFixture{
		t:                           t,
		homeDir:                     homeDir,
		db:                          db,
		app:                         app,
		sourceService:               sourceSvc,
		sourceProcessor:             sourceProc,
		lineageService:              lineageSvc,
		proposalService:             proposalSvc,
		plannerService:              plannerSvc,
		workitemService:             workitemSvc,
		trackingService:             trackingSvc,
		trackingReevaluationService: trackingReevaluationSvc,
		historyService:              historySvc,
		mockAI:                      mockAI,
	}
}

// restart closes existing DB and reopens fresh services against the same disk DB file.
func (f *round2TestFixture) restart() {
	f.t.Helper()
	if err := f.db.Close(); err != nil {
		f.t.Fatalf("restart close db: %v", err)
	}

	f.t.Setenv("HOME", f.homeDir)
	reopened, err := database.Open(context.Background())
	if err != nil {
		f.t.Fatalf("restart database.Open: %v", err)
	}
	f.t.Cleanup(func() {
		_ = reopened.DB.Close()
	})
	f.db = reopened.DB

	aiConfig := ai.Config{
		BuiltInProvider: ai.ProviderRegistration{
			ID:       "mock-ai",
			Name:     "Mock AI Provider",
			Provider: f.mockAI,
		},
	}
	aiSvc, err := ai.NewService(aiConfig)
	if err != nil {
		f.t.Fatalf("restart ai.NewService: %v", err)
	}

	fresh := buildAppWithDB(f.t, f.homeDir, f.db, f.mockAI, aiSvc)
	f.app = fresh.app
	f.sourceService = fresh.sourceService
	f.sourceProcessor = fresh.sourceProcessor
	f.lineageService = fresh.lineageService
	f.proposalService = fresh.proposalService
	f.plannerService = fresh.plannerService
	f.workitemService = fresh.workitemService
	f.trackingService = fresh.trackingService
	f.trackingReevaluationService = fresh.trackingReevaluationService
	f.historyService = fresh.historyService
}

// ============================================================================
// SCENARIO R2-A: Path A Full Chain (Source-Driven Semantic Work)
// ============================================================================
func TestM19_Round2_ScenarioA_PathAFullChain(t *testing.T) {
	f := newRound2Fixture(t)

	// Step 1 & 2: Local Source Ingress -> durable SourceRevision
	localSource, err := f.app.CaptureLocalSource(LocalSourceCaptureRequestDTO{
		Title:   "Quarterly Audit Note",
		Content: "Review all Q3 compliance logs before October 15th",
	})
	if err != nil {
		t.Fatalf("CaptureLocalSource: %v", err)
	}
	if localSource.SourceRecordID == "" || localSource.SourceRevisionID == "" {
		t.Fatalf("expected non-empty source record and revision IDs: %+v", localSource)
	}
	if localSource.SourceKind != "NOTE" {
		t.Errorf("expected sourceKind NOTE, got %s", localSource.SourceKind)
	}

	// Verify DB record and revision
	var recordExtID, recordInstID string
	err = f.db.QueryRow("SELECT extension_id, instance_id FROM source_records WHERE id = ?", localSource.SourceRecordID).Scan(&recordExtID, &recordInstID)
	if err != nil {
		t.Fatalf("verify source_records: %v", err)
	}
	if recordExtID != "core.local" || recordInstID != "local-inbox" {
		t.Errorf("unexpected record source identity: %s/%s", recordExtID, recordInstID)
	}

	var revPayload string
	err = f.db.QueryRow("SELECT payload_json FROM source_revisions WHERE id = ?", localSource.SourceRevisionID).Scan(&revPayload)
	if err != nil {
		t.Fatalf("verify source_revisions: %v", err)
	}
	if !strings.Contains(revPayload, "Quarterly Audit Note") {
		t.Errorf("payload missing title: %s", revPayload)
	}

	// Verify ListLocalSources
	listedSources, err := f.app.ListLocalSources()
	if err != nil {
		t.Fatalf("ListLocalSources: %v", err)
	}
	if len(listedSources) != 1 || listedSources[0].SourceRevisionID != localSource.SourceRevisionID {
		t.Fatalf("expected 1 listed source with ID %s, got %+v", localSource.SourceRevisionID, listedSources)
	}

	// Step 3, 4, 5, 6, 7: Trigger Interpretation -> AI Run -> Mutation Candidate -> Proposal
	interpretRes, err := f.app.InterpretSource(InterpretSourceRequestDTO{
		SourceRevisionID: localSource.SourceRevisionID,
	})
	if err != nil {
		t.Fatalf("InterpretSource: %v", err)
	}
	if !interpretRes.HasProposal || interpretRes.Proposal == nil {
		t.Fatalf("expected Proposal created from interpretation, got %+v", interpretRes)
	}
	if interpretRes.CleanRunID == "" || interpretRes.InterpretRunID == "" {
		t.Fatalf("expected non-empty clean and interpret run IDs: %+v", interpretRes)
	}
	if interpretRes.CleanRunID == interpretRes.InterpretRunID {
		t.Fatalf("cleanRunID and interpretRunID must be distinct: %s", interpretRes.CleanRunID)
	}

	// Verify AI Runs in database
	var cleanSourceRevID string
	err = f.db.QueryRow("SELECT source_revision_id FROM ai_run_sources WHERE ai_run_id = ?", interpretRes.CleanRunID).Scan(&cleanSourceRevID)
	if err != nil {
		t.Fatalf("query clean ai_run_sources: %v", err)
	}
	if cleanSourceRevID != localSource.SourceRevisionID {
		t.Errorf("clean ai_run_sources source_revision_id: got %s, want %s", cleanSourceRevID, localSource.SourceRevisionID)
	}

	var upstreamRunID sql.NullString
	err = f.db.QueryRow("SELECT upstream_run_id FROM ai_runs WHERE id = ?", interpretRes.InterpretRunID).Scan(&upstreamRunID)
	if err != nil {
		t.Fatalf("query interpret ai_run: %v", err)
	}
	if !upstreamRunID.Valid || upstreamRunID.String != interpretRes.CleanRunID {
		t.Errorf("interpret ai_run upstream_run_id: got %v, want %s", upstreamRunID, interpretRes.CleanRunID)
	}

	// Verify change_proposals lineage in DB
	proposalID := interpretRes.Proposal.ID
	var propAIRunID sql.NullString
	err = f.db.QueryRow("SELECT ai_run_id FROM change_proposals WHERE id = ?", proposalID).Scan(&propAIRunID)
	if err != nil {
		t.Fatalf("query proposal ai_run_id: %v", err)
	}
	if !propAIRunID.Valid || propAIRunID.String != interpretRes.InterpretRunID {
		t.Errorf("proposal ai_run_id: got %v, want %s", propAIRunID, interpretRes.InterpretRunID)
	}

	// Verify change_proposal_sources table
	var linkedSourceRevID, linkedRole string
	err = f.db.QueryRow("SELECT source_revision_id, role FROM change_proposal_sources WHERE proposal_id = ?", proposalID).Scan(&linkedSourceRevID, &linkedRole)
	if err != nil {
		t.Fatalf("query change_proposal_sources: %v", err)
	}
	if linkedSourceRevID != localSource.SourceRevisionID {
		t.Errorf("change_proposal_sources source_revision_id: got %s, want %s", linkedSourceRevID, localSource.SourceRevisionID)
	}
	if linkedRole != "PRIMARY_SOURCE" {
		t.Errorf("change_proposal_sources role: got %s, want PRIMARY_SOURCE", linkedRole)
	}

	// Step 8: Human Review (API/DTO)
	pending, err := f.app.GetPendingProposals()
	if err != nil {
		t.Fatalf("GetPendingProposals: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending proposal, got %d", len(pending))
	}
	if pending[0].ID != proposalID {
		t.Errorf("expected proposal ID %s, got %s", proposalID, pending[0].ID)
	}
	if pending[0].AIRunID == nil || *pending[0].AIRunID != interpretRes.InterpretRunID {
		t.Errorf("expected proposal DTO to carry AIRunID %s, got %v", interpretRes.InterpretRunID, pending[0].AIRunID)
	}
	if len(pending[0].SourceRevisionIDs) != 1 || pending[0].SourceRevisionIDs[0] != localSource.SourceRevisionID {
		t.Errorf("expected proposal DTO to carry SourceRevisionIDs [%s], got %v", localSource.SourceRevisionID, pending[0].SourceRevisionIDs)
	}

	// Step 9 & 10: Human Apply -> Go Domain Service creates Domain Truth
	applyRes, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: proposalID})
	if err != nil {
		t.Fatalf("ApplyProposal: %v", err)
	}
	if applyRes.Outcome != "APPLIED" {
		t.Fatalf("expected APPLIED outcome, got %s", applyRes.Outcome)
	}

	// Verify Domain Truth created (Planner Todo)
	var todoTitle, todoLastRevID, todoStatus string
	err = f.db.QueryRow("SELECT title, last_revision_id, status FROM planner_items LIMIT 1").Scan(&todoTitle, &todoLastRevID, &todoStatus)
	if err != nil {
		t.Fatalf("query planner_items: %v", err)
	}
	if todoTitle != "Prepare Budget Review" {
		t.Errorf("expected todo title 'Prepare Budget Review', got %q", todoTitle)
	}
	if todoStatus != "OPEN" {
		t.Errorf("expected status OPEN, got %s", todoStatus)
	}

	// Step 11: Verify revision_sources linked to the Source Revision
	var revSourceRevID, revSourceRole string
	err = f.db.QueryRow("SELECT source_revision_id, role FROM revision_sources WHERE revision_id = ?", todoLastRevID).Scan(&revSourceRevID, &revSourceRole)
	if err != nil {
		t.Fatalf("query revision_sources: %v", err)
	}
	if revSourceRevID != localSource.SourceRevisionID {
		t.Errorf("revision_sources source_revision_id: got %s, want %s", revSourceRevID, localSource.SourceRevisionID)
	}
	if revSourceRole != "PRIMARY_SOURCE" {
		t.Errorf("revision_sources role: got %s, want PRIMARY_SOURCE", revSourceRole)
	}

	// Step 12: Verify History reflects full provenance chain
	var sourceEventCount, todoEventCount int
	err = f.db.QueryRow("SELECT COUNT(*) FROM history_events WHERE event_type = 'SOURCE_REVISION_OBSERVED'").Scan(&sourceEventCount)
	if err != nil {
		t.Fatalf("query history source events: %v", err)
	}
	err = f.db.QueryRow("SELECT COUNT(*) FROM history_events WHERE event_type = 'PLANNER_ITEM_CREATED'").Scan(&todoEventCount)
	if err != nil {
		t.Fatalf("query history todo events: %v", err)
	}
	if sourceEventCount == 0 {
		t.Error("history missing SOURCE_REVISION_OBSERVED event")
	}
	if todoEventCount == 0 {
		t.Error("history missing PLANNER_ITEM_CREATED event")
	}

	// Verify PROPOSAL_CREATED actor is AI (not USER)
	var propActor string
	err = f.db.QueryRow("SELECT actor_type FROM history_events WHERE event_type = 'PROPOSAL_CREATED' AND proposal_id = ?", proposalID).Scan(&propActor)
	if err != nil {
		t.Fatalf("query history PROPOSAL_CREATED actor: %v", err)
	}
	if propActor != "AI" {
		t.Errorf("expected PROPOSAL_CREATED actor AI, got %s", propActor)
	}
}

// ============================================================================
// SCENARIO R2-B: Path B Full Chain (Direct Assistant Intent)
// ============================================================================
func TestM19_Round2_ScenarioB_PathBFullChain(t *testing.T) {
	f := newRound2Fixture(t)

	// Step 1 & 2: User issues direct intent via Assistant
	assistantRes, err := f.app.AskAssistant(AskAssistantRequestDTO{
		Message: "remind me to review budget tomorrow at 3pm",
		Surface: "PLANNER",
	})
	if err != nil {
		t.Fatalf("AskAssistant: %v", err)
	}
	if assistantRes.ProposalOutcome != AssistantProposalCreated || assistantRes.ProposalID == "" {
		t.Fatalf("expected AssistantProposalCreated with ProposalID, got %+v", assistantRes)
	}

	proposalID := assistantRes.ProposalID

	// Step 3 & 5: Verify AI Run record created for assistant.ask
	var propAIRunID sql.NullString
	err = f.db.QueryRow("SELECT ai_run_id FROM change_proposals WHERE id = ?", proposalID).Scan(&propAIRunID)
	if err != nil {
		t.Fatalf("query change_proposals.ai_run_id: %v", err)
	}
	if !propAIRunID.Valid || propAIRunID.String == "" {
		t.Fatal("expected proposal to have non-null ai_run_id")
	}

	// Phase B regression: the direct Assistant AI Run must reach a terminal
	// state with the real provider provenance (it previously stayed RUNNING
	// because a fabricated provenance violated the ai_runs CHECK).
	var runStatus, runProviderName, runProviderSource string
	if err := f.db.QueryRow("SELECT status, actual_provider_name, actual_provider_source FROM ai_runs WHERE id = ?", propAIRunID.String).Scan(&runStatus, &runProviderName, &runProviderSource); err != nil {
		t.Fatalf("query assistant ai_run terminal state: %v", err)
	}
	if runStatus != "SUCCEEDED" || runProviderName != "Mock AI Provider" || runProviderSource != "BUILTIN" {
		t.Fatalf("assistant AI run must be SUCCEEDED with real provider provenance, got status=%s provider=%s source=%s", runStatus, runProviderName, runProviderSource)
	}

	var runAction string
	err = f.db.QueryRow("SELECT action FROM ai_runs WHERE id = ?", propAIRunID.String).Scan(&runAction)
	if err != nil {
		t.Fatalf("query ai_runs for assistant: %v", err)
	}
	if runAction != string(ai.ActionAssistantAsk) {
		t.Errorf("expected AI run action %s, got %s", ai.ActionAssistantAsk, runAction)
	}

	// Step 6: SOURCE PROVENANCE VERIFICATION
	// Direct Assistant user turn IS a real Source representing user intent!

	var sourceRecordCount, sourceRevCount, propSourceCount, runSourceCount int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM source_records").Scan(&sourceRecordCount)
	_ = f.db.QueryRow("SELECT COUNT(*) FROM source_revisions").Scan(&sourceRevCount)
	_ = f.db.QueryRow("SELECT COUNT(*) FROM change_proposal_sources WHERE proposal_id = ?", proposalID).Scan(&propSourceCount)
	_ = f.db.QueryRow("SELECT COUNT(*) FROM ai_run_sources WHERE ai_run_id = ?", propAIRunID.String).Scan(&runSourceCount)

	if sourceRecordCount != 1 {
		t.Errorf("expected 1 source_record for assistant turn, got %d", sourceRecordCount)
	}
	if sourceRevCount != 1 {
		t.Errorf("expected 1 source_revision for assistant turn, got %d", sourceRevCount)
	}
	if propSourceCount != 1 {
		t.Errorf("expected 1 change_proposal_sources row, got %d", propSourceCount)
	}
	if runSourceCount != 1 {
		t.Errorf("expected 1 ai_run_sources row, got %d", runSourceCount)
	}

	// Step 7: Human Review
	pending, err := f.app.GetPendingProposals()
	if err != nil {
		t.Fatalf("GetPendingProposals: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != proposalID {
		t.Fatalf("expected 1 pending proposal with ID %s, got %+v", proposalID, pending)
	}
	if len(pending[0].SourceRevisionIDs) != 1 {
		t.Errorf("expected 1 SourceRevisionID on Assistant proposal DTO, got %+v", pending[0].SourceRevisionIDs)
	}

	// Step 8 & 9: Human Apply
	applyRes, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: proposalID})
	if err != nil {
		t.Fatalf("ApplyProposal: %v", err)
	}
	if applyRes.Outcome != "APPLIED" {
		t.Fatalf("expected APPLIED outcome, got %s", applyRes.Outcome)
	}

	// Step 10: Verify Domain Truth created, and revision_sources links to the source revision!
	var todoTitle, todoLastRevID string
	err = f.db.QueryRow("SELECT title, last_revision_id FROM planner_items LIMIT 1").Scan(&todoTitle, &todoLastRevID)
	if err != nil {
		t.Fatalf("query planner_items: %v", err)
	}
	if todoTitle != "Review Q4 Budget" {
		t.Errorf("expected todo title 'Review Q4 Budget', got %q", todoTitle)
	}

	var revisionSourceCount int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM revision_sources WHERE revision_id = ?", todoLastRevID).Scan(&revisionSourceCount)
	if revisionSourceCount != 1 {
		t.Errorf("expected 1 revision_sources row for Assistant proposal apply, got %d", revisionSourceCount)
	}
}

// ============================================================================
// SCENARIO R2-C: Restart Durability Across Path A Steps
// ============================================================================
func TestM19_Round2_ScenarioC_RestartDurabilityAcrossPathASteps(t *testing.T) {
	f := newRound2Fixture(t)

	// Step 1: Ingress local source
	sourceNote, err := f.app.CaptureLocalSource(LocalSourceCaptureRequestDTO{
		Title:   "Restart Durability Note",
		Content: "Remember to verify cold-start durability across all transitions",
	})
	if err != nil {
		t.Fatalf("CaptureLocalSource: %v", err)
	}

	// RESTART 1: Cold restart after source ingress
	f.restart()

	// Verify source preserved after restart
	listed, err := f.app.ListLocalSources()
	if err != nil {
		t.Fatalf("ListLocalSources after restart: %v", err)
	}
	if len(listed) != 1 || listed[0].SourceRevisionID != sourceNote.SourceRevisionID {
		t.Fatalf("expected preserved source revision %s, got %+v", sourceNote.SourceRevisionID, listed)
	}

	// Step 2: Trigger Interpretation
	interpretRes, err := f.app.InterpretSource(InterpretSourceRequestDTO{
		SourceRevisionID: sourceNote.SourceRevisionID,
	})
	if err != nil {
		t.Fatalf("InterpretSource: %v", err)
	}
	proposalID := interpretRes.Proposal.ID

	// RESTART 2: Cold restart after interpretation & proposal creation
	f.restart()

	// Verify proposal and AI runs preserved after restart
	pending, err := f.app.GetPendingProposals()
	if err != nil {
		t.Fatalf("GetPendingProposals after restart: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != proposalID {
		t.Fatalf("expected proposal %s preserved after restart, got %+v", proposalID, pending)
	}
	if pending[0].AIRunID == nil || *pending[0].AIRunID != interpretRes.InterpretRunID {
		t.Errorf("expected preserved AIRunID %s, got %v", interpretRes.InterpretRunID, pending[0].AIRunID)
	}
	if len(pending[0].SourceRevisionIDs) != 1 || pending[0].SourceRevisionIDs[0] != sourceNote.SourceRevisionID {
		t.Errorf("expected preserved SourceRevisionIDs [%s], got %v", sourceNote.SourceRevisionID, pending[0].SourceRevisionIDs)
	}

	// Step 3: Human Apply
	applyRes, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: proposalID})
	if err != nil {
		t.Fatalf("ApplyProposal: %v", err)
	}
	if applyRes.Outcome != "APPLIED" {
		t.Fatalf("expected APPLIED outcome, got %s", applyRes.Outcome)
	}

	// RESTART 3: Cold restart post-Apply
	f.restart()

	// Verify Domain Truth and revision_sources preserved after post-apply restart
	var todoTitle, todoLastRevID string
	err = f.db.QueryRow("SELECT title, last_revision_id FROM planner_items LIMIT 1").Scan(&todoTitle, &todoLastRevID)
	if err != nil {
		t.Fatalf("query planner_items after restart: %v", err)
	}

	var linkedSourceRevID string
	err = f.db.QueryRow("SELECT source_revision_id FROM revision_sources WHERE revision_id = ?", todoLastRevID).Scan(&linkedSourceRevID)
	if err != nil {
		t.Fatalf("query revision_sources after restart: %v", err)
	}
	if linkedSourceRevID != sourceNote.SourceRevisionID {
		t.Errorf("revision_sources after restart: got %s, want %s", linkedSourceRevID, sourceNote.SourceRevisionID)
	}
}

// ============================================================================
// SCENARIO R2-D: Restart Durability Across Path B Steps
// ============================================================================
func TestM19_Round2_ScenarioD_RestartDurabilityAcrossPathBSteps(t *testing.T) {
	f := newRound2Fixture(t)

	// Step 1: Assistant creates proposal
	assistantRes, err := f.app.AskAssistant(AskAssistantRequestDTO{
		Message: "remind me to prepare presentation next Monday",
		Surface: "PLANNER",
	})
	if err != nil {
		t.Fatalf("AskAssistant: %v", err)
	}
	proposalID := assistantRes.ProposalID

	// RESTART 1: Cold restart after assistant proposal creation
	f.restart()

	// Verify proposal preserved with source records
	pending, err := f.app.GetPendingProposals()
	if err != nil {
		t.Fatalf("GetPendingProposals after restart: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != proposalID {
		t.Fatalf("expected preserved proposal %s, got %+v", proposalID, pending)
	}
	if len(pending[0].SourceRevisionIDs) != 1 {
		t.Errorf("expected 1 source revision on Assistant proposal after restart, got %v", pending[0].SourceRevisionIDs)
	}

	var sourceRecordCount int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM source_records").Scan(&sourceRecordCount)
	if sourceRecordCount != 1 {
		t.Errorf("expected 1 source record after restart, got %d", sourceRecordCount)
	}

	// Step 2: Apply proposal
	applyRes, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: proposalID})
	if err != nil {
		t.Fatalf("ApplyProposal: %v", err)
	}
	if applyRes.Outcome != "APPLIED" {
		t.Fatalf("expected APPLIED outcome, got %s", applyRes.Outcome)
	}

	// RESTART 2: Cold restart post-apply
	f.restart()

	// Verify Domain Truth preserved, revision_sources remains 1 row, source_records remains 1 row
	var todoTitle, todoLastRevID string
	err = f.db.QueryRow("SELECT title, last_revision_id FROM planner_items LIMIT 1").Scan(&todoTitle, &todoLastRevID)
	if err != nil {
		t.Fatalf("query planner_items after restart: %v", err)
	}
	if todoTitle != "Review Q4 Budget" {
		t.Errorf("expected todo title 'Review Q4 Budget', got %q", todoTitle)
	}

	var revisionSourceCount int
	_ = f.db.QueryRow("SELECT COUNT(*) FROM revision_sources WHERE revision_id = ?", todoLastRevID).Scan(&revisionSourceCount)
	if revisionSourceCount != 1 {
		t.Errorf("revision_sources must have 1 row after post-apply restart, got %d", revisionSourceCount)
	}

	_ = f.db.QueryRow("SELECT COUNT(*) FROM source_records").Scan(&sourceRecordCount)
	if sourceRecordCount != 1 {
		t.Errorf("source_records must remain 1 after post-apply restart, got %d", sourceRecordCount)
	}
}

// ============================================================================
// SCENARIO R2-E: Conflict / Stale Source / Stale Proposal Handling
// ============================================================================
func TestM19_Round2_ScenarioE_ConflictAndStaleHandling(t *testing.T) {
	f := newRound2Fixture(t)

	// 1. Identical source payload deduplication
	extID := "dup-note-1"
	first, err := f.app.CaptureLocalSource(LocalSourceCaptureRequestDTO{
		Title:      "Duplicate Test",
		Content:    "Exact same content payload",
		ExternalID: &extID,
	})
	if err != nil {
		t.Fatalf("CaptureLocalSource 1: %v", err)
	}

	second, err := f.app.CaptureLocalSource(LocalSourceCaptureRequestDTO{
		Title:      "Duplicate Test",
		Content:    "Exact same content payload",
		ExternalID: &extID,
	})
	if err != nil {
		t.Fatalf("CaptureLocalSource 2: %v", err)
	}

	// Content hash match should reuse the existing current revision
	if first.ContentHash != second.ContentHash {
		t.Errorf("expected identical content hashes: %s vs %s", first.ContentHash, second.ContentHash)
	}
	if first.SourceRevisionID != second.SourceRevisionID {
		t.Errorf("expected deduplication to reuse revision ID %s, got %s", first.SourceRevisionID, second.SourceRevisionID)
	}

	// 2. CAS Stale proposal apply rejection
	interpretRes, err := f.app.InterpretSource(InterpretSourceRequestDTO{
		SourceRevisionID: first.SourceRevisionID,
	})
	if err != nil {
		t.Fatalf("InterpretSource: %v", err)
	}
	proposalID := interpretRes.Proposal.ID

	// First apply succeeds
	apply1, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: proposalID})
	if err != nil {
		t.Fatalf("ApplyProposal 1: %v", err)
	}
	if apply1.Outcome != "APPLIED" {
		t.Fatalf("expected APPLIED outcome, got %s", apply1.Outcome)
	}

	// Second apply returns ALREADY_APPLIED
	apply2, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: proposalID})
	if err != nil {
		t.Fatalf("ApplyProposal 2: %v", err)
	}
	if apply2.Outcome != "ALREADY_APPLIED" {
		t.Errorf("expected ALREADY_APPLIED for duplicate apply, got %s", apply2.Outcome)
	}

	// Rejecting an already applied proposal fails cleanly with invalid transition
	_, err = f.app.RejectProposal(RejectProposalRequestDTO{ID: proposalID})
	if err == nil {
		t.Error("expected error when rejecting an already applied proposal, got nil")
	}
}

// ============================================================================
// SCENARIO R2-F: Real Built-in OpenAI Acceptance (Opt-in)
// ============================================================================
func TestM19_Round2_ScenarioF_LiveRealOpenAIAcceptance(t *testing.T) {
	if os.Getenv("LUM_HARBOR_OPENAI_ACCEPTANCE") != "1" {
		t.Skip("skipping live OpenAI acceptance test; set LUM_HARBOR_OPENAI_ACCEPTANCE=1 to run")
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY is not set; skipping live test")
	}

	t.Log("Running live OpenAI acceptance test against endpoint...")
}

// ============================================================================
// SCENARIO R2-G: Repeat Interpretation Semantics (Bounded V1)
// ============================================================================
func TestM19_Round2_ScenarioG_RepeatInterpretationSemantics(t *testing.T) {
	f := newRound2Fixture(t)

	// Step 1: Capture a local source
	sourceNote, err := f.app.CaptureLocalSource(LocalSourceCaptureRequestDTO{
		Title:   "Repeat Interpretation Note",
		Content: "Prepare monthly infrastructure budget analysis",
	})
	if err != nil {
		t.Fatalf("CaptureLocalSource: %v", err)
	}

	// Step 2: First interpretation -> Creates AI run and fresh pending Proposal
	res1, err := f.app.InterpretSource(InterpretSourceRequestDTO{
		SourceRevisionID: sourceNote.SourceRevisionID,
	})
	if err != nil {
		t.Fatalf("InterpretSource 1: %v", err)
	}
	if !res1.HasProposal || res1.Proposal == nil {
		t.Fatalf("expected proposal on first interpretation")
	}
	if res1.AlreadyPending {
		t.Errorf("expected fresh proposal on first interpretation, got alreadyPending = true")
	}
	prop1ID := res1.Proposal.ID

	cleanCalls1 := f.mockAI.callCounts[ai.ActionSourceClean]
	interpretCalls1 := f.mockAI.callCounts[ai.ActionSourceInterpret]
	if cleanCalls1 != 1 || interpretCalls1 != 1 {
		t.Fatalf("expected 1 clean and 1 interpret call, got clean=%d interpret=%d", cleanCalls1, interpretCalls1)
	}

	var aiRunsCount1 int
	err = f.db.QueryRow("SELECT COUNT(*) FROM ai_runs").Scan(&aiRunsCount1)
	if err != nil {
		t.Fatalf("query ai_runs count 1: %v", err)
	}
	if aiRunsCount1 != 2 { // 1 clean + 1 interpret
		t.Fatalf("expected 2 ai_runs, got %d", aiRunsCount1)
	}

	var aiHistoryCount1 int
	err = f.db.QueryRow("SELECT COUNT(*) FROM history_events WHERE actor_type = 'AI'").Scan(&aiHistoryCount1)
	if err != nil {
		t.Fatalf("query ai history count 1: %v", err)
	}

	var propCount int
	err = f.db.QueryRow("SELECT COUNT(*) FROM change_proposals").Scan(&propCount)
	if err != nil {
		t.Fatalf("query change_proposals count: %v", err)
	}
	if propCount != 1 {
		t.Fatalf("expected exactly 1 proposal in DB, got %d", propCount)
	}

	// Step 3: Second interpretation of the SAME SourceRevision while Proposal 1 is still PENDING
	// V1 Product Rule:
	// - DO NOT call AI again
	// - DO NOT execute clean, interpret, or token generation
	// - DO NOT create another AI Run, AI input/output, or AI history event
	// - REUSES existing pending proposal and returns notice
	res2, err := f.app.InterpretSource(InterpretSourceRequestDTO{
		SourceRevisionID: sourceNote.SourceRevisionID,
	})
	if err != nil {
		t.Fatalf("InterpretSource 2: %v", err)
	}
	if !res2.HasProposal || res2.Proposal == nil {
		t.Fatalf("expected proposal on second interpretation")
	}
	if res2.Proposal.ID != prop1ID {
		t.Errorf("expected proposal reuse: got %s, want %s", res2.Proposal.ID, prop1ID)
	}
	if !res2.AlreadyPending {
		t.Errorf("expected alreadyPending = true on repeat interpretation of pending source")
	}
	if res2.Notice != "This source already has a proposal waiting for review." {
		t.Errorf("unexpected notice on pending reuse: %s", res2.Notice)
	}

	// Verify ZERO AI calls were made during second interpretation
	if f.mockAI.callCounts[ai.ActionSourceClean] != cleanCalls1 {
		t.Errorf("expected no clean calls on pending reuse: got %d, want %d", f.mockAI.callCounts[ai.ActionSourceClean], cleanCalls1)
	}
	if f.mockAI.callCounts[ai.ActionSourceInterpret] != interpretCalls1 {
		t.Errorf("expected no interpret calls on pending reuse: got %d, want %d", f.mockAI.callCounts[ai.ActionSourceInterpret], interpretCalls1)
	}

	// Verify AI Runs count is completely UNCHANGED
	var aiRunsCount2 int
	err = f.db.QueryRow("SELECT COUNT(*) FROM ai_runs").Scan(&aiRunsCount2)
	if err != nil {
		t.Fatalf("query ai_runs count 2: %v", err)
	}
	if aiRunsCount2 != aiRunsCount1 {
		t.Errorf("expected AI run count unchanged (%d), got %d", aiRunsCount1, aiRunsCount2)
	}

	// Verify AI History count is completely UNCHANGED
	var aiHistoryCount2 int
	err = f.db.QueryRow("SELECT COUNT(*) FROM history_events WHERE actor_type = 'AI'").Scan(&aiHistoryCount2)
	if err != nil {
		t.Fatalf("query ai history count 2: %v", err)
	}
	if aiHistoryCount2 != aiHistoryCount1 {
		t.Errorf("expected AI history count unchanged (%d), got %d", aiHistoryCount1, aiHistoryCount2)
	}

	// Verify DB has exactly ONE proposal (no duplicate proposal created)
	err = f.db.QueryRow("SELECT COUNT(*) FROM change_proposals").Scan(&propCount)
	if err != nil {
		t.Fatalf("query change_proposals count: %v", err)
	}
	if propCount != 1 {
		t.Errorf("expected exactly 1 proposal in DB after duplicate interpretation, got %d", propCount)
	}

	// Step 4: User rejects Proposal 1
	_, err = f.app.RejectProposal(RejectProposalRequestDTO{ID: prop1ID})
	if err != nil {
		t.Fatalf("RejectProposal: %v", err)
	}

	// Step 5: Third interpretation AFTER rejection of Proposal 1
	// Rule: Subsequent intentional reinterpretation after resolution creates a NEW Proposal with fresh AI runs
	res3, err := f.app.InterpretSource(InterpretSourceRequestDTO{
		SourceRevisionID: sourceNote.SourceRevisionID,
	})
	if err != nil {
		t.Fatalf("InterpretSource 3: %v", err)
	}
	if !res3.HasProposal || res3.Proposal == nil {
		t.Fatalf("expected proposal on third interpretation")
	}
	if res3.AlreadyPending {
		t.Errorf("expected alreadyPending = false on reinterpretation after rejection")
	}
	prop2ID := res3.Proposal.ID
	if prop2ID == prop1ID {
		t.Errorf("expected new proposal ID after rejection, got same ID %s", prop2ID)
	}

	// Verify AI was indeed invoked again
	if f.mockAI.callCounts[ai.ActionSourceClean] != cleanCalls1+1 {
		t.Errorf("expected clean call count incremented to %d, got %d", cleanCalls1+1, f.mockAI.callCounts[ai.ActionSourceClean])
	}
	if f.mockAI.callCounts[ai.ActionSourceInterpret] != interpretCalls1+1 {
		t.Errorf("expected interpret call count incremented to %d, got %d", interpretCalls1+1, f.mockAI.callCounts[ai.ActionSourceInterpret])
	}

	// Verify fresh AI Runs were created
	var aiRunsCount3 int
	err = f.db.QueryRow("SELECT COUNT(*) FROM ai_runs").Scan(&aiRunsCount3)
	if err != nil {
		t.Fatalf("query ai_runs count 3: %v", err)
	}
	if aiRunsCount3 != aiRunsCount1+2 {
		t.Errorf("expected AI run count incremented to %d, got %d", aiRunsCount1+2, aiRunsCount3)
	}

	// Total proposals in DB is now 2 (one REJECTED, one PENDING)
	err = f.db.QueryRow("SELECT COUNT(*) FROM change_proposals").Scan(&propCount)
	if err != nil {
		t.Fatalf("query change_proposals count: %v", err)
	}
	if propCount != 2 {
		t.Errorf("expected 2 proposals in DB, got %d", propCount)
	}
}

// ============================================================================
// SCENARIO R2-H: Rejected Source Proposal + Cold Restart Durability
// ============================================================================
func TestM19_Round2_ScenarioH_RejectedProposalRestartDurability(t *testing.T) {
	f := newRound2Fixture(t)

	// Step 1: Capture a local source
	sourceNote, err := f.app.CaptureLocalSource(LocalSourceCaptureRequestDTO{
		Title:   "Reject and Restart Test Source",
		Content: "Review vendor security compliance by Friday",
	})
	if err != nil {
		t.Fatalf("CaptureLocalSource: %v", err)
	}

	// Step 2: Interpret Source to produce pending Proposal
	interpretRes, err := f.app.InterpretSource(InterpretSourceRequestDTO{
		SourceRevisionID: sourceNote.SourceRevisionID,
	})
	if err != nil {
		t.Fatalf("InterpretSource: %v", err)
	}
	if !interpretRes.HasProposal || interpretRes.Proposal == nil {
		t.Fatalf("expected proposal from interpretation")
	}
	proposalID := interpretRes.Proposal.ID

	// Step 3: Human USER rejects the Proposal
	rejectRes, err := f.app.RejectProposal(RejectProposalRequestDTO{ID: proposalID})
	if err != nil {
		t.Fatalf("RejectProposal: %v", err)
	}
	if rejectRes.Status != "REJECTED" {
		t.Fatalf("expected REJECTED status, got %s", rejectRes.Status)
	}

	// Step 4: COLD RESTART
	f.restart()

	// Step 5: Post-restart assertions
	// 1. Source record and revision remain intact
	sources, err := f.app.ListLocalSources()
	if err != nil {
		t.Fatalf("ListLocalSources after restart: %v", err)
	}
	if len(sources) != 1 || sources[0].SourceRevisionID != sourceNote.SourceRevisionID {
		t.Errorf("source revision not preserved after restart: %+v", sources)
	}

	// 2. Proposal remains REJECTED
	prop, err := f.app.GetProposal(proposalID)
	if err != nil {
		t.Fatalf("GetProposal after restart: %v", err)
	}
	if prop.Status != "REJECTED" {
		t.Errorf("proposal status after restart: got %s, want REJECTED", prop.Status)
	}
	if prop.AIRunID == nil || *prop.AIRunID != interpretRes.InterpretRunID {
		t.Errorf("proposal ai_run_id after restart: got %v, want %s", prop.AIRunID, interpretRes.InterpretRunID)
	}
	if len(prop.SourceRevisionIDs) != 1 || prop.SourceRevisionIDs[0] != sourceNote.SourceRevisionID {
		t.Errorf("proposal sourceRevisionIds after restart: got %+v", prop.SourceRevisionIDs)
	}

	// 3. change_proposal_sources remains intact
	var linkedRevID string
	err = f.db.QueryRow("SELECT source_revision_id FROM change_proposal_sources WHERE proposal_id = ?", proposalID).Scan(&linkedRevID)
	if err != nil {
		t.Fatalf("query change_proposal_sources after restart: %v", err)
	}
	if linkedRevID != sourceNote.SourceRevisionID {
		t.Errorf("linked source revision: got %s, want %s", linkedRevID, sourceNote.SourceRevisionID)
	}

	// 4. Domain Truth is completely untouched (0 planner items)
	var plannerCount int
	err = f.db.QueryRow("SELECT COUNT(*) FROM planner_items").Scan(&plannerCount)
	if err != nil {
		t.Fatalf("query planner_items: %v", err)
	}
	if plannerCount != 0 {
		t.Errorf("INVARIANT VIOLATION: rejected proposal must not create domain truth, got %d items", plannerCount)
	}

	// 5. revision_sources has 0 rows
	var revSourceCount int
	err = f.db.QueryRow("SELECT COUNT(*) FROM revision_sources").Scan(&revSourceCount)
	if err != nil {
		t.Fatalf("query revision_sources: %v", err)
	}
	if revSourceCount != 0 {
		t.Errorf("INVARIANT VIOLATION: rejected proposal must not create revision_sources, got %d rows", revSourceCount)
	}

	// 6. History explains rejection: PROPOSAL_REJECTED event exists with actor USER
	var rejectEventCount int
	var rejectActor string
	err = f.db.QueryRow("SELECT COUNT(*), actor_type FROM history_events WHERE event_type = 'PROPOSAL_REJECTED' AND proposal_id = ?", proposalID).Scan(&rejectEventCount, &rejectActor)
	if err != nil {
		t.Fatalf("query history PROPOSAL_REJECTED: %v", err)
	}
	if rejectEventCount == 0 {
		t.Error("history missing PROPOSAL_REJECTED event")
	}
	if rejectActor != "USER" {
		t.Errorf("expected PROPOSAL_REJECTED actor USER, got %s", rejectActor)
	}

	// 7. No background job auto-processes or mutates state on startup
	// Pending proposals list is empty
	pending, err := f.app.GetPendingProposals()
	if err != nil {
		t.Fatalf("GetPendingProposals after restart: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("expected 0 pending proposals after rejection + restart, got %d", len(pending))
	}
}
