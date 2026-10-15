package database

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/foundation"
)

// TestMigrateCreatesFoundationSchema verifies that running Migrate once against
// a fresh database creates all of the foundational tables the schema expects.
func TestMigrateCreatesFoundationSchema(t *testing.T) {
	db := openMigrationTestDatabase(t)

	// Guard: migration itself must succeed before we can assert anything about
	// the resulting schema, so bail out the whole test immediately if it errors.
	if err := Migrate(db.DB); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	expectedTables := []string{
		"entities",
		"revisions",
		"external_refs",
		"work_items",
		"change_proposals",
		"change_proposal_sources",
		"proposal_operations",
		"proposal_operation_results",
		"planner_items",
		"source_records",
		"source_revisions",
		"source_sync_state",
		"source_processing_state",
		"revision_sources",
		"ai_runs",
		"ai_run_sources",
		"workflows",
		"workflow_definitions",
		"workflow_steps",
		"tracked_items",
		"tracked_item_step_states",
		"tracked_item_relations",
		"monitor_points",
		"attention_issues",
		"notifications",
		"notification_external_deliveries",
		"calendar_events",
		"todo_reminders",
		"tracking_reevaluation_requests",
		"history_events",
	}

	for _, tableName := range expectedTables {
		// Assertion: each expected table must exist after migration. Unlike the
		// setup check above, this uses t.Errorf so all missing tables are
		// reported in one run instead of stopping at the first failure.
		if !tableExists(t, db, tableName) {
			t.Errorf("expected table %q to exist after migration", tableName)
		}
	}
}

func TestWorkItemConstraints(t *testing.T) {
	db := openMigrationTestDatabase(t)
	now := "2026-09-18T12:00:00Z"

	insertPrerequisites := func(t *testing.T, suffix string) (string, string) {
		t.Helper()
		entityID := "work-item-entity-" + suffix
		revisionID := "work-item-revision-" + suffix
		if _, err := db.DB.Exec(`
			INSERT INTO entities (id, entity_type, created_at)
			VALUES (?, 'WORK_ITEM', ?)
		`, entityID, now); err != nil {
			t.Fatalf("insert entity prerequisite: %v", err)
		}
		if _, err := db.DB.Exec(`
			INSERT INTO revisions (id, origin_type, summary, applied_at)
			VALUES (?, 'USER', 'work item constraint test', ?)
		`, revisionID, now); err != nil {
			t.Fatalf("insert revision prerequisite: %v", err)
		}
		return entityID, revisionID
	}

	insert := func(entityID, revisionID, title, lifecycle string, completedAt any) error {
		_, err := db.DB.Exec(`
			INSERT INTO work_items (
				entity_id, title, lifecycle_state, completed_at,
				last_revision_id, created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, entityID, title, lifecycle, completedAt, revisionID, now, now)
		return err
	}

	for _, lifecycle := range []string{"ACTIVE", "PAUSED", "COMPLETED", "CANCELLED"} {
		t.Run("accepts_"+lifecycle, func(t *testing.T) {
			entityID, revisionID := insertPrerequisites(t, strings.ToLower(lifecycle))
			var completedAt any
			if lifecycle == "COMPLETED" {
				completedAt = now
			}
			if err := insert(entityID, revisionID, "Valid item", lifecycle, completedAt); err != nil {
				t.Fatalf("expected lifecycle %s to be accepted: %v", lifecycle, err)
			}
		})
	}

	t.Run("rejects_invalid_lifecycle", func(t *testing.T) {
		entityID, revisionID := insertPrerequisites(t, "invalid-lifecycle")
		if err := insert(entityID, revisionID, "Invalid lifecycle", "UNKNOWN", nil); err == nil {
			t.Fatal("expected invalid lifecycle rejection")
		}
	})

	t.Run("completed_requires_completed_at", func(t *testing.T) {
		entityID, revisionID := insertPrerequisites(t, "completed-without-time")
		if err := insert(entityID, revisionID, "Missing completion time", "COMPLETED", nil); err == nil {
			t.Fatal("expected COMPLETED without completed_at to be rejected")
		}
	})

	t.Run("non_completed_rejects_completed_at", func(t *testing.T) {
		entityID, revisionID := insertPrerequisites(t, "active-with-time")
		if err := insert(entityID, revisionID, "Unexpected completion time", "ACTIVE", now); err == nil {
			t.Fatal("expected ACTIVE with completed_at to be rejected")
		}
	})

	t.Run("rejects_blank_title", func(t *testing.T) {
		entityID, revisionID := insertPrerequisites(t, "blank-title")
		if err := insert(entityID, revisionID, "   ", "ACTIVE", nil); err == nil {
			t.Fatal("expected blank title to be rejected")
		}
	})

	t.Run("enforces_entity_foreign_key", func(t *testing.T) {
		_, revisionID := insertPrerequisites(t, "missing-entity")
		if err := insert("does-not-exist", revisionID, "Missing entity", "ACTIVE", nil); err == nil {
			t.Fatal("expected missing entity foreign key rejection")
		}
	})

	t.Run("enforces_revision_foreign_key", func(t *testing.T) {
		entityID, _ := insertPrerequisites(t, "missing-revision")
		if err := insert(entityID, "does-not-exist", "Missing revision", "ACTIVE", nil); err == nil {
			t.Fatal("expected missing revision foreign key rejection")
		}
	})
}

func TestPlannerWorkItemForeignKey(t *testing.T) {
	db := openMigrationTestDatabase(t)
	now := "2026-09-18T12:00:00Z"
	if _, err := db.DB.Exec(`
		INSERT INTO revisions (id, origin_type, summary, applied_at)
		VALUES ('planner-work-item-revision', 'USER', 'planner FK test', ?)
	`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`
		INSERT INTO entities (id, entity_type, created_at)
		VALUES ('planner-work-item-entity', 'PLANNER_ITEM', ?)
	`, now); err != nil {
		t.Fatal(err)
	}
	_, err := db.DB.Exec(`
		INSERT INTO planner_items (
			entity_id, authority_type, work_item_id, item_type, title,
			status, context_status, last_revision_id, created_at, updated_at
		)
		VALUES (?, 'LOCAL', ?, 'TODO', 'Invalid relation', 'OPEN', 'UNKNOWN', ?, ?, ?)
	`, "planner-work-item-entity", "missing-work-item", "planner-work-item-revision", now, now)
	if err == nil {
		t.Fatal("expected planner_items.work_item_id foreign key rejection")
	}
}

// TestMigrateCanRunMultipleTimes verifies that Migrate is idempotent: applying
// it a second time on an already-migrated database must not error or drop
// existing tables.
func TestMigrateCanRunMultipleTimes(t *testing.T) {
	db := openMigrationTestDatabase(t)

	// Guard: first migration run must succeed to set up the baseline schema.
	if err := Migrate(db.DB); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	// Assertion: re-running Migrate against a database that already has all
	// migrations applied must be a no-op, not an error (goose should detect
	// the already-applied versions and skip them).
	if err := Migrate(db.DB); err != nil {
		t.Fatalf("second migration failed: %v", err)
	}

	// Assertion: the schema created by the first run must still be intact
	// after the second, redundant migration run.
	if !tableExists(t, db, "entities") {
		t.Fatal("expected entities table to still exist after second migration")
	}
}

func openMigrationTestDatabase(t *testing.T) *Database {
	t.Helper()
	tempHome := t.TempDir()

	t.Setenv("HOME", tempHome)
	db, err := Open(context.Background())
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	t.Cleanup(func() {
		if err := db.DB.Close(); err != nil {
			t.Errorf("failed to close test database: %v", err)
		}
	})

	return db
}

func tableExists(t *testing.T, db *Database, tableName string) bool {
	t.Helper()

	var count int

	err := db.DB.QueryRow(
		`
		SELECT COUNT(*) from sqlite_master 
		WHERE type = 'table'
		AND name = ?
		`,
		tableName,
	).Scan(&count)

	if err != nil {
		t.Fatalf(
			"failed to check whether table %q exists: %v",
			tableName,
			err,
		)
	}

	return count > 0
}

// TestPlannerItemRejectsInvalidType verifies one stable database invariant:
//
// Planner item types are limited to the four types defined by the V3
// domain model. Even if application code contains a bug, SQLite should
// reject an invalid type rather than persist corrupted domain state.
func TestPlannerItemRejectsInvalidType(t *testing.T) {
	db := openMigrationTestDatabase(t)

	if err := Migrate(db.DB); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	// planner_items.entity_id references entities, so create the
	// corresponding global Entity first.
	_, err := db.DB.Exec(`
		INSERT INTO entities (
			id,
			entity_type,
			created_at
		)
		VALUES (?, ?, ?)
	`,
		"planner-test-1",
		"PLANNER_ITEM",
		"2026-09-13T23:00:00Z",
	)
	if err != nil {
		t.Fatalf("failed to create planner entity: %v", err)
	}

	// planner_items.last_revision_id must reference an existing Revision.
	//
	// In the real application this will later be created by the Service
	// inside the same transaction as the Planner Item mutation.
	_, err = db.DB.Exec(`
		INSERT INTO revisions (
			id,
			revision_no,
			origin_type,
			summary,
			applied_at
		)
		VALUES (?, ?, ?, ?, ?)
	`,
		"revision-test-1",
		1,
		"USER",
		"planner schema test",
		"2026-09-13T23:00:00Z",
	)
	if err != nil {
		t.Fatalf("failed to create test revision: %v", err)
	}

	// MAGIC_TASK is deliberately invalid.
	//
	// We expect the database CHECK constraint on item_type to reject
	// this INSERT.
	_, err = db.DB.Exec(`
		INSERT INTO planner_items (
			entity_id,
			authority_type,
			item_type,
			title,
			status,
			context_status,
			last_revision_id,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		"planner-test-1",
		"LOCAL",
		"MAGIC_TASK",
		"Invalid test item",
		"OPEN",
		"COMPLETE",
		"revision-test-1",
		"2026-09-13T23:00:00Z",
		"2026-09-13T23:00:00Z",
	)

	if err == nil {
		t.Fatal("expected invalid planner item type to be rejected")
	}
	if message := err.Error(); !strings.Contains(message, "CHECK constraint failed") ||
		!strings.Contains(message, "item_type") {
		t.Fatalf("expected item_type CHECK constraint failure, got: %v", err)
	}
}

// TestRevisionOriginConstraint keeps the foundation schema's accepted origin
// values aligned with the Go domain constants and proves SQLite rejects values
// outside that closed set. Every other inserted revision field is valid, so an
// invalid-origin failure can only come from the targeted CHECK constraint.
func TestRevisionOriginConstraint(t *testing.T) {
	db := openMigrationTestDatabase(t)

	validOrigins := []foundation.RevisionOrigin{
		foundation.RevisionOriginUser,
		foundation.RevisionOriginAI,
		foundation.RevisionOriginSync,
		foundation.RevisionOriginImport,
		foundation.RevisionOriginSystem,
	}
	for index, origin := range validOrigins {
		_, err := db.DB.Exec(`
			INSERT INTO revisions (id, origin_type, summary, applied_at)
			VALUES (?, ?, ?, ?)
		`, fmt.Sprintf("revision-valid-origin-%d", index), string(origin), "origin constraint test", "2026-09-16T12:00:00Z")
		if err != nil {
			t.Fatalf("expected revision origin %q to be accepted: %v", origin, err)
		}
	}

	_, err := db.DB.Exec(`
		INSERT INTO revisions (id, origin_type, summary, applied_at)
		VALUES (?, ?, ?, ?)
	`, "revision-invalid-origin", "UNKNOWN_ORIGIN", "origin constraint test", "2026-09-16T12:00:00Z")
	if err == nil {
		t.Fatal("expected UNKNOWN_ORIGIN revision to be rejected")
	}
	if message := err.Error(); !strings.Contains(message, "CHECK constraint failed") ||
		!strings.Contains(message, "origin_type") {
		t.Fatalf("expected origin_type CHECK constraint failure, got: %v", err)
	}
}

func TestChangeProposalConstraints(t *testing.T) {
	db := openMigrationTestDatabase(t)
	now := "2026-09-19T12:00:00Z"
	validPayload := `{"operation_kind":"work_item.update_details","version":"v1","preconditions":{"expected_last_revision_id":"rev-1","expected_title":"Old Title"},"proposed_title":"New Title"}`

	// Prerequisite: create target entity and work item revision
	entityID := "entity-target-wi"
	revisionID := "rev-1"
	if _, err := db.DB.Exec(`INSERT INTO entities (id, entity_type, created_at) VALUES (?, 'WORK_ITEM', ?)`, entityID, now); err != nil {
		t.Fatalf("insert entity prerequisite: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO revisions (id, origin_type, applied_at) VALUES (?, 'USER', ?)`, revisionID, now); err != nil {
		t.Fatalf("insert revision prerequisite: %v", err)
	}

	// 1. Valid PENDING proposal insert must succeed
	_, err := db.DB.Exec(`
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, summary, reason,
			status, operation_payload_json, applied_revision_id, created_at, updated_at, resolved_at
		) VALUES (
			'prop-1', ?, 'work_item.update_details', 'Update Title', 'Summary', 'Reason',
			'PENDING', ?, NULL, ?, ?, NULL
		)
	`, entityID, validPayload, now, now)
	if err != nil {
		t.Fatalf("expected valid PENDING proposal insert to succeed: %v", err)
	}

	// 2. Foreign key check on target_entity_id (nonexistent entity must fail)
	_, err = db.DB.Exec(`
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, created_at, updated_at
		) VALUES (
			'prop-nonexistent-target', 'nonexistent-entity', 'work_item.update_details', 'Title', 'PENDING', ?, ?, ?
		)
	`, validPayload, now, now)
	if err == nil {
		t.Fatal("expected foreign key constraint failure for nonexistent target_entity_id")
	}

	// 3. Status CHECK constraint (invalid status must fail)
	_, err = db.DB.Exec(`
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, created_at, updated_at
		) VALUES (
			'prop-invalid-status', ?, 'work_item.update_details', 'Title', 'UNKNOWN_STATUS', ?, ?, ?
		)
	`, entityID, validPayload, now, now)
	if err == nil {
		t.Fatal("expected status CHECK constraint failure for UNKNOWN_STATUS")
	}

	// 4. json_valid on operation_payload_json (malformed JSON must fail)
	_, err = db.DB.Exec(`
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, created_at, updated_at
		) VALUES (
			'prop-malformed-json', ?, 'work_item.update_details', 'Title', 'PENDING', '{malformed', ?, ?
		)
	`, entityID, now, now)
	if err == nil {
		t.Fatal("expected json_valid CHECK constraint failure for malformed JSON")
	}

	// 5. Blank title CHECK constraint
	_, err = db.DB.Exec(`
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, created_at, updated_at
		) VALUES (
			'prop-blank-title', ?, 'work_item.update_details', '   ', 'PENDING', ?, ?, ?
		)
	`, entityID, validPayload, now, now)
	if err == nil {
		t.Fatal("expected title CHECK constraint failure for blank title")
	}

	// 6. resolved_at / status consistency: PENDING cannot have resolved_at
	_, err = db.DB.Exec(`
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, created_at, updated_at, resolved_at
		) VALUES (
			'prop-pending-with-resolved', ?, 'work_item.update_details', 'Title', 'PENDING', ?, ?, ?, ?
		)
	`, entityID, validPayload, now, now, now)
	if err == nil {
		t.Fatal("expected CHECK failure when PENDING has resolved_at")
	}

	// 7. resolved_at / status consistency: REJECTED must have resolved_at
	_, err = db.DB.Exec(`
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, created_at, updated_at, resolved_at
		) VALUES (
			'prop-rejected-without-resolved', ?, 'work_item.update_details', 'Title', 'REJECTED', ?, ?, ?, NULL
		)
	`, entityID, validPayload, now, now)
	if err == nil {
		t.Fatal("expected CHECK failure when REJECTED has NULL resolved_at")
	}

	// 8. applied_revision_id / status consistency: PENDING cannot have applied_revision_id
	_, err = db.DB.Exec(`
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, applied_revision_id, created_at, updated_at
		) VALUES (
			'prop-pending-with-applied-rev', ?, 'work_item.update_details', 'Title', 'PENDING', ?, ?, ?, ?
		)
	`, entityID, validPayload, revisionID, now, now)
	if err == nil {
		t.Fatal("expected CHECK failure when PENDING has applied_revision_id")
	}

	// 9. applied_revision_id / status consistency: APPLIED must have applied_revision_id
	_, err = db.DB.Exec(`
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, applied_revision_id, created_at, updated_at, resolved_at
		) VALUES (
			'prop-applied-without-rev', ?, 'work_item.update_details', 'Title', 'APPLIED', ?, NULL, ?, ?, ?
		)
	`, entityID, validPayload, now, now, now)
	if err == nil {
		t.Fatal("expected CHECK failure when APPLIED has NULL applied_revision_id")
	}
}

func TestTrackingSchemaConstraints(t *testing.T) {
	db := openMigrationTestDatabase(t)
	now := "2026-09-18T12:00:00Z"
	exec := func(query string, args ...any) error {
		_, err := db.DB.Exec(query, args...)
		return err
	}
	mustExec := func(query string, args ...any) {
		t.Helper()
		if err := exec(query, args...); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	mustExec(`INSERT INTO revisions (id, origin_type, summary, applied_at) VALUES ('rev', 'USER', 's', ?)`, now)
	for _, id := range []string{"wi", "wi2", "ti", "ti2"} {
		mustExec(`INSERT INTO entities (id, entity_type, created_at) VALUES (?, 'X', ?)`, id, now)
	}
	for _, id := range []string{"wi", "wi2"} {
		mustExec(`INSERT INTO work_items (entity_id, title, lifecycle_state, last_revision_id, created_at, updated_at) VALUES (?, 't', 'ACTIVE', 'rev', ?, ?)`, id, now, now)
	}
	mustExec(`INSERT INTO workflow_definitions (id, name, version, active, created_at) VALUES ('wf', 'n', 1, 1, ?)`, now)
	mustExec(`INSERT INTO workflow_steps (id, workflow_definition_id, step_key, title, sequence_no, terminal) VALUES ('s1', 'wf', 'a', 'A', 1, 0)`)
	mustExec(`INSERT INTO workflows (id, work_item_id, workflow_definition_id, current_step_id, started_at, last_revision_id, created_at, updated_at) VALUES ('w1', 'wi', 'wf', 's1', ?, 'rev', ?, ?)`, now, now, now)
	mustExec(`INSERT INTO workflows (id, work_item_id, workflow_definition_id, current_step_id, started_at, last_revision_id, created_at, updated_at) VALUES ('w2', 'wi2', 'wf', 's1', ?, 'rev', ?, ?)`, now, now, now)

	insertItem := func(id, workItem string, parent any, lifecycle string, completed any, workflow any) error {
		return exec(`INSERT INTO tracked_items (entity_id, work_item_id, workflow_id, parent_item_id, title, lifecycle_state, completed_at, last_revision_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'x', ?, ?, 'rev', ?, ?)`, id, workItem, workflow, parent, lifecycle, completed, now, now)
	}
	if err := insertItem("ti", "wi", nil, "ACTIVE", nil, "w1"); err != nil {
		t.Fatalf("valid tracked item rejected: %v", err)
	}
	expectReject := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: schema must reject", name)
		}
	}
	expectReject("invalid lifecycle", insertItem("ti2", "wi", nil, "PAUSED", nil, "w1"))
	expectReject("ARCHIVED lifecycle (entity archival is the authority)", insertItem("ti2", "wi", nil, "ARCHIVED", nil, "w1"))
	expectReject("COMPLETED without completed_at", insertItem("ti2", "wi", nil, "COMPLETED", nil, "w1"))
	expectReject("completed_at while ACTIVE", insertItem("ti2", "wi", nil, "ACTIVE", now, "w1"))
	expectReject("unknown work item", insertItem("ti2", "missing", nil, "ACTIVE", nil, "w1"))
	expectReject("unknown workflow", insertItem("ti2", "wi", nil, "ACTIVE", nil, "missing"))
	expectReject("workflow belonging to other work item", insertItem("ti2", "wi", nil, "ACTIVE", nil, "w2"))
	expectReject("unknown parent", insertItem("ti2", "wi", "missing", "ACTIVE", nil, "w1"))
	expectReject("parent from another work item", insertItem("ti2", "wi2", "ti", "ACTIVE", nil, "w2"))
	expectReject("duplicate workflow for same work item", exec(`INSERT INTO workflows (id, work_item_id, workflow_definition_id, current_step_id, started_at, last_revision_id, created_at, updated_at) VALUES ('w1_dup', 'wi', 'wf', 's1', ?, 'rev', ?, ?)`, now, now, now))
	if err := insertItem("ti2", "wi", "ti", "ACTIVE", nil, "w1"); err != nil {
		t.Fatalf("same-work-item parent must be accepted: %v", err)
	}

	// Workflow.current_step_id is constrained to the Workflow's own definition,
	// and current_step_id / started_at / last_revision_id are mandatory.
	mustExec(`INSERT INTO workflow_definitions (id, name, version, active, created_at) VALUES ('wf_other', 'o', 1, 1, ?)`, now)
	mustExec(`INSERT INTO workflow_steps (id, workflow_definition_id, step_key, title, sequence_no, terminal) VALUES ('s_other', 'wf_other', 'a', 'A', 1, 1)`)
	mustExec(`INSERT INTO entities (id, entity_type, created_at) VALUES ('wi3', 'X', ?)`, now)
	mustExec(`INSERT INTO work_items (entity_id, title, lifecycle_state, last_revision_id, created_at, updated_at) VALUES ('wi3', 't', 'ACTIVE', 'rev', ?, ?)`, now, now)
	insertWorkflow := func(id, workItem, step any, started any) error {
		return exec(`INSERT INTO workflows (id, work_item_id, workflow_definition_id, current_step_id, started_at, last_revision_id, created_at, updated_at) VALUES (?, ?, 'wf', ?, ?, 'rev', ?, ?)`, id, workItem, step, started, now, now)
	}
	expectReject("current step from another WorkflowDefinition", insertWorkflow("w3", "wi3", "s_other", now))
	expectReject("unknown current step", insertWorkflow("w3", "wi3", "missing", now))
	expectReject("NULL current step", insertWorkflow("w3", "wi3", nil, now))
	expectReject("NULL started_at", insertWorkflow("w3", "wi3", "s1", nil))
	if err := insertWorkflow("w3", "wi3", "s1", now); err != nil {
		t.Fatalf("valid workflow rejected: %v", err)
	}
	expectReject("moving current step to a step of another definition", exec(`UPDATE workflows SET current_step_id = 's_other' WHERE id = 'w3'`))
	expectReject("workflow without last_revision_id", exec(`INSERT INTO workflows (id, work_item_id, workflow_definition_id, current_step_id, started_at, last_revision_id, created_at, updated_at) VALUES ('w4', 'wi3', 'wf', 's1', ?, 'nope', ?, ?)`, now, now, now))

	insertState := func(state string, step string) error {
		return exec(`INSERT INTO tracked_item_step_states (tracked_item_id, workflow_step_id, state, last_revision_id) VALUES ('ti', ?, ?, 'rev')`, step, state)
	}
	expectReject("invalid step state", insertState("DONE", "s1"))
	expectReject("unknown step", insertState("NOT_STARTED", "missing"))
	for _, state := range []string{"NOT_STARTED"} {
		if err := insertState(state, "s1"); err != nil {
			t.Fatalf("valid step state rejected: %v", err)
		}
	}
	expectReject("duplicate (item, step) state", insertState("BLOCKED", "s1"))

	expectReject("duplicate step key", exec(`INSERT INTO workflow_steps (id, workflow_definition_id, step_key, title, sequence_no, terminal) VALUES ('s2', 'wf', 'a', 'B', 2, 0)`))
	expectReject("duplicate sequence", exec(`INSERT INTO workflow_steps (id, workflow_definition_id, step_key, title, sequence_no, terminal) VALUES ('s2', 'wf', 'b', 'B', 1, 0)`))
	expectReject("zero sequence", exec(`INSERT INTO workflow_steps (id, workflow_definition_id, step_key, title, sequence_no, terminal) VALUES ('s2', 'wf', 'b', 'B', 0, 0)`))
	expectReject("blank step key", exec(`INSERT INTO workflow_steps (id, workflow_definition_id, step_key, title, sequence_no, terminal) VALUES ('s2', 'wf', ' ', 'B', 2, 0)`))
	expectReject("step of unknown workflow", exec(`INSERT INTO workflow_steps (id, workflow_definition_id, step_key, title, sequence_no, terminal) VALUES ('s2', 'missing', 'b', 'B', 2, 0)`))
	expectReject("non-boolean terminal", exec(`INSERT INTO workflow_steps (id, workflow_definition_id, step_key, title, sequence_no, terminal) VALUES ('s2', 'wf', 'b', 'B', 2, 2)`))
	expectReject("zero workflow version", exec(`INSERT INTO workflow_definitions (id, name, version, active, created_at) VALUES ('wf0', 'n2', 0, 1, ?)`, now))
	expectReject("blank workflow name", exec(`INSERT INTO workflow_definitions (id, name, version, active, created_at) VALUES ('wf0', ' ', 1, 1, ?)`, now))
	expectReject("duplicate (name, version)", exec(`INSERT INTO workflow_definitions (id, name, version, active, created_at) VALUES ('wf0', 'n', 1, 1, ?)`, now))

	for _, table := range []string{"tracked_items", "tracked_item_step_states", "workflow_definitions", "workflow_steps"} {
		rows, err := db.DB.Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(name, "current") {
				t.Fatalf("%s.%s: current workflow position must never be persisted", table, name)
			}
		}
		_ = rows.Close()
	}
}

func TestMonitorPointSchemaConstraints(t *testing.T) {
	db := openMigrationTestDatabase(t)
	now := "2026-09-18T12:00:00Z"
	exec := func(query string, args ...any) error {
		_, err := db.DB.Exec(query, args...)
		return err
	}
	must := func(query string, args ...any) {
		t.Helper()
		if err := exec(query, args...); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	must(`INSERT INTO revisions (id, origin_type, summary, applied_at) VALUES ('rev', 'USER', 's', ?)`, now)
	for _, id := range []string{"wi", "wi2", "ti", "mp1", "mp2", "mp3"} {
		must(`INSERT INTO entities (id, entity_type, created_at) VALUES (?, 'X', ?)`, id, now)
	}
	for _, id := range []string{"wi", "wi2"} {
		must(`INSERT INTO work_items (entity_id, title, lifecycle_state, last_revision_id, created_at, updated_at) VALUES (?, 't', 'ACTIVE', 'rev', ?, ?)`, id, now, now)
	}
	must(`INSERT INTO workflow_definitions (id, name, version, active, created_at) VALUES ('wf', 'n', 1, 1, ?)`, now)
	must(`INSERT INTO workflow_steps (id, workflow_definition_id, step_key, title, sequence_no, terminal) VALUES ('s1', 'wf', 'a', 'A', 1, 0)`)
	must(`INSERT INTO workflows (id, work_item_id, workflow_definition_id, current_step_id, started_at, last_revision_id, created_at, updated_at) VALUES ('w1', 'wi', 'wf', 's1', ?, 'rev', ?, ?)`, now, now, now)
	must(`INSERT INTO tracked_items (entity_id, work_item_id, workflow_id, title, lifecycle_state, last_revision_id, created_at, updated_at) VALUES ('ti', 'wi', 'w1', 'x', 'ACTIVE', 'rev', ?, ?)`, now, now)

	insert := func(id, workItem string, subject any, kind, state string, satisfied any, expectedKind string, lifecycle, step any) error {
		return exec(`INSERT INTO monitor_points (
			entity_id, work_item_id, subject_tracked_item_id, monitor_kind, title, due_at, timezone,
			expected_kind, expected_work_item_lifecycle, expected_step_id, state, satisfied_at,
			last_revision_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, 't', ?, 'UTC', ?, ?, ?, ?, ?, 'rev', ?, ?)`,
			id, workItem, subject, kind, now, expectedKind, lifecycle, step, state, satisfied, now, now)
	}
	reject := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: schema must reject", name)
		}
	}
	if err := insert("mp1", "wi", nil, "FINAL_TARGET", "ACTIVE", nil, "WORK_ITEM_LIFECYCLE", "COMPLETED", nil); err != nil {
		t.Fatalf("valid WorkItem monitor rejected: %v", err)
	}
	if err := insert("mp2", "wi", "ti", "CHECKPOINT", "ACTIVE", nil, "TRACKED_ITEM_STEP_COMPLETED", nil, "s1"); err != nil {
		t.Fatalf("valid TrackedItem monitor rejected: %v", err)
	}
	reject("invalid kind", insert("mp3", "wi", nil, "DEPLOYMENT", "ACTIVE", nil, "WORK_ITEM_LIFECYCLE", "COMPLETED", nil))
	reject("invalid state", insert("mp3", "wi", nil, "CHECKPOINT", "OVERDUE", nil, "WORK_ITEM_LIFECYCLE", "COMPLETED", nil))
	reject("derived state AT_RISK", insert("mp3", "wi", nil, "CHECKPOINT", "AT_RISK", nil, "WORK_ITEM_LIFECYCLE", "COMPLETED", nil))
	reject("unknown work item", insert("mp3", "missing", nil, "CHECKPOINT", "ACTIVE", nil, "WORK_ITEM_LIFECYCLE", "COMPLETED", nil))
	reject("unknown subject", insert("mp3", "wi", "missing", "CHECKPOINT", "ACTIVE", nil, "TRACKED_ITEM_STEP_COMPLETED", nil, "s1"))
	reject("subject from another work item", insert("mp3", "wi2", "ti", "CHECKPOINT", "ACTIVE", nil, "TRACKED_ITEM_STEP_COMPLETED", nil, "s1"))
	reject("unknown expected step", insert("mp3", "wi", "ti", "CHECKPOINT", "ACTIVE", nil, "TRACKED_ITEM_STEP_COMPLETED", nil, "missing"))
	reject("work item subject with step expectation", insert("mp3", "wi", nil, "CHECKPOINT", "ACTIVE", nil, "TRACKED_ITEM_STEP_COMPLETED", nil, "s1"))
	reject("tracked subject with lifecycle expectation", insert("mp3", "wi", "ti", "CHECKPOINT", "ACTIVE", nil, "WORK_ITEM_LIFECYCLE", "COMPLETED", nil))
	reject("both expectation fields", insert("mp3", "wi", nil, "CHECKPOINT", "ACTIVE", nil, "WORK_ITEM_LIFECYCLE", "COMPLETED", "s1"))
	reject("neither expectation field", insert("mp3", "wi", nil, "CHECKPOINT", "ACTIVE", nil, "WORK_ITEM_LIFECYCLE", nil, nil))
	reject("invalid expected lifecycle", insert("mp3", "wi", nil, "CHECKPOINT", "ACTIVE", nil, "WORK_ITEM_LIFECYCLE", "DONE", nil))
	reject("SATISFIED without satisfied_at", insert("mp3", "wi", nil, "CHECKPOINT", "SATISFIED", nil, "WORK_ITEM_LIFECYCLE", "COMPLETED", nil))
	reject("satisfied_at while ACTIVE", insert("mp3", "wi", nil, "CHECKPOINT", "ACTIVE", now, "WORK_ITEM_LIFECYCLE", "COMPLETED", nil))
	reject("blank timezone", exec(`UPDATE monitor_points SET timezone = ' ' WHERE entity_id = 'mp1'`))
	reject("second active FINAL_TARGET for one subject", insert("mp3", "wi", nil, "FINAL_TARGET", "ACTIVE", nil, "WORK_ITEM_LIFECYCLE", "COMPLETED", nil))
	if err := insert("mp3", "wi", nil, "FINAL_TARGET", "CANCELLED", nil, "WORK_ITEM_LIFECYCLE", "COMPLETED", nil); err != nil {
		t.Fatalf("a non-active FINAL_TARGET must be allowed: %v", err)
	}
}

func TestAttentionNotificationSchemaConstraints(t *testing.T) {
	db := openMigrationTestDatabase(t)
	now := "2026-09-18T12:00:00Z"
	exec := func(query string, args ...any) error {
		_, err := db.DB.Exec(query, args...)
		return err
	}
	must := func(query string, args ...any) {
		t.Helper()
		if err := exec(query, args...); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	reject := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: schema must reject", name)
		}
	}
	must(`INSERT INTO revisions (id, origin_type, summary, applied_at) VALUES ('rev', 'SYSTEM', 's', ?)`, now)
	for _, id := range []string{"wi", "wi2", "mp", "a1", "a2", "a3", "n1", "n2", "n3"} {
		must(`INSERT INTO entities (id, entity_type, created_at) VALUES (?, 'X', ?)`, id, now)
	}
	for _, id := range []string{"wi", "wi2"} {
		must(`INSERT INTO work_items (entity_id, title, lifecycle_state, last_revision_id, created_at, updated_at) VALUES (?, 't', 'ACTIVE', 'rev', ?, ?)`, id, now, now)
	}
	must(`INSERT INTO monitor_points (entity_id, work_item_id, monitor_kind, title, due_at, timezone, expected_kind, expected_work_item_lifecycle, state, last_revision_id, created_at, updated_at)
		VALUES ('mp', 'wi', 'CHECKPOINT', 't', ?, 'UTC', 'WORK_ITEM_LIFECYCLE', 'COMPLETED', 'ACTIVE', 'rev', ?, ?)`, now, now, now)

	issue := func(id, workItem, issueType, severity, state string, resolved, snoozed any, dedupe, reasonJSON string) error {
		return exec(`INSERT INTO attention_issues (entity_id, work_item_id, monitor_point_id, issue_type, severity, state, reason, reason_json, opened_at, resolved_at, snoozed_until, dedupe_key, last_revision_id, updated_at)
			VALUES (?, ?, 'mp', ?, ?, ?, 'why', ?, ?, ?, ?, ?, 'rev', ?)`, id, workItem, issueType, severity, state, reasonJSON, now, resolved, snoozed, dedupe, now)
	}
	if err := issue("a1", "wi", "MONITOR_OVERDUE_UNMET", "WARNING", "OPEN", nil, nil, "k1", `{"version":1}`); err != nil {
		t.Fatalf("valid OPEN issue rejected: %v", err)
	}
	if err := issue("a2", "wi", "MONITOR_OVERDUE_UNMET", "CRITICAL", "RESOLVED", now, nil, "k1", `{}`); err != nil {
		t.Fatalf("a RESOLVED issue may reuse the dedupe key of a resolved recurrence: %v", err)
	}
	reject("second OPEN issue with the same dedupe key", issue("a3", "wi", "MONITOR_OVERDUE_UNMET", "WARNING", "OPEN", nil, nil, "k1", `{}`))
	reject("unknown issue type", issue("a3", "wi", "DUE_SOON", "WARNING", "OPEN", nil, nil, "k2", `{}`))
	reject("unknown severity", issue("a3", "wi", "MONITOR_OVERDUE_UNMET", "URGENT", "OPEN", nil, nil, "k2", `{}`))
	reject("SNOOZED lifecycle state", issue("a3", "wi", "MONITOR_OVERDUE_UNMET", "WARNING", "SNOOZED", nil, nil, "k2", `{}`))
	reject("DISMISSED lifecycle state", issue("a3", "wi", "MONITOR_OVERDUE_UNMET", "WARNING", "DISMISSED", nil, nil, "k2", `{}`))
	reject("RESOLVED without resolved_at", issue("a3", "wi", "MONITOR_OVERDUE_UNMET", "WARNING", "RESOLVED", nil, nil, "k2", `{}`))
	reject("OPEN with resolved_at", issue("a3", "wi", "MONITOR_OVERDUE_UNMET", "WARNING", "OPEN", now, nil, "k2", `{}`))
	reject("snoozed_until on a RESOLVED issue", issue("a3", "wi", "MONITOR_OVERDUE_UNMET", "WARNING", "RESOLVED", now, now, "k2", `{}`))
	reject("invalid reason_json", issue("a3", "wi", "MONITOR_OVERDUE_UNMET", "WARNING", "OPEN", nil, nil, "k2", `not json`))
	reject("unknown work item", issue("a3", "missing", "MONITOR_OVERDUE_UNMET", "WARNING", "OPEN", nil, nil, "k2", `{}`))
	if err := exec(`UPDATE attention_issues SET snoozed_until = ? WHERE entity_id = 'a1'`, now); err != nil {
		t.Fatalf("snooze is an independent timestamp on an OPEN issue: %v", err)
	}

	notification := func(id, issueID, workItem, state string, sent, read, dismissed any, dedupe, channel, snapshot string) error {
		return exec(`INSERT INTO notifications (entity_id, attention_issue_id, work_item_id, title, message, reason_snapshot_json, state, channel, sent_at, read_at, dismissed_at, dedupe_key, last_revision_id, created_at, updated_at)
			VALUES (?, ?, ?, 't', 'm', ?, ?, ?, ?, ?, ?, ?, 'rev', ?, ?)`, id, issueID, workItem, snapshot, state, channel, sent, read, dismissed, dedupe, now, now)
	}
	if err := notification("n1", "a1", "wi", "PENDING", nil, nil, nil, "d1", "IN_APP", `{"version":1}`); err != nil {
		t.Fatalf("valid notification rejected: %v", err)
	}
	if err := notification("n2", "a1", "wi", "PENDING", nil, now, now, "d2", "IN_APP", `{}`); err != nil {
		t.Fatalf("read_at and dismissed_at are independent interaction timestamps: %v", err)
	}
	reject("duplicate notification dedupe key", notification("n3", "a1", "wi", "PENDING", nil, nil, nil, "d1", "IN_APP", `{}`))
	reject("unknown delivery state", notification("n3", "a1", "wi", "READ", nil, nil, nil, "d3", "IN_APP", `{}`))
	reject("unsupported channel", notification("n3", "a1", "wi", "PENDING", nil, nil, nil, "d3", "EMAIL", `{}`))
	reject("SENT without sent_at", notification("n3", "a1", "wi", "SENT", nil, nil, nil, "d3", "IN_APP", `{}`))
	reject("PENDING with sent_at", notification("n3", "a1", "wi", "PENDING", now, nil, nil, "d3", "IN_APP", `{}`))
	reject("CANCELLED with sent_at", notification("n3", "a1", "wi", "CANCELLED", now, nil, nil, "d3", "IN_APP", `{}`))
	reject("invalid snapshot json", notification("n3", "a1", "wi", "PENDING", nil, nil, nil, "d3", "IN_APP", `nope`))
	reject("unknown attention issue", notification("n3", "missing", "wi", "PENDING", nil, nil, nil, "d3", "IN_APP", `{}`))
	reject("notification work item different from its issue's", notification("n3", "a1", "wi2", "PENDING", nil, nil, nil, "d3", "IN_APP", `{}`))
	if err := notification("n3", "a1", "wi", "CANCELLED", nil, nil, nil, "d3", "IN_APP", `{}`); err != nil {
		t.Fatalf("a CANCELLED notification (never sent) must be accepted: %v", err)
	}
	if err := exec(`UPDATE notifications SET state='SENT', sent_at=? WHERE entity_id='n2'`, now); err != nil {
		t.Fatalf("a SENT notification with sent_at must be accepted: %v", err)
	}

	for _, table := range []string{"attention_issues", "notifications"} {
		rows, err := db.DB.Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			for _, banned := range []string{"next_notification", "next_evaluate", "score", "confidence", "origin_ai", "superseded", "expires"} {
				if strings.Contains(name, banned) {
					t.Fatalf("%s.%s is out of M11 Slice 1 scope", table, name)
				}
			}
		}
		_ = rows.Close()
	}
}

// TestNotificationExternalDeliverySchemaConstraints proves M17 Slice 1's
// delivery-boundary idempotency table: it must reference a real Notification
// (never an arbitrary string), it must accept at most one row per
// Notification (the PK is the idempotency guarantee at the schema level),
// and it must never require or accept anything beyond a bare success
// timestamp - no failure/status/channel columns exist to persist.
func TestNotificationExternalDeliverySchemaConstraints(t *testing.T) {
	db := openMigrationTestDatabase(t)
	now := "2026-09-24T12:00:00Z"
	exec := func(query string, args ...any) error {
		_, err := db.DB.Exec(query, args...)
		return err
	}
	must := func(query string, args ...any) {
		t.Helper()
		if err := exec(query, args...); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	must(`INSERT INTO revisions (id, origin_type, summary, applied_at) VALUES ('rev', 'SYSTEM', 's', ?)`, now)
	for _, id := range []string{"wi", "mp", "a1", "n1"} {
		must(`INSERT INTO entities (id, entity_type, created_at) VALUES (?, 'X', ?)`, id, now)
	}
	must(`INSERT INTO work_items (entity_id, title, lifecycle_state, last_revision_id, created_at, updated_at) VALUES ('wi', 't', 'ACTIVE', 'rev', ?, ?)`, now, now)
	must(`INSERT INTO monitor_points (entity_id, work_item_id, monitor_kind, title, due_at, timezone, expected_kind, expected_work_item_lifecycle, state, last_revision_id, created_at, updated_at)
		VALUES ('mp', 'wi', 'CHECKPOINT', 't', ?, 'UTC', 'WORK_ITEM_LIFECYCLE', 'COMPLETED', 'ACTIVE', 'rev', ?, ?)`, now, now, now)
	must(`INSERT INTO attention_issues (entity_id, work_item_id, monitor_point_id, issue_type, severity, state, reason, reason_json, opened_at, resolved_at, snoozed_until, dedupe_key, last_revision_id, updated_at)
		VALUES ('a1', 'wi', 'mp', 'MONITOR_OVERDUE_UNMET', 'WARNING', 'OPEN', 'why', '{}', ?, NULL, NULL, 'k1', 'rev', ?)`, now, now)
	must(`INSERT INTO notifications (entity_id, attention_issue_id, work_item_id, title, message, reason_snapshot_json, state, channel, sent_at, read_at, dismissed_at, dedupe_key, last_revision_id, created_at, updated_at)
		VALUES ('n1', 'a1', 'wi', 't', 'm', '{}', 'SENT', 'IN_APP', ?, NULL, NULL, 'd1', 'rev', ?, ?)`, now, now, now)

	if err := exec(`INSERT INTO notification_external_deliveries (notification_id, delivered_at) VALUES ('n1', ?)`, now); err != nil {
		t.Fatalf("valid delivery-success row rejected: %v", err)
	}
	if err := exec(`INSERT INTO notification_external_deliveries (notification_id, delivered_at) VALUES ('n1', ?)`, now); err == nil {
		t.Fatal("expected a second success row for the same Notification to be rejected (PK is the idempotency guarantee)")
	}
	if err := exec(`INSERT INTO notification_external_deliveries (notification_id, delivered_at) VALUES ('does-not-exist', ?)`, now); err == nil {
		t.Fatal("expected a foreign key constraint failure for a nonexistent Notification")
	}
	if err := exec(`INSERT INTO notification_external_deliveries (notification_id, delivered_at) VALUES ('n1-blank', ' ')`); err == nil {
		t.Fatal("expected blank delivered_at to be rejected")
	}

	rows, err := db.DB.Query(`SELECT name FROM pragma_table_info('notification_external_deliveries')`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"status", "failed", "attempt", "retry", "channel", "unavailable", "denied"} {
			if strings.Contains(strings.ToLower(name), banned) {
				t.Fatalf("notification_external_deliveries.%s is out of M17 Slice 1 scope (success-marker only, no failure/attempt/channel state)", name)
			}
		}
	}
}

// TestTodoReminderSchemaConstraints proves M18's todo_reminders schema constraints:
// FK to entities and planner_items, unique per planner_item, valid JSON for offsets
// and fired_offsets, state CHECK constraint, and satisfied_at semantics.
func TestTodoReminderSchemaConstraints(t *testing.T) {
	db := openMigrationTestDatabase(t)
	now := "2026-09-25T12:00:00Z"
	exec := func(query string, args ...any) error {
		_, err := db.DB.Exec(query, args...)
		return err
	}
	must := func(query string, args ...any) {
		t.Helper()
		if err := exec(query, args...); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	must(`INSERT INTO revisions (id, origin_type, summary, applied_at) VALUES ('rev', 'SYSTEM', 's', ?)`, now)
	for _, id := range []string{"p1", "p2", "rem1", "rem2"} {
		must(`INSERT INTO entities (id, entity_type, created_at) VALUES (?, 'X', ?)`, id, now)
	}
	must(`INSERT INTO planner_items (entity_id, authority_type, item_type, title, status, context_status, last_revision_id, created_at, updated_at) VALUES ('p1', 'LOCAL', 'TODO', 'Task 1', 'OPEN', 'COMPLETE', 'rev', ?, ?)`, now, now)
	must(`INSERT INTO planner_items (entity_id, authority_type, item_type, title, status, context_status, last_revision_id, created_at, updated_at) VALUES ('p2', 'LOCAL', 'TODO', 'Task 2', 'OPEN', 'COMPLETE', 'rev', ?, ?)`, now, now)

	// Valid ACTIVE reminder
	if err := exec(`INSERT INTO todo_reminders (entity_id, planner_item_id, state, offsets_seconds_json, fired_offsets_json, remind_mode, last_revision_id, created_at, updated_at)
		VALUES ('rem1', 'p1', 'ACTIVE', '[86400, 172800]', '[]', 'BEFORE_DUE', 'rev', ?, ?)`, now, now); err != nil {
		t.Fatalf("valid todo_reminder rejected: %v", err)
	}

	// Unique per planner_item_id
	if err := exec(`INSERT INTO todo_reminders (entity_id, planner_item_id, state, offsets_seconds_json, fired_offsets_json, remind_mode, last_revision_id, created_at, updated_at)
		VALUES ('rem2', 'p1', 'ACTIVE', '[3600]', '[]', 'BEFORE_DUE', 'rev', ?, ?)`, now, now); err == nil {
		t.Fatal("expected duplicate reminder on same planner_item to be rejected")
	}

	// Invalid JSON for offsets
	if err := exec(`INSERT INTO todo_reminders (entity_id, planner_item_id, state, offsets_seconds_json, fired_offsets_json, remind_mode, last_revision_id, created_at, updated_at)
		VALUES ('rem2', 'p2', 'ACTIVE', 'not-json', '[]', 'BEFORE_DUE', 'rev', ?, ?)`, now, now); err == nil {
		t.Fatal("expected invalid offsets_seconds_json to be rejected")
	}

	// Invalid state
	if err := exec(`INSERT INTO todo_reminders (entity_id, planner_item_id, state, offsets_seconds_json, fired_offsets_json, remind_mode, last_revision_id, created_at, updated_at)
		VALUES ('rem2', 'p2', 'SNOOZED', '[]', '[]', 'BEFORE_DUE', 'rev', ?, ?)`, now, now); err == nil {
		t.Fatal("expected invalid state SNOOZED to be rejected")
	}

	// FK violation to nonexistent planner item
	if err := exec(`INSERT INTO todo_reminders (entity_id, planner_item_id, state, offsets_seconds_json, fired_offsets_json, remind_mode, last_revision_id, created_at, updated_at)
		VALUES ('rem2', 'nonexistent', 'ACTIVE', '[]', '[]', 'BEFORE_DUE', 'rev', ?, ?)`, now, now); err == nil {
		t.Fatal("expected nonexistent planner_item_id to be rejected by FK")
	}
}

func TestSeedV2Integrity(t *testing.T) {
	db := openMigrationTestDatabase(t)
	if err := Migrate(db.DB); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}

	seedSQLPath := "../../../docs/lum_harbor_workitem_visual_iteration_seed_v2.sql"
	content, err := os.ReadFile(seedSQLPath)
	if err != nil {
		t.Fatalf("failed to read seed v2 SQL file: %v", err)
	}

	stmts := strings.Split(string(content), ";")
	for i, stmt := range stmts {
		trimmed := strings.TrimSpace(stmt)
		if trimmed == "" {
			continue
		}
		if _, err := db.DB.Exec(trimmed); err != nil {
			t.Fatalf("failed to execute statement #%d [%s...]: %v", i, trimmed[:min(60, len(trimmed))], err)
		}
	}

	rows, err := db.DB.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("failed to run pragma foreign_key_check: %v", err)
	}
	defer rows.Close()

	var fkErrors []string
	for rows.Next() {
		var table, parent string
		var rowid int64
		var fkid int
		if err := rows.Scan(&table, &rowid, &parent, &fkid); err != nil {
			t.Fatalf("failed to scan fk check row: %v", err)
		}
		fkErrors = append(fkErrors, fmt.Sprintf("table: %s, rowid: %d, parent: %s, fkid: %d", table, rowid, parent, fkid))
	}
	if len(fkErrors) > 0 {
		t.Fatalf("foreign key violations found in seed v2:\n%s", strings.Join(fkErrors, "\n"))
	}

	var wiCount, wfCount, tiCount int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM work_items WHERE entity_id LIKE '02b11000-%'`).Scan(&wiCount); err != nil {
		t.Fatal(err)
	}
	if wiCount != 5 {
		t.Fatalf("expected 5 seed work items, got %d", wiCount)
	}

	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM workflows WHERE id LIKE '02b18000-%'`).Scan(&wfCount); err != nil {
		t.Fatal(err)
	}
	if wfCount != 4 {
		t.Fatalf("expected 4 seed workflows (WorkItem 5 has none), got %d", wfCount)
	}

	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM tracked_items WHERE entity_id LIKE '02b12000-%'`).Scan(&tiCount); err != nil {
		t.Fatal(err)
	}
	if tiCount != 26 {
		t.Fatalf("expected 26 seed tracked items, got %d", tiCount)
	}

	// Verify WorkItem 5 has NO workflow and NO tracked items
	var wi5WfCount, wi5TiCount int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM workflows WHERE work_item_id = '02b11000-0005-7000-8000-000000000005'`).Scan(&wi5WfCount); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM tracked_items WHERE work_item_id = '02b11000-0005-7000-8000-000000000005'`).Scan(&wi5TiCount); err != nil {
		t.Fatal(err)
	}
	if wi5WfCount != 0 || wi5TiCount != 0 {
		t.Fatalf("expected WorkItem 5 to have 0 workflows and 0 tracked items, got wf=%d, ti=%d", wi5WfCount, wi5TiCount)
	}

	// Every seeded Workflow owns an explicit current step of its own definition
	// and an actual started_at, deterministic for visual acceptance.
	wantSteps := map[string]string{
		"02b18000-0001-7000-8000-000000000001": "VERIFY",
		"02b18000-0002-7000-8000-000000000002": "TEST",
		"02b18000-0003-7000-8000-000000000003": "MITIGATE",
		"02b18000-0004-7000-8000-000000000004": "GATHER_EVIDENCE",
	}
	for id, key := range wantSteps {
		var gotKey, started string
		if err := db.DB.QueryRow(`SELECT s.step_key, w.started_at FROM workflows w JOIN workflow_steps s ON s.id = w.current_step_id AND s.workflow_definition_id = w.workflow_definition_id WHERE w.id = ?`, id).Scan(&gotKey, &started); err != nil {
			t.Fatalf("seed workflow %s has no valid current step: %v", id, err)
		}
		if gotKey != key || started == "" {
			t.Fatalf("seed workflow %s current step = %q started_at=%q, want %q", id, gotKey, started, key)
		}
	}
	// Decoupling in the benchmark: Payments Workflow is at VERIFY while its items
	// sit at other steps (Workflow position is independent of item positions).
	var otherStepItems int
	if err := db.DB.QueryRow(`SELECT COUNT(DISTINCT st.workflow_step_id) FROM tracked_item_step_states st JOIN tracked_items t ON t.entity_id = st.tracked_item_id WHERE t.workflow_id = '02b18000-0001-7000-8000-000000000001' AND st.state <> 'NOT_STARTED' AND st.workflow_step_id <> '02b19100-0004-7000-8000-000000000004'`).Scan(&otherStepItems); err != nil {
		t.Fatal(err)
	}
	if otherStepItems == 0 {
		t.Fatal("the Payments benchmark must contain item progress at steps other than the Workflow's current step")
	}

	// Verify WorkItem 1 has exactly 1 workflow and 12 tracked items, all with matching workflow_id
	var wi1WfCount, wi1TiCount, wi1MatchingTiCount int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM workflows WHERE work_item_id = '02b11000-0001-7000-8000-000000000001'`).Scan(&wi1WfCount); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM tracked_items WHERE work_item_id = '02b11000-0001-7000-8000-000000000001'`).Scan(&wi1TiCount); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM tracked_items WHERE work_item_id = '02b11000-0001-7000-8000-000000000001' AND workflow_id = '02b18000-0001-7000-8000-000000000001'`).Scan(&wi1MatchingTiCount); err != nil {
		t.Fatal(err)
	}
	if wi1WfCount != 1 || wi1TiCount != 12 || wi1MatchingTiCount != 12 {
		t.Fatalf("expected WorkItem 1 to have 1 workflow and 12 tracked items belonging to that workflow, got wf=%d, ti=%d, matching=%d", wi1WfCount, wi1TiCount, wi1MatchingTiCount)
	}
}

func TestM19_Round2_ProposalAndRevisionSourcesSchema(t *testing.T) {
	db := openMigrationTestDatabase(t)
	now := "2026-09-26T12:00:00Z"

	// Insert prerequisite entity and revision
	entityID := "entity-m19-r2"
	revID := "rev-m19-r2"
	if _, err := db.DB.Exec(`INSERT INTO entities (id, entity_type, created_at) VALUES (?, 'WORK_ITEM', ?)`, entityID, now); err != nil {
		t.Fatalf("insert entity: %v", err)
	}
	if _, err := db.DB.Exec(`INSERT INTO revisions (id, origin_type, summary, applied_at) VALUES (?, 'USER', 'test', ?)`, revID, now); err != nil {
		t.Fatalf("insert revision: %v", err)
	}

	// Insert prerequisite source record and revision
	sourceRecordID := "src-rec-1"
	sourceRevID := "src-rev-1"
	contentHash := strings.Repeat("a", 64)
	if _, err := db.DB.Exec(`
		INSERT INTO source_records (id, extension_id, instance_id, source_kind, external_id, current_revision_id, current_content_hash, first_seen_at, last_seen_at, created_at, updated_at)
		VALUES (?, 'core.local', 'default', 'NOTE', 'ext-1', NULL, NULL, ?, ?, ?, ?)
	`, sourceRecordID, now, now, now, now); err != nil {
		t.Fatalf("insert source record: %v", err)
	}
	if _, err := db.DB.Exec(`
		INSERT INTO source_revisions (id, source_record_id, content_hash, payload_json, observed_at, created_at)
		VALUES (?, ?, ?, '{"text":"test"}', ?, ?)
	`, sourceRevID, sourceRecordID, contentHash, now, now); err != nil {
		t.Fatalf("insert source revision: %v", err)
	}
	if _, err := db.DB.Exec(`
		UPDATE source_records SET current_revision_id = ?, current_content_hash = ? WHERE id = ?
	`, sourceRevID, contentHash, sourceRecordID); err != nil {
		t.Fatalf("update source record head: %v", err)
	}

	// Insert prerequisite AI run
	aiRunID := "ai-run-1"
	if _, err := db.DB.Exec(`
		INSERT INTO ai_runs (id, processing_id, request_id, action, status, canonical_request_json, started_at)
		VALUES (?, 'proc-1', 'req-1', 'source.interpret', 'RUNNING', '{"action":"source.interpret"}', ?)
	`, aiRunID, now); err != nil {
		t.Fatalf("insert ai run: %v", err)
	}

	// 1. Valid proposal with ai_run_id
	proposalID := "prop-m19-r2-1"
	if _, err := db.DB.Exec(`
		INSERT INTO change_proposals (id, target_entity_id, operation_type, title, status, operation_payload_json, ai_run_id, created_at, updated_at)
		VALUES (?, ?, 'work_item.update_details', 'Update Title', 'PENDING', '{"operation_kind":"work_item.update_details","version":"v1","preconditions":{"expected_last_revision_id":"rev-1","expected_title":"T"},"proposed_title":"New T"}', ?, ?, ?)
	`, proposalID, entityID, aiRunID, now, now); err != nil {
		t.Fatalf("insert proposal with ai_run_id: %v", err)
	}

	// 2. Proposal with non-existent ai_run_id must fail foreign key
	if _, err := db.DB.Exec(`
		INSERT INTO change_proposals (id, target_entity_id, operation_type, title, status, operation_payload_json, ai_run_id, created_at, updated_at)
		VALUES ('prop-invalid-airun', ?, 'work_item.update_details', 'Title', 'PENDING', '{}', 'non-existent-ai-run', ?, ?)
	`, entityID, now, now); err == nil {
		t.Fatal("expected foreign key failure for non-existent ai_run_id")
	}

	// 3. Insert into change_proposal_sources
	if _, err := db.DB.Exec(`
		INSERT INTO change_proposal_sources (proposal_id, source_revision_id, role)
		VALUES (?, ?, 'PRIMARY_SOURCE')
	`, proposalID, sourceRevID); err != nil {
		t.Fatalf("insert change_proposal_sources: %v", err)
	}

	// 4. change_proposal_sources with invalid role must fail check
	if _, err := db.DB.Exec(`
		INSERT INTO change_proposal_sources (proposal_id, source_revision_id, role)
		VALUES (?, ?, 'INVALID_ROLE')
	`, proposalID, sourceRevID); err == nil {
		t.Fatal("expected check constraint failure for invalid role in change_proposal_sources")
	}

	// 5. Insert into revision_sources
	if _, err := db.DB.Exec(`
		INSERT INTO revision_sources (revision_id, source_revision_id, role)
		VALUES (?, ?, 'PRIMARY_SOURCE')
	`, revID, sourceRevID); err != nil {
		t.Fatalf("insert revision_sources: %v", err)
	}

	// 6. revision_sources with invalid role must fail check
	if _, err := db.DB.Exec(`
		INSERT INTO revision_sources (revision_id, source_revision_id, role)
		VALUES (?, ?, 'INVALID_ROLE')
	`, revID, sourceRevID); err == nil {
		t.Fatal("expected check constraint failure for invalid role in revision_sources")
	}

	// 7. revision_sources with non-existent revision_id must fail foreign key
	if _, err := db.DB.Exec(`
		INSERT INTO revision_sources (revision_id, source_revision_id, role)
		VALUES ('non-existent-rev', ?, 'PRIMARY_SOURCE')
	`, sourceRevID); err == nil {
		t.Fatal("expected foreign key failure for non-existent revision_id in revision_sources")
	}

	// 8. revision_sources with non-existent source_revision_id must fail foreign key
	if _, err := db.DB.Exec(`
		INSERT INTO revision_sources (revision_id, source_revision_id, role)
		VALUES (?, 'non-existent-source-rev', 'PRIMARY_SOURCE')
	`, revID); err == nil {
		t.Fatal("expected foreign key failure for non-existent source_revision_id in revision_sources")
	}

	// 9. change_proposal_sources with non-existent proposal_id must fail foreign key
	if _, err := db.DB.Exec(`
		INSERT INTO change_proposal_sources (proposal_id, source_revision_id, role)
		VALUES ('non-existent-proposal', ?, 'PRIMARY_SOURCE')
	`, sourceRevID); err == nil {
		t.Fatal("expected foreign key failure for non-existent proposal_id in change_proposal_sources")
	}

	// 10. change_proposal_sources with non-existent source_revision_id must fail foreign key
	if _, err := db.DB.Exec(`
		INSERT INTO change_proposal_sources (proposal_id, source_revision_id, role)
		VALUES (?, 'non-existent-source-rev', 'PRIMARY_SOURCE')
	`, proposalID); err == nil {
		t.Fatal("expected foreign key failure for non-existent source_revision_id in change_proposal_sources")
	}
}

// TestProposalOperationsSchema covers the aggregate Proposal persistence
// foundation: ordered typed child operations under one parent and the generic
// (entity, revision) result rows of an applied child.
func TestProposalOperationsSchema(t *testing.T) {
	db := openMigrationTestDatabase(t)
	now := "2026-10-03T12:00:00Z"
	exec := func(query string, args ...any) error {
		_, err := db.DB.Exec(query, args...)
		return err
	}
	mustExec := func(label, query string, args ...any) {
		t.Helper()
		if err := exec(query, args...); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	mustFail := func(label, query string, args ...any) {
		t.Helper()
		if err := exec(query, args...); err == nil {
			t.Fatalf("%s: expected constraint failure", label)
		}
	}
	aggregatePayload := `{"operation_kind":"proposal.aggregate","version":"v1"}`
	childPayload := `{"operation_kind":"tracked_item.create","version":"v1","work_item_id":"$op[0].work_item_id","title":"API"}`

	for _, entity := range []string{"entity-wi", "entity-todo", "entity-reminder"} {
		mustExec("insert entity", `INSERT INTO entities (id, entity_type, created_at) VALUES (?, 'WORK_ITEM', ?)`, entity, now)
	}
	for _, rev := range []string{"rev-wi", "rev-wf", "rev-todo"} {
		mustExec("insert revision", `INSERT INTO revisions (id, origin_type, applied_at) VALUES (?, 'USER', ?)`, rev, now)
	}

	insertParent := `INSERT INTO change_proposals (id, target_entity_id, operation_type, title, status, operation_payload_json, applied_revision_id, created_at, updated_at, resolved_at)
		VALUES (?, ?, ?, 'Plan', ?, ?, ?, ?, ?, ?)`

	// Parent compatibility: an aggregate may have a NULL target; singular
	// non-root-create operations still may not.
	mustExec("aggregate parent with NULL target", insertParent, "prop-agg", nil, "proposal.aggregate", "PENDING", aggregatePayload, nil, now, now, nil)
	mustFail("singular non-root-create with NULL target", insertParent, "prop-bad-null", nil, "tracked_item.update", "PENDING", `{}`, nil, now, now, nil)
	// APPLIED keeps the single compatibility primary pointer invariant.
	mustFail("APPLIED aggregate without applied_revision_id", insertParent, "prop-agg-applied-null", nil, "proposal.aggregate", "APPLIED", aggregatePayload, nil, now, now, now)
	mustExec("APPLIED aggregate with primary revision", insertParent, "prop-agg-applied", nil, "proposal.aggregate", "APPLIED", aggregatePayload, "rev-wi", now, now, now)

	insertOp := `INSERT INTO proposal_operations (id, proposal_id, sequence_no, operation_type, target_entity_id, operation_payload_json) VALUES (?, ?, ?, ?, ?, ?)`
	mustExec("child 0 root create", insertOp, "op-0", "prop-agg", 0, "work_item.create", nil, `{"operation_kind":"work_item.create","version":"v1","title":"A"}`)
	mustExec("child 1 symbolic payload", insertOp, "op-1", "prop-agg", 1, "tracked_item.create", nil, childPayload)
	mustExec("child 2 concrete target", insertOp, "op-2", "prop-agg", 2, "workitem.set_lifecycle", "entity-wi", `{}`)

	mustFail("unknown parent", insertOp, "op-x1", "missing-proposal", 0, "work_item.create", nil, `{}`)
	mustFail("unknown concrete target", insertOp, "op-x2", "prop-agg", 3, "workitem.set_lifecycle", "missing-entity", `{}`)
	mustFail("duplicate sequence within parent", insertOp, "op-x3", "prop-agg", 1, "work_item.create", nil, `{}`)
	mustFail("negative sequence", insertOp, "op-x4", "prop-agg", -1, "work_item.create", nil, `{}`)
	mustFail("malformed payload JSON", insertOp, "op-x5", "prop-agg", 4, "work_item.create", nil, `{malformed`)
	mustFail("nested aggregate child", insertOp, "op-x6", "prop-agg", 5, "proposal.aggregate", nil, `{}`)
	mustFail("blank child operation type", insertOp, "op-x7", "prop-agg", 6, "  ", nil, `{}`)
	mustFail("blank child ID", insertOp, " ", "prop-agg", 7, "work_item.create", nil, `{}`)
	// The same sequence number is legal under a different parent.
	mustExec("sequence 0 under another parent", insertOp, "op-other-0", "prop-agg-applied", 0, "work_item.create", nil, `{}`)

	var payload string
	if err := db.DB.QueryRow(`SELECT operation_payload_json FROM proposal_operations WHERE id = 'op-1'`).Scan(&payload); err != nil || payload != childPayload {
		t.Fatalf("child payload must round-trip verbatim: %q %v", payload, err)
	}

	insertResult := `INSERT INTO proposal_operation_results (proposal_operation_id, sequence_no, entity_id, revision_id, result_role) VALUES (?, ?, ?, ?, ?)`
	// One child, several Revisions; a Revision with no Entity (Workflow).
	mustExec("result entity+revision", insertResult, "op-0", 0, "entity-wi", "rev-wi", "PRIMARY")
	mustExec("result revision without entity", insertResult, "op-0", 1, nil, "rev-wf", "SECONDARY")
	// One Revision covering several Entities (Todo + Reminder).
	mustExec("result todo", insertResult, "op-1", 0, "entity-todo", "rev-todo", "PRIMARY")
	mustExec("result reminder same revision", insertResult, "op-1", 1, "entity-reminder", "rev-todo", "SECONDARY")

	mustFail("second PRIMARY for one child", insertResult, "op-0", 2, nil, "rev-todo", "PRIMARY")
	mustFail("duplicate result sequence", insertResult, "op-0", 1, nil, "rev-todo", "SECONDARY")
	mustFail("unknown revision", insertResult, "op-2", 0, nil, "missing-revision", "PRIMARY")
	mustFail("NULL revision", insertResult, "op-2", 0, "entity-wi", nil, "PRIMARY")
	mustFail("unknown entity", insertResult, "op-2", 0, "missing-entity", "rev-wi", "PRIMARY")
	mustFail("unknown child operation", insertResult, "missing-op", 0, nil, "rev-wi", "PRIMARY")
	mustFail("domain role is not a result role", insertResult, "op-2", 0, nil, "rev-wi", "WORK_ITEM")
	mustFail("negative result sequence", insertResult, "op-2", -1, nil, "rev-wi", "PRIMARY")

	// No cascade: a parent with children, or a child with results, cannot be
	// deleted out from under its durable review/result record.
	mustFail("delete parent with children", `DELETE FROM change_proposals WHERE id = 'prop-agg'`)
	mustFail("delete child with results", `DELETE FROM proposal_operations WHERE id = 'op-0'`)

	var count int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM proposal_operation_results WHERE revision_id = 'rev-todo'`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("expected two result rows sharing one Revision, got %d %v", count, err)
	}
}
