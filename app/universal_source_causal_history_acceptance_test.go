package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/source"
)

// TestScenario1_DirectAssistantTurnIsUniversalSource tests:
// User Assistant Turn -> durable SourceRecord/SourceRevision -> SOURCE History event ->
// AI Run linked via ai_run_sources -> Proposal linked via change_proposal_sources ->
// Human Apply -> Domain Revision linked via revision_sources -> Causal Chain traverses to Source.
func TestScenario1_DirectAssistantTurnIsUniversalSource(t *testing.T) {
	fixture := newRound2Fixture(t)
	ctx := context.Background()

	// 1. Direct assistant turn
	userPrompt := "Please create a new project todo for Q4 budget review"
	assistantResult, err := fixture.app.AskAssistant(AskAssistantRequestDTO{
		Message: userPrompt,
	})
	if err != nil {
		t.Fatalf("AskAssistant failed: %v", err)
	}
	if assistantResult.ProposalID == "" {
		t.Fatal("expected proposal to be created by assistant")
	}

	propID := assistantResult.ProposalID

	// The review UI labels provenance from the capturing extension, so an
	// Assistant-turn Proposal must not be presented as a Local Source one.
	pending, err := fixture.app.GetPendingProposals()
	if err != nil {
		t.Fatalf("GetPendingProposals failed: %v", err)
	}
	if len(pending) != 1 || len(pending[0].SourceExtensionIDs) != 1 || pending[0].SourceExtensionIDs[0] != "core.assistant" {
		t.Fatalf("pending proposal must report core.assistant source extension, got %+v", pending)
	}
	single, err := fixture.app.GetProposal(propID)
	if err != nil {
		t.Fatalf("GetProposal failed: %v", err)
	}
	if len(single.SourceExtensionIDs) != 1 || single.SourceExtensionIDs[0] != "core.assistant" {
		t.Fatalf("proposal must report core.assistant source extension, got %v", single.SourceExtensionIDs)
	}

	// 2. Verify durable SourceRecord and SourceRevision were created for this user turn
	var sourceRecordCount, sourceRevCount int
	if err := fixture.db.QueryRow("SELECT COUNT(*) FROM source_records WHERE extension_id = 'core.assistant'").Scan(&sourceRecordCount); err != nil {
		t.Fatalf("query source_records: %v", err)
	}
	if sourceRecordCount != 1 {
		t.Fatalf("expected 1 assistant source record, got %d", sourceRecordCount)
	}
	if err := fixture.db.QueryRow("SELECT COUNT(*) FROM source_revisions").Scan(&sourceRevCount); err != nil {
		t.Fatalf("query source_revisions: %v", err)
	}
	if sourceRevCount != 1 {
		t.Fatalf("expected 1 source revision, got %d", sourceRevCount)
	}

	var sourceRevID string
	var sourcePayload []byte
	if err := fixture.db.QueryRow("SELECT id, payload_json FROM source_revisions LIMIT 1").Scan(&sourceRevID, &sourcePayload); err != nil {
		t.Fatalf("query source revision detail: %v", err)
	}
	if string(sourcePayload) == "" {
		t.Fatal("source revision payload must not be empty")
	}

	// 3. Verify SOURCE_REVISION_OBSERVED history event was recorded
	var srcObsEventCount int
	if err := fixture.db.QueryRow("SELECT COUNT(*) FROM history_events WHERE event_type = 'SOURCE_REVISION_OBSERVED' AND source_revision_id = ?", sourceRevID).Scan(&srcObsEventCount); err != nil {
		t.Fatalf("query SOURCE_REVISION_OBSERVED event: %v", err)
	}
	if srcObsEventCount != 1 {
		t.Fatalf("expected 1 SOURCE_REVISION_OBSERVED event for revision %s, got %d", sourceRevID, srcObsEventCount)
	}

	// 4. Verify AI run is linked to SourceRevision via ai_run_sources
	var aiRunSourceCount int
	if err := fixture.db.QueryRow("SELECT COUNT(*) FROM ai_run_sources WHERE source_revision_id = ?", sourceRevID).Scan(&aiRunSourceCount); err != nil {
		t.Fatalf("query ai_run_sources: %v", err)
	}
	if aiRunSourceCount != 1 {
		t.Fatalf("expected ai_run_sources linkage to source revision, got %d", aiRunSourceCount)
	}

	// 5. Verify Proposal is linked to SourceRevision via change_proposal_sources
	var propSourceCount int
	if err := fixture.db.QueryRow("SELECT COUNT(*) FROM change_proposal_sources WHERE proposal_id = ? AND source_revision_id = ?", propID, sourceRevID).Scan(&propSourceCount); err != nil {
		t.Fatalf("query change_proposal_sources: %v", err)
	}
	if propSourceCount != 1 {
		t.Fatalf("expected change_proposal_sources linkage for proposal %s, got %d", propID, propSourceCount)
	}

	// 6. Verify source_processing_state is COMPLETED with proposal_id
	procState, err := fixture.sourceService.GetProcessingState(ctx, sourceRevID)
	if err != nil {
		t.Fatalf("get processing state: %v", err)
	}
	if procState == nil || procState.State != source.ProcessingStatusCompleted {
		t.Fatalf("expected COMPLETED processing state, got %+v", procState)
	}
	if procState.ProposalID == nil || *procState.ProposalID != propID {
		t.Fatalf("expected ProposalID %s in processing state, got %v", propID, procState.ProposalID)
	}

	// 7. Apply Proposal -> Domain Revision
	_, err = fixture.app.ApplyProposal(ApplyProposalRequestDTO{ID: propID})
	if err != nil {
		t.Fatalf("ApplyProposal failed: %v", err)
	}
	var appliedRevID string
	if err := fixture.db.QueryRow("SELECT applied_revision_id FROM change_proposals WHERE id = ?", propID).Scan(&appliedRevID); err != nil {
		t.Fatalf("query applied_revision_id: %v", err)
	}
	if appliedRevID == "" {
		t.Fatal("expected non-empty applied_revision_id from proposal apply")
	}

	// 8. Verify Domain Revision is linked to SourceRevision via revision_sources
	var revSourceCount int
	if err := fixture.db.QueryRow("SELECT COUNT(*) FROM revision_sources WHERE revision_id = ? AND source_revision_id = ?", appliedRevID, sourceRevID).Scan(&revSourceCount); err != nil {
		t.Fatalf("query revision_sources: %v", err)
	}
	if revSourceCount != 1 {
		t.Fatalf("expected revision_sources linkage for revision %s, got %d", appliedRevID, revSourceCount)
	}

	// 9. Causal Chain: traverse from proposal apply event back to root Source
	page, err := fixture.app.QueryHistory(HistoryQueryRequestDTO{Days: 7, Scope: "PROPOSAL", Search: "applied"})
	if err != nil || len(page.Events) == 0 {
		t.Fatalf("failed to query applied event: %v", err)
	}
	appliedEventID := page.Events[0].ID

	chain, err := fixture.app.GetCausalChain(appliedEventID)
	if err != nil {
		t.Fatalf("GetCausalChain failed: %v", err)
	}
	if chain.RootEvent == nil || chain.RootEvent.EventType != string(history.EventSourceRevisionObserved) {
		t.Fatalf("expected root event to be SOURCE_REVISION_OBSERVED, got %+v", chain.RootEvent)
	}
	if chain.RootEvent.SourceRevisionID == nil || *chain.RootEvent.SourceRevisionID != sourceRevID {
		t.Fatalf("expected root event to link to SourceRevision %s, got %v", sourceRevID, chain.RootEvent.SourceRevisionID)
	}

	// The original user prompt is recoverable from the chain's root Source.
	if chain.RootSource == nil || chain.RootSource.ExtensionID != "core.assistant" || !strings.Contains(chain.RootSource.PayloadJSON, userPrompt) {
		t.Fatalf("causal chain must expose the originating Assistant Source payload, got %+v", chain.RootSource)
	}

	// 10. The Apply's domain consequence belongs to the chain, and the domain
	// event itself explains back to the originating Source.
	var domainEventID string
	for _, event := range chain.Events {
		if event.RevisionID != nil && *event.RevisionID == appliedRevID && event.EventType != string(history.EventProposalApplied) {
			domainEventID = event.ID
			break
		}
	}
	if domainEventID == "" {
		t.Fatalf("causal chain of PROPOSAL_APPLIED must include the domain event of revision %s, got %+v", appliedRevID, chain.Events)
	}
	domainChain, err := fixture.app.GetCausalChain(domainEventID)
	if err != nil {
		t.Fatalf("GetCausalChain(domain event) failed: %v", err)
	}
	if domainChain.RootEvent == nil || domainChain.RootEvent.SourceRevisionID == nil || *domainChain.RootEvent.SourceRevisionID != sourceRevID {
		t.Fatalf("domain event chain must root at SourceRevision %s, got %+v", sourceRevID, domainChain.RootEvent)
	}
	var sawApplied, sawProposalCreated bool
	for _, event := range domainChain.Events {
		sawApplied = sawApplied || event.EventType == string(history.EventProposalApplied)
		sawProposalCreated = sawProposalCreated || event.EventType == string(history.EventProposalCreated)
	}
	if !sawApplied || !sawProposalCreated {
		t.Fatalf("domain event chain must pass through Proposal Created and Applied, got %+v", domainChain.Events)
	}
}

// TestScenario2_ManualCreationIsSourceBacked tests:
// User explicit create command -> SourceRevision -> History -> direct deterministic domain mutation ->
// Domain Revision linked via revision_sources -> COMPLETED processing state.
func TestScenario2_ManualCreationIsSourceBacked(t *testing.T) {
	fixture := newRound2Fixture(t)
	ctx := context.Background()

	// A. Create WorkItem
	wi, err := fixture.app.CreateWorkItem(CreateWorkItemRequestDTO{
		Title: "Explicit User Created Work Item",
	})
	if err != nil {
		t.Fatalf("CreateWorkItem failed: %v", err)
	}

	// Verify SourceRecord and SourceRevision were created with core.manual
	var wiSourceCount int
	var wiSourceRevID string
	if err := fixture.db.QueryRow("SELECT r.id FROM source_revisions r JOIN source_records s ON r.source_record_id = s.id WHERE s.extension_id = 'core.manual' AND s.instance_id = 'workitem-create'").Scan(&wiSourceRevID); err != nil {
		t.Fatalf("query manual workitem source revision: %v", err)
	}
	if wiSourceRevID == "" {
		t.Fatal("expected non-empty source revision ID for manual workitem")
	}

	// Verify revision_sources links to the source revision
	if err := fixture.db.QueryRow("SELECT COUNT(*) FROM revision_sources WHERE source_revision_id = ?", wiSourceRevID).Scan(&wiSourceCount); err != nil {
		t.Fatalf("query revision_sources: %v", err)
	}
	if wiSourceCount != 1 {
		t.Fatalf("expected 1 revision_sources row for manual workitem, got %d", wiSourceCount)
	}

	// Verify source_processing_state is COMPLETED
	wiState, err := fixture.sourceService.GetProcessingState(ctx, wiSourceRevID)
	if err != nil || wiState == nil || wiState.State != source.ProcessingStatusCompleted {
		t.Fatalf("expected COMPLETED state for manual workitem source, got %+v", wiState)
	}

	// Verify WORK_ITEM_CREATED history event has causation pointing to SOURCE_REVISION_OBSERVED
	var wiCausationEventID *string
	if err := fixture.db.QueryRow("SELECT causation_event_id FROM history_events WHERE event_type = 'WORK_ITEM_CREATED' AND subject_entity_id = ?", wi.ID).Scan(&wiCausationEventID); err != nil {
		t.Fatalf("query WORK_ITEM_CREATED causation: %v", err)
	}
	if wiCausationEventID == nil || *wiCausationEventID == "" {
		t.Fatal("expected WORK_ITEM_CREATED causation_event_id pointing to SOURCE_REVISION_OBSERVED")
	}

	// B. Create Local Todo
	todo, err := fixture.app.CreateLocalTodo(CreateLocalTodoRequestDTO{
		Title: "Explicit User Created Todo",
	})
	if err != nil {
		t.Fatalf("CreateLocalTodo failed: %v", err)
	}

	var todoSourceRevID string
	if err := fixture.db.QueryRow("SELECT r.id FROM source_revisions r JOIN source_records s ON r.source_record_id = s.id WHERE s.extension_id = 'core.manual' AND s.instance_id = 'todo-create'").Scan(&todoSourceRevID); err != nil {
		t.Fatalf("query manual todo source revision: %v", err)
	}

	var todoRevSourceCount int
	if err := fixture.db.QueryRow("SELECT COUNT(*) FROM revision_sources WHERE source_revision_id = ?", todoSourceRevID).Scan(&todoRevSourceCount); err != nil {
		t.Fatalf("query todo revision_sources: %v", err)
	}
	if todoRevSourceCount != 1 {
		t.Fatalf("expected 1 revision_sources row for manual todo, got %d", todoRevSourceCount)
	}

	// Verify PLANNER_ITEM_CREATED history event has causation pointing to SOURCE_REVISION_OBSERVED
	var todoCausationEventID *string
	if err := fixture.db.QueryRow("SELECT causation_event_id FROM history_events WHERE event_type = 'PLANNER_ITEM_CREATED' AND subject_entity_id = ?", todo.ID).Scan(&todoCausationEventID); err != nil {
		t.Fatalf("query PLANNER_ITEM_CREATED causation: %v", err)
	}
	if todoCausationEventID == nil || *todoCausationEventID == "" {
		t.Fatal("expected PLANNER_ITEM_CREATED causation_event_id pointing to SOURCE_REVISION_OBSERVED")
	}

	// Causal Chain for Todo resolves back to the manual Source
	todoChain, err := fixture.app.GetCausalChainBySubject(todo.ID)
	if err != nil {
		t.Fatalf("GetCausalChainBySubject failed for todo: %v", err)
	}
	if todoChain.RootEvent == nil || todoChain.RootEvent.EventType != string(history.EventSourceRevisionObserved) {
		t.Fatalf("expected root event to be SOURCE_REVISION_OBSERVED, got %+v", todoChain.RootEvent)
	}

	// C. Schedule Action from a WorkItem without a Workflow is still captured
	// as the schedule-action surface.
	if _, err := fixture.app.CreateLocalTodo(CreateLocalTodoRequestDTO{
		Title:      "Scheduled From Work Item",
		WorkItemID: &wi.ID,
	}); err != nil {
		t.Fatalf("CreateLocalTodo (schedule action) failed: %v", err)
	}
	var scheduleActionSources int
	if err := fixture.db.QueryRow("SELECT COUNT(*) FROM source_records WHERE extension_id = 'core.manual' AND instance_id = 'schedule-action'").Scan(&scheduleActionSources); err != nil {
		t.Fatalf("query schedule-action source: %v", err)
	}
	if scheduleActionSources != 1 {
		t.Fatalf("expected 1 schedule-action Source for a WorkItem-scheduled Todo, got %d", scheduleActionSources)
	}
}

// TestScenario3_NegativeInvariant_TodoCompletionNeverCreatesSource tests:
// Authoritative Decision: NOT every user action creates a Source.
// Completing a Todo marks it complete in Planner, records PLANNER_ITEM_COMPLETED History,
// satisfies reminders, resolves attention issues, but NEVER inserts any row into source_records or source_revisions.
func TestScenario3_NegativeInvariant_TodoCompletionNeverCreatesSource(t *testing.T) {
	fixture := newRound2Fixture(t)

	// 1. Create a Todo
	todo, err := fixture.app.CreateLocalTodo(CreateLocalTodoRequestDTO{
		Title: "Complete Me Test",
	})
	if err != nil {
		t.Fatalf("create todo: %v", err)
	}

	// Record Source counts before completion
	var recordsBefore, revisionsBefore int
	if err := fixture.db.QueryRow("SELECT COUNT(*) FROM source_records").Scan(&recordsBefore); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRow("SELECT COUNT(*) FROM source_revisions").Scan(&revisionsBefore); err != nil {
		t.Fatal(err)
	}

	// 2. Complete the Todo
	completed, err := fixture.app.CompleteLocalTodo(todo.ID)
	if err != nil {
		t.Fatalf("CompleteLocalTodo failed: %v", err)
	}
	if completed.Status != "COMPLETED" {
		t.Fatalf("expected status COMPLETED, got %s", completed.Status)
	}

	// 3. Assert NEGATIVE INVARIANT: source_records and source_revisions counts did NOT change!
	var recordsAfter, revisionsAfter int
	if err := fixture.db.QueryRow("SELECT COUNT(*) FROM source_records").Scan(&recordsAfter); err != nil {
		t.Fatal(err)
	}
	if err := fixture.db.QueryRow("SELECT COUNT(*) FROM source_revisions").Scan(&revisionsAfter); err != nil {
		t.Fatal(err)
	}

	if recordsAfter != recordsBefore {
		t.Fatalf("NEGATIVE INVARIANT VIOLATION: CompleteLocalTodo created a SourceRecord! Before=%d, After=%d",
			recordsBefore, recordsAfter)
	}
	if revisionsAfter != revisionsBefore {
		t.Fatalf("NEGATIVE INVARIANT VIOLATION: CompleteLocalTodo created a SourceRevision! Before=%d, After=%d",
			revisionsBefore, revisionsAfter)
	}

	// 4. Assert PLANNER_ITEM_COMPLETED history event exists
	var completedEventCount int
	if err := fixture.db.QueryRow("SELECT COUNT(*) FROM history_events WHERE event_type = 'PLANNER_ITEM_COMPLETED' AND subject_entity_id = ?", todo.ID).Scan(&completedEventCount); err != nil {
		t.Fatal(err)
	}
	if completedEventCount != 1 {
		t.Fatalf("expected 1 PLANNER_ITEM_COMPLETED history event, got %d", completedEventCount)
	}
}

// TestScenario4_DerivedObjectsNotificationPresentationAndDelivery tests:
// Derived objects (Reminder, Attention, Notification) do not manufacture Sources.
// Presentation emits NOTIFICATION_PRESENTED History event and records presented_at idempotently.
// Causal Chain traverses back to original trigger.
func TestScenario4_DerivedObjectsNotificationPresentationAndDelivery(t *testing.T) {
	fixture := newRound2Fixture(t)
	ctx := context.Background()

	now := time.Now().UTC()
	dueStr := now.Format(time.RFC3339)

	// 1. Create a todo and trigger reminder attention / notification
	todo, err := fixture.app.CreateLocalTodo(CreateLocalTodoRequestDTO{Title: "Notification Causal Test Item", DueAt: &dueStr, OffsetsSeconds: []int{0}})
	if err != nil {
		t.Fatalf("create todo: %v", err)
	}

	triggerNow := time.Now().UTC()
	issue, notif, err := fixture.app.attentionService.OpenTodoReminderAttention(ctx, attention.OpenTodoReminderAttentionInput{
		PlannerItemID: todo.ID,
		Title:         "Reminder: " + todo.Title,
		OffsetSeconds: 0,
		DueAt:         now,
	}, triggerNow)
	if err != nil {
		t.Fatalf("open todo reminder attention: %v", err)
	}

	// Dispatch notification
	notifs, err := fixture.app.attentionService.DispatchPendingNotifications(ctx, triggerNow)
	if err != nil {
		t.Fatalf("dispatch pending notifications: %v", err)
	}
	if notifs.Sent != 1 {
		t.Fatalf("expected 1 sent notification, got %d", notifs.Sent)
	}

	// Verify initially NOT presented
	var presentedAtBefore *string
	if err := fixture.db.QueryRow("SELECT presented_at FROM notifications WHERE entity_id = ?", notif.ID).Scan(&presentedAtBefore); err != nil {
		t.Fatalf("query presented_at: %v", err)
	}
	if presentedAtBefore != nil {
		t.Fatalf("expected presented_at to be NULL initially, got %v", *presentedAtBefore)
	}

	// 2. Acknowledge in-app presentation
	if err := fixture.app.AcknowledgeNotificationPresented(notif.ID); err != nil {
		t.Fatalf("AcknowledgeNotificationPresented failed: %v", err)
	}

	// Verify presented_at is now set
	var presentedAtAfter *string
	if err := fixture.db.QueryRow("SELECT presented_at FROM notifications WHERE entity_id = ?", notif.ID).Scan(&presentedAtAfter); err != nil {
		t.Fatalf("query presented_at after: %v", err)
	}
	if presentedAtAfter == nil || *presentedAtAfter == "" {
		t.Fatal("expected presented_at to be populated in notifications table")
	}

	// Verify NOTIFICATION_PRESENTED History event exists
	var presentedEventCount int
	if err := fixture.db.QueryRow("SELECT COUNT(*) FROM history_events WHERE event_type = 'NOTIFICATION_PRESENTED' AND subject_entity_id = ?", notif.ID).Scan(&presentedEventCount); err != nil {
		t.Fatal(err)
	}
	if presentedEventCount != 1 {
		t.Fatalf("expected 1 NOTIFICATION_PRESENTED history event, got %d", presentedEventCount)
	}

	// Idempotency: second call does not re-emit or error
	if err := fixture.app.AcknowledgeNotificationPresented(notif.ID); err != nil {
		t.Fatalf("second AcknowledgeNotificationPresented failed: %v", err)
	}
	if err := fixture.db.QueryRow("SELECT COUNT(*) FROM history_events WHERE event_type = 'NOTIFICATION_PRESENTED' AND subject_entity_id = ?", notif.ID).Scan(&presentedEventCount); err != nil {
		t.Fatal(err)
	}
	if presentedEventCount != 1 {
		t.Fatalf("idempotency violated: expected 1 NOTIFICATION_PRESENTED event, got %d", presentedEventCount)
	}

	// 3. Causal Chain: inspect the presentation event
	var presentedEvID string
	if err := fixture.db.QueryRow("SELECT id FROM history_events WHERE event_type = 'NOTIFICATION_PRESENTED' AND subject_entity_id = ?", notif.ID).Scan(&presentedEvID); err != nil {
		t.Fatal(err)
	}

	chain, err := fixture.app.GetCausalChain(presentedEvID)
	if err != nil {
		t.Fatalf("GetCausalChain failed for notification presentation: %v", err)
	}
	if chain.TargetEventID != presentedEvID {
		t.Fatalf("expected target event %s, got %s", presentedEvID, chain.TargetEventID)
	}

	// The presented Notification explains back through Attention, the Todo's
	// reminder configuration, and the Todo's creation to the manual Source.
	if chain.RootEvent == nil || chain.RootEvent.EventType != string(history.EventSourceRevisionObserved) {
		t.Fatalf("notification chain must root at SOURCE_REVISION_OBSERVED, got %+v", chain.RootEvent)
	}
	seen := map[string]bool{}
	for _, event := range chain.Events {
		seen[event.EventType] = true
	}
	for _, want := range []history.EventType{
		history.EventPlannerItemCreated, history.EventPlannerItemReminderSet, history.EventAttentionOpened,
		history.EventNotificationCreated, history.EventNotificationSent, history.EventNotificationPresented,
	} {
		if !seen[string(want)] {
			t.Fatalf("notification causal chain is missing %s: %+v", want, chain.Events)
		}
	}

	// 4. Completing the Todo resolves its Attention, caused by the completion.
	if _, err := fixture.app.CompleteLocalTodo(todo.ID); err != nil {
		t.Fatalf("CompleteLocalTodo failed: %v", err)
	}
	var resolvedCauseType string
	if err := fixture.db.QueryRow(`SELECT cause.event_type FROM history_events resolved
		JOIN history_events cause ON cause.id = resolved.causation_event_id
		WHERE resolved.event_type = 'ATTENTION_RESOLVED' AND resolved.subject_entity_id = ?`, issue.ID).Scan(&resolvedCauseType); err != nil {
		t.Fatalf("query ATTENTION_RESOLVED cause: %v", err)
	}
	if resolvedCauseType != string(history.EventPlannerItemCompleted) {
		t.Fatalf("ATTENTION_RESOLVED must be caused by PLANNER_ITEM_COMPLETED, got %s", resolvedCauseType)
	}
}
