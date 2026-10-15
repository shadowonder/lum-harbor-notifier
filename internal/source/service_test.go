package source

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/testsupport"
)

var (
	observedOne   = time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	observedTwo   = observedOne.Add(time.Minute)
	observedThree = observedTwo.Add(time.Minute)
)

func TestIngestCreatesStableRecordAndImmutableRevision(t *testing.T) {
	service, _ := newTestService(t)
	cursor := "cursor-1"
	result, err := ingestCurrent(service, testBatch("team-a", "item-1", observedOne, ptrTime(observedOne), `{"b":2,"a":1}`, &cursor))
	if err != nil {
		t.Fatal(err)
	}
	if result.CreatedCount != 1 || result.UpdatedCount != 0 || result.UnchangedCount != 0 || len(result.NewlyChangedRevisionIDs) != 1 || !result.CursorUpdated {
		t.Fatalf("unexpected ingestion result: %+v", result)
	}
	record, err := service.GetSourceRecordByIdentity(context.Background(), "dummy", "team-a", "dummy.todo", "item-1")
	if err != nil {
		t.Fatal(err)
	}
	if record.CurrentRevisionID != result.NewlyChangedRevisionIDs[0] || record.FirstSeenAt != observedOne || record.LastSeenAt != observedOne {
		t.Fatalf("unexpected source record: %+v", record)
	}
	revision, err := service.GetCurrentRevision(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(revision.Payload) != `{"a":1,"b":2}` || revision.ContentHash != record.CurrentContentHash {
		t.Fatalf("unexpected current revision: %+v", revision)
	}
	loaded, err := service.GetSourceRevision(context.Background(), revision.ID)
	if err != nil || loaded.ID != revision.ID {
		t.Fatalf("GetSourceRevision: revision=%+v err=%v", loaded, err)
	}
	state, err := service.GetSyncState(context.Background(), "dummy", "team-a", "source.fetch")
	if err != nil || state.Cursor == nil || *state.Cursor != cursor || state.CheckpointVersion != 1 {
		t.Fatalf("unexpected sync state: state=%+v err=%v", state, err)
	}
}

func TestIngestDeduplicatesCurrentPayloadAndDetectsChangesAndReversion(t *testing.T) {
	service, _ := newTestService(t)
	inputs := []struct {
		observed  time.Time
		payload   string
		created   int
		updated   int
		unchanged int
	}{
		{observedOne, `{"a":1,"b":2}`, 1, 0, 0},
		{observedTwo, "{ \"b\": 2, \"a\": 1 }", 0, 0, 1},
		{observedThree, `{"a":2,"b":2}`, 0, 1, 0},
		{observedThree.Add(time.Minute), `{"a":1,"b":2}`, 0, 1, 0},
	}
	var recordID string
	for index, input := range inputs {
		cursor := fmt.Sprintf("cursor-%d", index+1)
		result, err := ingestCurrent(service, testBatch("team-a", "item-1", input.observed, nil, input.payload, &cursor))
		if err != nil {
			t.Fatalf("ingest %d: %v", index, err)
		}
		if result.CreatedCount != input.created || result.UpdatedCount != input.updated || result.UnchangedCount != input.unchanged {
			t.Fatalf("ingest %d result: %+v", index, result)
		}
		record, err := service.GetSourceRecordByIdentity(context.Background(), "dummy", "team-a", "dummy.todo", "item-1")
		if err != nil {
			t.Fatal(err)
		}
		if recordID == "" {
			recordID = record.ID
		} else if record.ID != recordID {
			t.Fatal("stable identity changed")
		}
	}
	revisions, err := service.ListRevisionsForRecord(context.Background(), recordID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 3 {
		t.Fatalf("A -> A -> B -> A should produce three revisions, got %d", len(revisions))
	}
	if revisions[0].ContentHash != revisions[2].ContentHash || revisions[0].ID == revisions[2].ID {
		t.Fatalf("reversion was not retained as a new immutable revision: %+v", revisions)
	}
}

func TestObservationOrderingAndEqualSourceTimestamp(t *testing.T) {
	service, _ := newTestService(t)
	sourceTime := observedTwo
	if _, err := ingestCurrent(service, testBatch("team-a", "item-1", observedTwo, &sourceTime, `{"value":"B"}`, nil)); err != nil {
		t.Fatal(err)
	}
	olderSourceTime := observedOne
	stale, err := ingestCurrent(service, testBatch("team-a", "item-1", observedOne, &olderSourceTime, `{"value":"A"}`, nil))
	if err != nil {
		t.Fatal(err)
	}
	if stale.StaleCount != 1 || stale.UnchangedCount != 1 || stale.UpdatedCount != 0 {
		t.Fatalf("unexpected stale result: %+v", stale)
	}
	record, _ := service.GetSourceRecordByIdentity(context.Background(), "dummy", "team-a", "dummy.todo", "item-1")
	if record.LastSeenAt != observedTwo || record.SourceUpdatedAt == nil || *record.SourceUpdatedAt != sourceTime {
		t.Fatalf("stale observation moved record metadata backward: %+v", record)
	}

	equalChanged, err := ingestCurrent(service, testBatch("team-a", "item-1", observedThree, &sourceTime, `{"value":"C"}`, nil))
	if err != nil {
		t.Fatal(err)
	}
	if equalChanged.UpdatedCount != 1 {
		t.Fatalf("equal source timestamp with changed content was not revised: %+v", equalChanged)
	}
	revisions, _ := service.ListRevisionsForRecord(context.Background(), record.ID)
	if len(revisions) != 2 {
		t.Fatalf("expected two revisions, got %d", len(revisions))
	}
}

func TestObservedAtOrdersHeadWhenSourceUpdatedAtIsMissing(t *testing.T) {
	service, _ := newTestService(t)
	if _, err := ingestCurrent(service, testBatch("team-a", "item-1", observedTwo, nil, `{"value":"new"}`, nil)); err != nil {
		t.Fatal(err)
	}
	result, err := ingestCurrent(service, testBatch("team-a", "item-1", observedOne, nil, `{"value":"old"}`, nil))
	if err != nil {
		t.Fatal(err)
	}
	if result.StaleCount != 1 || result.UpdatedCount != 0 {
		t.Fatalf("older observation was not treated as stale: %+v", result)
	}
	record, _ := service.GetSourceRecordByIdentity(context.Background(), "dummy", "team-a", "dummy.todo", "item-1")
	revision, _ := service.GetCurrentRevision(context.Background(), record.ID)
	if string(revision.Payload) != `{"value":"new"}` || !record.LastSeenAt.Equal(observedTwo) {
		t.Fatalf("older observation rolled head backward: record=%+v revision=%s", record, revision.Payload)
	}
}

func TestBatchValidationRejectsDuplicateIdentityAndInvalidFields(t *testing.T) {
	service, db := newTestService(t)
	duplicate := testBatch("team-a", "item-1", observedOne, nil, `{}`, nil)
	duplicate.Records = append(duplicate.Records, duplicate.Records[0])
	if _, err := ingestCurrent(service, duplicate); err == nil {
		t.Fatal("expected duplicate identity to be rejected")
	}
	invalidCases := []FetchBatch{
		{InstanceID: "x", CapabilityID: "source.fetch"},
		{ExtensionID: "x", CapabilityID: "source.fetch"},
		{ExtensionID: "x", InstanceID: "y"},
		testBatch("team-a", "", observedOne, nil, `{}`, nil),
		testBatch("team-a", "item", time.Time{}, nil, `{}`, nil),
		testBatch("team-a", "item", observedOne, ptrTime(time.Time{}), `{}`, nil),
		testBatch("team-a", "item", observedOne, nil, `{`, nil),
	}
	for index, batch := range invalidCases {
		batch.Checkpoint = FetchCheckpoint{
			ExtensionID: batch.ExtensionID, InstanceID: batch.InstanceID, CapabilityID: batch.CapabilityID,
		}
		if _, err := service.IngestFetchResult(context.Background(), batch); err == nil {
			t.Fatalf("invalid case %d was accepted", index)
		}
	}
	assertTableCount(t, db, "source_records", 0)
	assertTableCount(t, db, "source_revisions", 0)
	assertTableCount(t, db, "source_sync_state", 0)
}

func TestNilNextCursorPreservesCheckpointAndZeroRecordFetchAdvancesIt(t *testing.T) {
	service, _ := newTestService(t)
	first := "cursor-1"
	if _, err := ingestCurrent(service, FetchBatch{ExtensionID: "dummy", InstanceID: "team-a", CapabilityID: "source.fetch", NextCursor: &first}); err != nil {
		t.Fatal(err)
	}
	result, err := ingestCurrent(service, FetchBatch{ExtensionID: "dummy", InstanceID: "team-a", CapabilityID: "source.fetch"})
	if err != nil {
		t.Fatal(err)
	}
	if result.CursorUpdated || result.Cursor == nil || *result.Cursor != first || result.CheckpointVersion != 2 {
		t.Fatalf("nil NextCursor did not preserve checkpoint: %+v", result)
	}
	second := "cursor-2"
	result, err = ingestCurrent(service, FetchBatch{ExtensionID: "dummy", InstanceID: "team-a", CapabilityID: "source.fetch", NextCursor: &second})
	if err != nil {
		t.Fatal(err)
	}
	if !result.CursorUpdated || result.Cursor == nil || *result.Cursor != second || result.CheckpointVersion != 3 {
		t.Fatalf("zero-record fetch did not advance supplied checkpoint: %+v", result)
	}
}

func TestBatchFailureRollsBackEarlierRecordsAndCursor(t *testing.T) {
	service, db := newTestService(t)
	oldCursor := "old"
	if _, err := ingestCurrent(service, FetchBatch{ExtensionID: "dummy", InstanceID: "team-a", CapabilityID: "source.fetch", NextCursor: &oldCursor}); err != nil {
		t.Fatal(err)
	}
	ids := []string{"same-record-id", "revision-1", "same-record-id", "revision-2"}
	service.newID = func() (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}
	batch := testBatch("team-a", "item-1", observedOne, nil, `{"value":1}`, ptrString("new"))
	batch.Records = append(batch.Records, Observation{ExternalID: "item-2", SourceKind: "dummy.todo", ObservedAt: observedTwo, Payload: json.RawMessage(`{"value":2}`)})
	if _, err := ingestCurrent(service, batch); err == nil {
		t.Fatal("expected second record constraint failure")
	}
	assertTableCount(t, db, "source_records", 0)
	assertTableCount(t, db, "source_revisions", 0)
	state, err := service.GetSyncState(context.Background(), "dummy", "team-a", "source.fetch")
	if err != nil || state.Cursor == nil || *state.Cursor != oldCursor || state.CheckpointVersion != 1 {
		t.Fatalf("failed batch advanced cursor: state=%+v err=%v", state, err)
	}
}

func TestRevisionFailureDoesNotAdvanceHead(t *testing.T) {
	service, _ := newTestService(t)
	first, err := ingestCurrent(service, testBatch("team-a", "item-1", observedOne, nil, `{"value":"A"}`, ptrString("one")))
	if err != nil {
		t.Fatal(err)
	}
	record, _ := service.GetSourceRecordByIdentity(context.Background(), "dummy", "team-a", "dummy.todo", "item-1")
	originalHead := record.CurrentRevisionID
	service.newID = func() (string, error) { return first.NewlyChangedRevisionIDs[0], nil }
	if _, err := ingestCurrent(service, testBatch("team-a", "item-1", observedTwo, nil, `{"value":"B"}`, ptrString("two"))); err == nil {
		t.Fatal("expected duplicate revision ID failure")
	}
	record, _ = service.GetSourceRecordByIdentity(context.Background(), "dummy", "team-a", "dummy.todo", "item-1")
	if record.CurrentRevisionID != originalHead {
		t.Fatalf("head advanced after revision failure: %+v", record)
	}
	revisions, _ := service.ListRevisionsForRecord(context.Background(), record.ID)
	if len(revisions) != 1 {
		t.Fatalf("revision failure persisted history: %d", len(revisions))
	}
	state, _ := service.GetSyncState(context.Background(), "dummy", "team-a", "source.fetch")
	if state.Cursor == nil || *state.Cursor != "one" || state.CheckpointVersion != 1 {
		t.Fatalf("revision failure advanced cursor: %+v", state)
	}
}

func TestSyncStateFailureRollsBackSourceWrites(t *testing.T) {
	service, db := newTestService(t)
	if _, err := db.Exec(`CREATE TRIGGER reject_source_sync BEFORE INSERT ON source_sync_state BEGIN SELECT RAISE(ABORT, 'sync rejected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := ingestCurrent(service, testBatch("team-a", "item-1", observedOne, nil, `{}`, ptrString("cursor"))); err == nil {
		t.Fatal("expected sync-state trigger failure")
	}
	assertTableCount(t, db, "source_records", 0)
	assertTableCount(t, db, "source_revisions", 0)
	assertTableCount(t, db, "source_sync_state", 0)
}

func TestInstancesRemainIndependentDuringConcurrentIngestion(t *testing.T) {
	service, db := newTestService(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, instance := range []string{"team-a", "team-b"} {
		instance := instance
		wg.Add(1)
		go func() {
			defer wg.Done()
			cursor := instance + "-cursor"
			_, err := ingestCurrent(service, testBatch(instance, "ITEM-1", observedOne, nil, `{"shared":true}`, &cursor))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertTableCount(t, db, "source_records", 2)
	assertTableCount(t, db, "source_revisions", 2)
	for _, instance := range []string{"team-a", "team-b"} {
		record, err := service.GetSourceRecordByIdentity(context.Background(), "dummy", instance, "dummy.todo", "ITEM-1")
		if err != nil || record.InstanceID != instance {
			t.Fatalf("instance record %s: %+v err=%v", instance, record, err)
		}
		state, err := service.GetSyncState(context.Background(), "dummy", instance, "source.fetch")
		if err != nil || state.Cursor == nil || *state.Cursor != instance+"-cursor" || state.CheckpointVersion != 1 {
			t.Fatalf("instance state %s: %+v err=%v", instance, state, err)
		}
	}
}

func TestConcurrentSameCheckpointRejectsLateFetchWithoutPartialWrites(t *testing.T) {
	service, db := newTestService(t)
	if _, err := ingestCurrent(service, FetchBatch{ExtensionID: "dummy", InstanceID: "team-a", CapabilityID: "source.fetch", NextCursor: ptrString("C0")}); err != nil {
		t.Fatal(err)
	}
	checkpointA, _ := service.PrepareFetch(context.Background(), "dummy", "team-a", "source.fetch")
	checkpointB, _ := service.PrepareFetch(context.Background(), "dummy", "team-a", "source.fetch")

	bDone := make(chan struct{})
	type outcome struct {
		name string
		err  error
	}
	outcomes := make(chan outcome, 2)
	go func() {
		batch := testBatch("team-a", "B-only", observedTwo, nil, `{"winner":"B"}`, ptrString("C2"))
		batch.Checkpoint = checkpointB
		_, err := service.IngestFetchResult(context.Background(), batch)
		outcomes <- outcome{name: "B", err: err}
		close(bDone)
	}()
	go func() {
		<-bDone
		batch := testBatch("team-a", "A-only", observedOne, nil, `{"stale":"A"}`, ptrString("C1"))
		batch.Checkpoint = checkpointA
		_, err := service.IngestFetchResult(context.Background(), batch)
		outcomes <- outcome{name: "A", err: err}
	}()
	results := map[string]error{}
	for range 2 {
		result := <-outcomes
		results[result.name] = result.err
	}
	if results["B"] != nil || !errors.Is(results["A"], ErrStaleSourceFetch) {
		t.Fatalf("unexpected concurrent outcomes: B=%v A=%v", results["B"], results["A"])
	}
	state, _ := service.GetSyncState(context.Background(), "dummy", "team-a", "source.fetch")
	if state.Cursor == nil || *state.Cursor != "C2" || state.CheckpointVersion != 2 {
		t.Fatalf("late fetch regressed checkpoint: %+v", state)
	}
	if _, err := service.GetSourceRecordByIdentity(context.Background(), "dummy", "team-a", "dummy.todo", "A-only"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale batch persisted a new record: %v", err)
	}
	assertTableCount(t, db, "source_records", 1)
	assertTableCount(t, db, "source_revisions", 1)
}

func TestSameCursorABAStillRejectsOldGeneration(t *testing.T) {
	service, _ := newTestService(t)
	if _, err := ingestCurrent(service, FetchBatch{ExtensionID: "dummy", InstanceID: "team-a", CapabilityID: "source.fetch", NextCursor: ptrString("same")}); err != nil {
		t.Fatal(err)
	}
	checkpointA, _ := service.PrepareFetch(context.Background(), "dummy", "team-a", "source.fetch")
	checkpointB, _ := service.PrepareFetch(context.Background(), "dummy", "team-a", "source.fetch")
	batchB := FetchBatch{ExtensionID: "dummy", InstanceID: "team-a", CapabilityID: "source.fetch", Checkpoint: checkpointB, NextCursor: ptrString("same")}
	result, err := service.IngestFetchResult(context.Background(), batchB)
	if err != nil || result.CheckpointVersion != 2 {
		t.Fatalf("same-cursor winner: result=%+v err=%v", result, err)
	}
	batchA := FetchBatch{ExtensionID: "dummy", InstanceID: "team-a", CapabilityID: "source.fetch", Checkpoint: checkpointA, NextCursor: ptrString("same")}
	if _, err := service.IngestFetchResult(context.Background(), batchA); !errors.Is(err, ErrStaleSourceFetch) {
		t.Fatalf("same-cursor stale generation was accepted: %v", err)
	}
	state, _ := service.GetSyncState(context.Background(), "dummy", "team-a", "source.fetch")
	if state.Cursor == nil || *state.Cursor != "same" || state.CheckpointVersion != 2 {
		t.Fatalf("ABA stale fetch changed state: %+v", state)
	}
}

func TestConcurrentNoStateOnlyOneInitialGenerationCommits(t *testing.T) {
	service, _ := newTestService(t)
	checkpointA, _ := service.PrepareFetch(context.Background(), "dummy", "team-a", "source.fetch")
	checkpointB, _ := service.PrepareFetch(context.Background(), "dummy", "team-a", "source.fetch")
	if checkpointA.Exists || checkpointB.Exists {
		t.Fatal("initial checkpoints unexpectedly existed")
	}
	winner := FetchBatch{ExtensionID: "dummy", InstanceID: "team-a", CapabilityID: "source.fetch", Checkpoint: checkpointB, NextCursor: ptrString("winner")}
	if result, err := service.IngestFetchResult(context.Background(), winner); err != nil || result.CheckpointVersion != 1 {
		t.Fatalf("initial winner: result=%+v err=%v", result, err)
	}
	stale := testBatch("team-a", "stale-new", observedOne, nil, `{}`, ptrString("loser"))
	stale.Checkpoint = checkpointA
	if _, err := service.IngestFetchResult(context.Background(), stale); !errors.Is(err, ErrStaleSourceFetch) {
		t.Fatalf("no-state loser was accepted: %v", err)
	}
	state, _ := service.GetSyncState(context.Background(), "dummy", "team-a", "source.fetch")
	if state.Cursor == nil || *state.Cursor != "winner" || state.CheckpointVersion != 1 {
		t.Fatalf("no-state loser changed winner: %+v", state)
	}
}

func TestStaleCheckpointCannotMoveExistingHead(t *testing.T) {
	service, _ := newTestService(t)
	if _, err := ingestCurrent(service, testBatch("team-a", "item", observedOne, nil, `{"value":"base"}`, ptrString("C0"))); err != nil {
		t.Fatal(err)
	}
	checkpointA, _ := service.PrepareFetch(context.Background(), "dummy", "team-a", "source.fetch")
	checkpointB, _ := service.PrepareFetch(context.Background(), "dummy", "team-a", "source.fetch")
	winner := testBatch("team-a", "item", observedTwo, nil, `{"value":"newer"}`, ptrString("C2"))
	winner.Checkpoint = checkpointB
	if _, err := service.IngestFetchResult(context.Background(), winner); err != nil {
		t.Fatal(err)
	}
	loser := testBatch("team-a", "item", observedThree, nil, `{"value":"late-stale"}`, ptrString("C1"))
	loser.Checkpoint = checkpointA
	if _, err := service.IngestFetchResult(context.Background(), loser); !errors.Is(err, ErrStaleSourceFetch) {
		t.Fatalf("stale head change was accepted: %v", err)
	}
	record, _ := service.GetSourceRecordByIdentity(context.Background(), "dummy", "team-a", "dummy.todo", "item")
	revision, _ := service.GetCurrentRevision(context.Background(), record.ID)
	revisions, _ := service.ListRevisionsForRecord(context.Background(), record.ID)
	if string(revision.Payload) != `{"value":"newer"}` || len(revisions) != 2 {
		t.Fatalf("stale fetch moved head/history: head=%s revisions=%d", revision.Payload, len(revisions))
	}
}

func TestDatabaseUniqueSourceIdentityConstraint(t *testing.T) {
	_, db := newTestService(t)
	insert := `INSERT INTO source_records (id, extension_id, instance_id, source_kind, external_id, first_seen_at, last_seen_at, created_at, updated_at) VALUES (?, 'dummy', 'team-a', 'kind', 'external', ?, ?, ?, ?)`
	stamp := "2026-09-17T12:00:00.000000000Z"
	if _, err := db.Exec(insert, "one", stamp, stamp, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(insert, "two", stamp, stamp, stamp, stamp); err == nil {
		t.Fatal("expected duplicate stable identity constraint failure")
	}
}

func TestDatabaseProtectsRevisionImmutabilityAndHeadOwnership(t *testing.T) {
	service, db := newTestService(t)
	if _, err := ingestCurrent(service, testBatch("team-a", "item-1", observedOne, nil, `{"value":1}`, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := ingestCurrent(service, testBatch("team-a", "item-2", observedOne, nil, `{"value":2}`, nil)); err != nil {
		t.Fatal(err)
	}
	recordOne, _ := service.GetSourceRecordByIdentity(context.Background(), "dummy", "team-a", "dummy.todo", "item-1")
	recordTwo, _ := service.GetSourceRecordByIdentity(context.Background(), "dummy", "team-a", "dummy.todo", "item-2")
	if _, err := db.Exec(`UPDATE source_revisions SET payload_json = '{"changed":true}' WHERE id = ?`, recordOne.CurrentRevisionID); err == nil {
		t.Fatal("expected revision update to be rejected")
	}
	if _, err := db.Exec(`DELETE FROM source_revisions WHERE id = ?`, recordOne.CurrentRevisionID); err == nil {
		t.Fatal("expected revision delete to be rejected")
	}
	if _, err := db.Exec(`UPDATE source_records SET current_revision_id = ?, current_content_hash = ? WHERE id = ?`, recordTwo.CurrentRevisionID, recordTwo.CurrentContentHash, recordOne.ID); err == nil {
		t.Fatal("expected cross-record head to be rejected")
	}
	if _, err := db.Exec(`UPDATE source_records SET current_revision_id = ?, current_content_hash = ? WHERE id = ?`, recordOne.CurrentRevisionID, recordTwo.CurrentContentHash, recordOne.ID); err == nil {
		t.Fatal("expected mismatched head hash to be rejected")
	}
}

func newTestService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	database := testsupport.OpenTestDatabase(t)
	service := NewService(database.DB)
	now := time.Date(2026, 9, 17, 18, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	return service, database.DB
}

func ingestCurrent(service *Service, batch FetchBatch) (IngestionResult, error) {
	checkpoint, err := service.PrepareFetch(context.Background(), batch.ExtensionID, batch.InstanceID, batch.CapabilityID)
	if err != nil {
		return IngestionResult{}, err
	}
	batch.Checkpoint = checkpoint
	return service.IngestFetchResult(context.Background(), batch)
}

func testBatch(instanceID, externalID string, observedAt time.Time, sourceUpdatedAt *time.Time, payload string, cursor *string) FetchBatch {
	return FetchBatch{
		ExtensionID: "dummy", InstanceID: instanceID, CapabilityID: "source.fetch", NextCursor: cursor,
		Records: []Observation{{ExternalID: externalID, SourceKind: "dummy.todo", ObservedAt: observedAt, SourceUpdatedAt: sourceUpdatedAt, Payload: json.RawMessage(payload)}},
	}
}

func assertTableCount(t *testing.T, db *sql.DB, table string, expected int) {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != expected {
		t.Fatalf("%s count: got %d want %d", table, count, expected)
	}
}

func ptrString(value string) *string     { return &value }
func ptrTime(value time.Time) *time.Time { return &value }

func TestIDGenerationFailureRollsBack(t *testing.T) {
	service, db := newTestService(t)
	service.newID = func() (string, error) { return "", errors.New("no IDs") }
	if _, err := ingestCurrent(service, testBatch("team-a", "item", observedOne, nil, `{}`, nil)); err == nil {
		t.Fatal("expected ID generation failure")
	}
	assertTableCount(t, db, "source_records", 0)
}

func TestSourceRevisionHistoryBatchUnchangedAndAtomicRollback(t *testing.T) {
	t.Run("changed and unchanged", func(t *testing.T) {
		service, db := newTestService(t)
		batch := testBatch("team-a", "item-1", observedOne, nil, `{"value":1}`, ptrString("one"))
		batch.Records = append(batch.Records, Observation{ExternalID: "item-2", SourceKind: "dummy.todo", ObservedAt: observedOne, Payload: json.RawMessage(`{"value":2}`)})
		result, err := ingestCurrent(service, batch)
		if err != nil || len(result.NewlyChangedRevisionIDs) != 2 {
			t.Fatalf("ingest: %+v %v", result, err)
		}
		repo := history.NewRepository(db)
		for _, revisionID := range result.NewlyChangedRevisionIDs {
			events, err := repo.ListByCorrelation(context.Background(), history.SourceRevisionCorrelation(revisionID), 10)
			if err != nil || len(events) != 1 || events[0].Type != history.EventSourceRevisionObserved || events[0].Actor != history.ActorSource || events[0].SourceRevisionID == nil || *events[0].SourceRevisionID != revisionID || events[0].RevisionID != nil || events[0].AIRunID != nil {
				t.Fatalf("source event %s: %+v %v", revisionID, events, err)
			}
		}
		beforeHistory := tableCount(t, db, "history_events")
		unchanged := testBatch("team-a", "item-1", observedTwo, nil, `{"value":1}`, ptrString("two"))
		unchanged.Records = append(unchanged.Records, Observation{ExternalID: "item-2", SourceKind: "dummy.todo", ObservedAt: observedTwo, Payload: json.RawMessage(`{"value":2}`)})
		result, err = ingestCurrent(service, unchanged)
		if err != nil || result.UnchangedCount != 2 || result.CheckpointVersion != 2 || tableCount(t, db, "history_events") != beforeHistory {
			t.Fatalf("unchanged fetch created History or failed checkpoint semantics: %+v %v", result, err)
		}
	})

	t.Run("second event failure rolls back whole batch", func(t *testing.T) {
		service, db := newTestService(t)
		if _, err := db.Exec(`CREATE TRIGGER fail_second_source_history BEFORE INSERT ON history_events WHEN NEW.event_type = 'SOURCE_REVISION_OBSERVED' AND NEW.after_json LIKE '%item-2%' BEGIN SELECT RAISE(ABORT, 'forced source history failure'); END`); err != nil {
			t.Fatal(err)
		}
		batch := testBatch("team-a", "item-1", observedOne, nil, `{"value":1}`, ptrString("cursor"))
		batch.Records = append(batch.Records, Observation{ExternalID: "item-2", SourceKind: "dummy.todo", ObservedAt: observedOne, Payload: json.RawMessage(`{"value":2}`)})
		if _, err := ingestCurrent(service, batch); err == nil {
			t.Fatal("expected injected History failure")
		}
		for _, table := range []string{"source_records", "source_revisions", "source_sync_state", "history_events"} {
			assertTableCount(t, db, table, 0)
		}
	})
}

func tableCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var value int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestCaptureIntentSourceAndProcessingStateLifecycle(t *testing.T) {
	service, db := newTestService(t)
	ctx := context.Background()

	// 1. Capture Intent Source
	payload := []byte(`{"message":"Create a new task","surface":"inbox"}`)
	rev, err := service.CaptureIntentSource(ctx, CaptureIntentInput{
		ExtensionID:    "core.assistant",
		InstanceID:     "direct-turn",
		CapabilityID:   "assistant.turn",
		SourceKind:     "USER_INTENT",
		ExternalID:     "turn-123",
		Payload:        payload,
		ProcessingMode: ProcessingModeSemanticProposal,
	})
	if err != nil {
		t.Fatalf("CaptureIntentSource: %v", err)
	}
	if rev == nil || rev.ID == "" {
		t.Fatalf("expected non-empty revision")
	}

	// Verify processing state is PENDING
	state, err := service.GetProcessingState(ctx, rev.ID)
	if err != nil {
		t.Fatalf("GetProcessingState: %v", err)
	}
	if state.State != ProcessingStatusPending || state.Mode != ProcessingModeSemanticProposal {
		t.Fatalf("unexpected state: %+v", state)
	}

	// Verify history event was created
	histRepo := history.NewRepository(db)
	events, err := histRepo.ListByCorrelation(ctx, history.SourceRevisionCorrelation(rev.ID), 10)
	if err != nil || len(events) != 1 || events[0].Type != history.EventSourceRevisionObserved {
		t.Fatalf("expected 1 SOURCE_REVISION_OBSERVED event, got %+v (err: %v)", events, err)
	}

	// 2. List Pending Semantic Sources
	pending, err := service.ListPendingSemanticSources(ctx, 10)
	if err != nil {
		t.Fatalf("ListPendingSemanticSources: %v", err)
	}
	if len(pending) != 1 || pending[0].SourceRevisionID != rev.ID {
		t.Fatalf("expected pending source revision %s, got %+v", rev.ID, pending)
	}

	// 3. Claim Pending Processing
	claimed, err := service.ClaimPendingProcessing(ctx, rev.ID)
	if err != nil || !claimed {
		t.Fatalf("ClaimPendingProcessing: claimed=%v, err=%v", claimed, err)
	}

	state, err = service.GetProcessingState(ctx, rev.ID)
	if err != nil {
		t.Fatalf("GetProcessingState: %v", err)
	}
	if state.State != ProcessingStatusProcessing || state.StartedAt == nil {
		t.Fatalf("expected PROCESSING state, got %+v", state)
	}

	// 4. Complete with Proposal
	proposalID := "prop-123"
	_, err = db.Exec(`INSERT INTO change_proposals (id, target_entity_id, operation_type, title, status, operation_payload_json, created_at, updated_at) VALUES (?, NULL, 'planner.item.create', 'Test Proposal', 'PENDING', '{}', '2026-09-26T00:00:00Z', '2026-09-26T00:00:00Z')`, proposalID)
	if err != nil {
		t.Fatalf("insert dummy proposal: %v", err)
	}
	if err := service.CompleteProcessingWithProposal(ctx, rev.ID, proposalID); err != nil {
		t.Fatalf("CompleteProcessingWithProposal: %v", err)
	}

	state, err = service.GetProcessingState(ctx, rev.ID)
	if err != nil {
		t.Fatalf("GetProcessingState: %v", err)
	}
	if state.State != ProcessingStatusCompleted || state.ProposalID == nil || *state.ProposalID != proposalID {
		t.Fatalf("expected COMPLETED with proposal, got %+v", state)
	}
}

func TestReleaseClaimToPending(t *testing.T) {
	service, _ := newTestService(t)
	ctx := context.Background()

	rev, err := service.CaptureIntentSource(ctx, CaptureIntentInput{
		ExtensionID:    "dummy",
		InstanceID:     "team-a",
		SourceKind:     "NOTE",
		ExternalID:     "item-deferred",
		Payload:        []byte(`{"title":"Deferred Note"}`),
		ProcessingMode: ProcessingModeSemanticProposal,
	})
	if err != nil {
		t.Fatalf("CaptureIntentSource: %v", err)
	}

	// 1. ReleaseClaimToPending fails when state is still PENDING (not claimed yet)
	released, err := service.ReleaseClaimToPending(ctx, rev.ID)
	if err != nil {
		t.Fatalf("ReleaseClaimToPending on PENDING: %v", err)
	}
	if released {
		t.Fatal("expected ReleaseClaimToPending on PENDING row to return false (0 rows affected)")
	}

	// 2. Claim pending processing
	claimed, err := service.ClaimPendingProcessing(ctx, rev.ID)
	if err != nil || !claimed {
		t.Fatalf("ClaimPendingProcessing: claimed=%v, err=%v", claimed, err)
	}
	state, err := service.GetProcessingState(ctx, rev.ID)
	if err != nil || state.State != ProcessingStatusProcessing || state.StartedAt == nil {
		t.Fatalf("expected PROCESSING state with started_at, got %+v (err %v)", state, err)
	}

	// 3. Release claim back to PENDING
	released, err = service.ReleaseClaimToPending(ctx, rev.ID)
	if err != nil || !released {
		t.Fatalf("ReleaseClaimToPending: released=%v, err=%v", released, err)
	}

	state, err = service.GetProcessingState(ctx, rev.ID)
	if err != nil {
		t.Fatalf("GetProcessingState after release: %v", err)
	}
	if state.State != ProcessingStatusPending {
		t.Fatalf("expected state PENDING after release, got %v", state.State)
	}
	if state.StartedAt != nil {
		t.Fatalf("expected started_at = nil after release, got %v", *state.StartedAt)
	}
	if state.ProposalID != nil {
		t.Fatalf("expected proposal_id = nil after release, got %v", *state.ProposalID)
	}
	if state.RevisionID != nil {
		t.Fatalf("expected revision_id = nil after release, got %v", *state.RevisionID)
	}

	// 4. Verify it remains eligible for ListPendingSemanticSources
	pending, err := service.ListPendingSemanticSources(ctx, 10)
	if err != nil {
		t.Fatalf("ListPendingSemanticSources after release: %v", err)
	}
	if len(pending) != 1 || pending[0].SourceRevisionID != rev.ID {
		t.Fatalf("expected 1 pending source revision %s, got %+v", rev.ID, pending)
	}

	// 5. Verify it can be claimed again in a subsequent pass
	claimedAgain, err := service.ClaimPendingProcessing(ctx, rev.ID)
	if err != nil || !claimedAgain {
		t.Fatalf("expected successful reclaim, got claimed=%v (err %v)", claimedAgain, err)
	}
}
