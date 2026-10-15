package tracking

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/workitem"
)

var due = time.Date(2026, 10, 15, 17, 0, 0, 0, time.UTC)

func lifecyclePtr(l workitem.LifecycleState) *workitem.LifecycleState { return &l }

func TestGetWorkItemTrackingDistinguishesEmptyFromUnavailable(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Release")
	empty, err := f.tracking.GetWorkItemTracking(f.ctx, wi.ID)
	if err != nil || empty.WorkItemID != wi.ID || len(empty.Items) != 0 || empty.Workflow != nil || len(empty.MonitorPoints) != 0 {
		t.Fatalf("a valid WorkItem with no tracking is a valid empty model: %+v err=%v", empty, err)
	}
	if _, err := f.tracking.GetWorkItemTracking(f.ctx, "unknown"); !errors.Is(err, ErrWorkItemUnavailable) {
		t.Fatalf("unknown WorkItem must be unavailable, got %v", err)
	}
	archived := f.workItem(t, "Archived")
	if _, err := f.db.Exec("UPDATE entities SET archived_at = ? WHERE id = ?", time.Now().UTC().Format(time.RFC3339Nano), archived.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.GetWorkItemTracking(f.ctx, archived.ID); !errors.Is(err, ErrWorkItemUnavailable) {
		t.Fatalf("archived WorkItem must be unavailable, got %v", err)
	}
}

func TestCreateWorkItemLevelMonitorPoint(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Release")
	revisions, entities := f.count(t, "SELECT COUNT(*) FROM revisions"), f.count(t, "SELECT COUNT(*) FROM entities")
	proposals := f.count(t, "SELECT COUNT(*) FROM change_proposals")

	point, err := f.tracking.CreateMonitorPoint(f.ctx, CreateMonitorPointInput{
		WorkItemID: wi.ID, Kind: MonitorFinalTarget, Title: "  Ship it  ", DueAt: due.In(time.FixedZone("x", 3600)),
		Timezone: "America/Chicago", ExpectedWorkItemLifecycle: lifecyclePtr(workitem.LifecycleCompleted),
	})
	if err != nil {
		t.Fatal(err)
	}
	if id, err := uuid.Parse(point.ID); err != nil || id.Version() != 7 {
		t.Fatalf("expected UUIDv7 identity: %q", point.ID)
	}
	if point.Subject != (MonitorSubject{Kind: SubjectWorkItem, ID: wi.ID}) || point.Expected.Kind != ExpectWorkItemLifecycle ||
		point.Expected.WorkItemState == nil || *point.Expected.WorkItemState != workitem.LifecycleCompleted || point.Expected.StepID != nil {
		t.Fatalf("unexpected point: %+v", point)
	}
	if point.Timezone != "America/Chicago" || !point.DueAt.Equal(due) {
		t.Fatalf("timezone / due mismatch: %+v", point)
	}
	if f.count(t, "SELECT COUNT(*) FROM revisions") != revisions+1 || f.count(t, "SELECT COUNT(*) FROM entities") != entities+1 {
		t.Fatal("expected one Revision and one Entity")
	}
	var entityType, origin string
	if err := f.db.QueryRow("SELECT entity_type FROM entities WHERE id = ?", point.ID).Scan(&entityType); err != nil || entityType != "MONITOR_POINT" {
		t.Fatalf("entity type %q err=%v", entityType, err)
	}
	if err := f.db.QueryRow("SELECT origin_type FROM revisions WHERE id = ?", point.LastRevisionID).Scan(&origin); err != nil || origin != "USER" {
		t.Fatalf("origin %q err=%v", origin, err)
	}
	got, _ := f.workItems.GetByID(f.ctx, wi.ID)
	if got.LastRevisionID != wi.LastRevisionID || got.Lifecycle != workitem.LifecycleActive {
		t.Fatalf("WorkItem must be untouched: %+v err=%v", got, err)
	}
	if f.count(t, "SELECT COUNT(*) FROM change_proposals") != proposals {
		t.Fatal("a direct Monitor command must not create a Proposal")
	}
}

func TestCreateTrackedItemLevelMonitorPointAndNoAutomaticSatisfaction(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Release")
	def := f.workflow(t, "Release", 1, "plan", "ship")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	item, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "API"})
	point, err := f.tracking.CreateMonitorPoint(f.ctx, CreateMonitorPointInput{
		WorkItemID: wi.ID, SubjectTrackedItemID: &item.ID, Kind: MonitorCheckpoint, Title: "Ship API", DueAt: due,
		Timezone: "UTC", ExpectedStepID: &def.Steps[1].ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if point.Subject != (MonitorSubject{Kind: SubjectTrackedItem, ID: item.ID}) || point.Expected.Kind != ExpectStepCompleted ||
		*point.Expected.StepID != def.Steps[1].ID || point.Expected.WorkItemState != nil {
		t.Fatalf("unexpected: %+v", point)
	}
	// Reaching the expected condition does not auto-satisfy: no evaluator exists.
	for _, step := range def.Steps {
		if _, err := f.tracking.SetTrackedItemStepState(f.ctx, SetStepStateInput{TrackedItemID: item.ID, WorkflowStepID: step.ID, State: StepCompleted}); err != nil {
			t.Fatal(err)
		}
	}
	view, err := f.tracking.GetWorkItemTracking(f.ctx, wi.ID)
	if err != nil || len(view.MonitorPoints) != 1 || view.MonitorPoints[0].State != MonitorActive || view.MonitorPoints[0].SatisfiedAt != nil {
		t.Fatalf("MonitorPoint must stay ACTIVE (no automatic evaluation): %+v err=%v", view.MonitorPoints, err)
	}
}

func TestCreateMonitorPointRejectionsLeaveNothingBehind(t *testing.T) {
	f := newFixture(t)
	a, b := f.workItem(t, "A"), f.workItem(t, "B")
	def := f.workflow(t, "Release", 1, "plan")
	other := f.workflow(t, "Other", 1, "x")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: a.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: b.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	bound, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: a.ID, Title: "bound"})
	foreign, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: b.ID, Title: "foreign"})
	missing := "missing"
	completed := lifecyclePtr(workitem.LifecycleCompleted)
	bogus := lifecyclePtr("DONE")
	step := def.Steps[0].ID
	otherStep := other.Steps[0].ID

	revisions, entities, monitors := f.count(t, "SELECT COUNT(*) FROM revisions"), f.count(t, "SELECT COUNT(*) FROM entities"), f.count(t, "SELECT COUNT(*) FROM monitor_points")
	base := CreateMonitorPointInput{WorkItemID: a.ID, Kind: MonitorDeadline, Title: "t", DueAt: due, Timezone: "UTC", ExpectedWorkItemLifecycle: completed}
	mut := func(f func(*CreateMonitorPointInput)) CreateMonitorPointInput { in := base; f(&in); return in }
	cases := []struct {
		name  string
		input CreateMonitorPointInput
		want  error
	}{
		{"invalid kind", mut(func(i *CreateMonitorPointInput) { i.Kind = "ALARM" }), ErrInvalidInput},
		{"blank title", mut(func(i *CreateMonitorPointInput) { i.Title = " " }), ErrInvalidInput},
		{"blank timezone", mut(func(i *CreateMonitorPointInput) { i.Timezone = " " }), ErrInvalidInput},
		{"zero due_at", mut(func(i *CreateMonitorPointInput) { i.DueAt = time.Time{} }), ErrInvalidInput},
		{"work-item subject with step", mut(func(i *CreateMonitorPointInput) { i.ExpectedStepID = &step }), ErrInvalidInput},
		{"work-item subject without lifecycle", mut(func(i *CreateMonitorPointInput) { i.ExpectedWorkItemLifecycle = nil }), ErrInvalidInput},
		{"invalid lifecycle", mut(func(i *CreateMonitorPointInput) { i.ExpectedWorkItemLifecycle = bogus }), ErrInvalidInput},
		{"tracked subject without step", mut(func(i *CreateMonitorPointInput) {
			i.SubjectTrackedItemID = &bound.ID
			i.ExpectedWorkItemLifecycle = nil
		}), ErrInvalidInput},
		{"tracked subject with lifecycle", mut(func(i *CreateMonitorPointInput) { i.SubjectTrackedItemID = &bound.ID; i.ExpectedStepID = &step }), ErrInvalidInput},
		{"unknown subject", mut(func(i *CreateMonitorPointInput) {
			i.SubjectTrackedItemID = &missing
			i.ExpectedWorkItemLifecycle = nil
			i.ExpectedStepID = &step
		}), ErrInvalidParent},
		{"cross-WorkItem subject", mut(func(i *CreateMonitorPointInput) {
			i.SubjectTrackedItemID = &foreign.ID
			i.ExpectedWorkItemLifecycle = nil
			i.ExpectedStepID = &step
		}), ErrInvalidParent},
		{"wrong-workflow step", mut(func(i *CreateMonitorPointInput) {
			i.SubjectTrackedItemID = &bound.ID
			i.ExpectedWorkItemLifecycle = nil
			i.ExpectedStepID = &otherStep
		}), ErrStepNotInWorkflow},
		{"unknown step", mut(func(i *CreateMonitorPointInput) {
			i.SubjectTrackedItemID = &bound.ID
			i.ExpectedWorkItemLifecycle = nil
			i.ExpectedStepID = &missing
		}), ErrStepNotInWorkflow},
	}
	for _, tc := range cases {
		if _, err := f.tracking.CreateMonitorPoint(f.ctx, tc.input); !errors.Is(err, tc.want) {
			t.Fatalf("%s: expected %v, got %v", tc.name, tc.want, err)
		}
	}
	if f.count(t, "SELECT COUNT(*) FROM revisions") != revisions || f.count(t, "SELECT COUNT(*) FROM entities") != entities || f.count(t, "SELECT COUNT(*) FROM monitor_points") != monitors {
		t.Fatal("rejected creations left orphan Entity/Revision/monitor rows")
	}
}

func TestFinalTargetInvariant(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Release")
	def := f.workflow(t, "Release", 1, "plan")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	item, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "API"})
	workItemTarget := func() (*MonitorPoint, error) {
		return f.tracking.CreateMonitorPoint(f.ctx, CreateMonitorPointInput{WorkItemID: wi.ID, Kind: MonitorFinalTarget, Title: "t", DueAt: due, Timezone: "UTC", ExpectedWorkItemLifecycle: lifecyclePtr(workitem.LifecycleCompleted)})
	}
	itemTarget := func() (*MonitorPoint, error) {
		return f.tracking.CreateMonitorPoint(f.ctx, CreateMonitorPointInput{WorkItemID: wi.ID, SubjectTrackedItemID: &item.ID, Kind: MonitorFinalTarget, Title: "t", DueAt: due, Timezone: "UTC", ExpectedStepID: &def.Steps[0].ID})
	}
	first, err := workItemTarget()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workItemTarget(); !errors.Is(err, ErrFinalTargetExists) {
		t.Fatalf("second active FINAL_TARGET on the same subject must be rejected, got %v", err)
	}
	if _, err := itemTarget(); err != nil {
		t.Fatalf("a different subject may have its own final target: %v", err)
	}
	if _, err := itemTarget(); !errors.Is(err, ErrFinalTargetExists) {
		t.Fatalf("got %v", err)
	}
	// Non-final kinds are unrestricted.
	for i := 0; i < 2; i++ {
		if _, err := f.tracking.CreateMonitorPoint(f.ctx, CreateMonitorPointInput{WorkItemID: wi.ID, Kind: MonitorCheckpoint, Title: "c", DueAt: due, Timezone: "UTC", ExpectedWorkItemLifecycle: lifecyclePtr(workitem.LifecycleActive)}); err != nil {
			t.Fatal(err)
		}
	}
	// Once the target is no longer ACTIVE a new one is allowed.
	if _, err := f.tracking.SetMonitorPointState(f.ctx, SetMonitorPointStateInput{ID: first.ID, Target: MonitorCancelled}); err != nil {
		t.Fatal(err)
	}
	if _, err := workItemTarget(); err != nil {
		t.Fatalf("a cancelled final target must free the slot: %v", err)
	}
	// The schema independently protects the invariant.
	if _, err := f.db.Exec(`INSERT INTO entities (id, entity_type, created_at) VALUES ('dup', 'MONITOR_POINT', ?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO monitor_points SELECT ?, work_item_id, subject_tracked_item_id, monitor_kind, title, due_at, timezone, expected_kind, expected_work_item_lifecycle, expected_step_id, state, satisfied_at, last_revision_id, created_at, updated_at FROM monitor_points WHERE monitor_kind='FINAL_TARGET' AND state='ACTIVE' AND subject_tracked_item_id IS NULL`, "dup"); err == nil {
		t.Fatal("schema must reject a second active FINAL_TARGET for one subject")
	}
}

func TestSetMonitorPointState(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Release")
	create := func() *MonitorPoint {
		p, err := f.tracking.CreateMonitorPoint(f.ctx, CreateMonitorPointInput{WorkItemID: wi.ID, Kind: MonitorDeadline, Title: "d", DueAt: due, Timezone: "UTC", ExpectedWorkItemLifecycle: lifecyclePtr(workitem.LifecycleCompleted)})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	point := create()
	revisions := f.count(t, "SELECT COUNT(*) FROM revisions")

	same, err := f.tracking.SetMonitorPointState(f.ctx, SetMonitorPointStateInput{ID: point.ID, Target: MonitorActive})
	if err != nil || same.State != MonitorActive || same.LastRevisionID != point.LastRevisionID || !same.UpdatedAt.Equal(point.UpdatedAt) || f.count(t, "SELECT COUNT(*) FROM revisions") != revisions {
		t.Fatalf("same-state must be a no-op: %+v err=%v", same, err)
	}
	done, err := f.tracking.SetMonitorPointState(f.ctx, SetMonitorPointStateInput{ID: point.ID, Target: MonitorSatisfied})
	if err != nil || done.State != MonitorSatisfied || done.SatisfiedAt == nil || done.LastRevisionID == point.LastRevisionID || f.count(t, "SELECT COUNT(*) FROM revisions") != revisions+1 {
		t.Fatalf("satisfy: %+v err=%v", done, err)
	}
	if _, err := f.tracking.SetMonitorPointState(f.ctx, SetMonitorPointStateInput{ID: point.ID, Target: MonitorCancelled}); !errors.Is(err, ErrInvalidMonitorState) {
		t.Fatalf("terminal states cannot change: %v", err)
	}
	if _, err := f.tracking.SetMonitorPointState(f.ctx, SetMonitorPointStateInput{ID: point.ID, Target: MonitorActive}); !errors.Is(err, ErrInvalidMonitorState) {
		t.Fatalf("no reactivation in V1: %v", err)
	}
	cancelled := create()
	c, err := f.tracking.SetMonitorPointState(f.ctx, SetMonitorPointStateInput{ID: cancelled.ID, Target: MonitorCancelled})
	if err != nil || c.State != MonitorCancelled || c.SatisfiedAt != nil {
		t.Fatalf("cancel: %+v err=%v", c, err)
	}
	for name, in := range map[string]SetMonitorPointStateInput{
		"unknown": {ID: "nope", Target: MonitorSatisfied}, "blank": {ID: " ", Target: MonitorSatisfied}, "invalid": {ID: point.ID, Target: "DONE"},
	} {
		if _, err := f.tracking.SetMonitorPointState(f.ctx, in); err == nil {
			t.Fatalf("%s must be rejected", name)
		}
	}
	got, _ := f.workItems.GetByID(f.ctx, wi.ID)
	if got.LastRevisionID != wi.LastRevisionID || got.Lifecycle != workitem.LifecycleActive {
		t.Fatal("Monitor state must not touch the WorkItem")
	}
}

func TestProjectionCombinesRealTrackingAndMonitorTruth(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Release")
	def := f.workflow(t, "Release", 3, "plan", "build", "ship")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	root, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "root"})
	child, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "child", ParentItemID: &root.ID})
	sibling, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "sibling", ParentItemID: &root.ID})
	set := func(item *TrackedItem, step int, state StepState) {
		if _, err := f.tracking.SetTrackedItemStepState(f.ctx, SetStepStateInput{TrackedItemID: item.ID, WorkflowStepID: def.Steps[step].ID, State: state}); err != nil {
			t.Fatal(err)
		}
	}
	set(root, 0, StepCompleted)
	set(root, 1, StepInProgress)
	set(child, 0, StepSkipped)
	set(child, 1, StepBlocked)
	late, err := f.tracking.CreateMonitorPoint(f.ctx, CreateMonitorPointInput{WorkItemID: wi.ID, Kind: MonitorFinalTarget, Title: "late", DueAt: due.Add(24 * time.Hour), Timezone: "UTC", ExpectedWorkItemLifecycle: lifecyclePtr(workitem.LifecycleCompleted)})
	if err != nil {
		t.Fatal(err)
	}
	early, err := f.tracking.CreateMonitorPoint(f.ctx, CreateMonitorPointInput{WorkItemID: wi.ID, SubjectTrackedItemID: &child.ID, Kind: MonitorCheckpoint, Title: "early", DueAt: due, Timezone: "Asia/Tokyo", ExpectedStepID: &def.Steps[2].ID})
	if err != nil {
		t.Fatal(err)
	}
	// A second WorkItem's data must never leak into the projection.
	other := f.workItem(t, "Other")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: other.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: other.ID, Title: "leak"}); err != nil {
		t.Fatal(err)
	}

	for run := 0; run < 2; run++ {
		view, err := f.tracking.GetWorkItemTracking(f.ctx, wi.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(view.Items) != 3 || view.Items[0].Item.ID != root.ID || view.Items[1].Item.ID != child.ID || view.Items[2].Item.ID != sibling.ID {
			t.Fatalf("items/hierarchy order wrong: %+v", view.Items)
		}
		if *view.Items[1].Item.ParentItemID != root.ID || *view.Items[2].Item.ParentItemID != root.ID {
			t.Fatal("parent identity lost")
		}
		if view.Workflow == nil || view.Workflow.Definition.ID != def.ID || view.Workflow.Definition.Version != 3 || len(view.Workflow.Definition.Steps) != 3 || view.Workflow.Definition.Steps[2].Key != "ship" {
			t.Fatalf("workflow not pinned/ordered: %+v", view.Workflow)
		}
		rootP, childP := view.Items[0].Progression, view.Items[1].Progression
		if rootP.PassedCount != 1 || *rootP.CurrentStepKey != "build" || *rootP.CurrentState != StepInProgress {
			t.Fatalf("root progression: %+v", rootP)
		}
		if childP.PassedCount != 1 || childP.SkippedCount != 1 || *childP.CurrentState != StepBlocked {
			t.Fatalf("child progression: %+v", childP)
		}
		if view.Items[2].Progression == nil || view.Items[2].Progression.PassedCount != 0 {
			t.Fatal("sibling should have initial progression")
		}
		if len(view.MonitorPoints) != 2 || view.MonitorPoints[0].ID != early.ID || view.MonitorPoints[1].ID != late.ID {
			t.Fatalf("monitor points must be ordered by due time: %+v", view.MonitorPoints)
		}
		if view.MonitorPoints[0].Subject.ID != child.ID || view.MonitorPoints[0].Timezone != "Asia/Tokyo" || view.MonitorPoints[1].Subject.Kind != SubjectWorkItem {
			t.Fatalf("monitor subjects wrong: %+v", view.MonitorPoints)
		}
	}
}

func TestMonitorPointsSurviveReopeningTheFileBackedDatabase(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Release")
	point, err := f.tracking.CreateMonitorPoint(f.ctx, CreateMonitorPointInput{WorkItemID: wi.ID, Kind: MonitorReview, Title: "r", DueAt: due, Timezone: "UTC", ExpectedWorkItemLifecycle: lifecyclePtr(workitem.LifecycleCompleted)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.SetMonitorPointState(f.ctx, SetMonitorPointStateInput{ID: point.ID, Target: MonitorSatisfied}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.DB.Close() })
	view, err := NewService(reopened.DB, workitem.NewService(reopened.DB, workitem.NewRepository(reopened.DB))).GetWorkItemTracking(f.ctx, wi.ID)
	if err != nil || len(view.MonitorPoints) != 1 {
		t.Fatalf("view: %+v err=%v", view, err)
	}
	got := view.MonitorPoints[0]
	if got.ID != point.ID || got.State != MonitorSatisfied || got.SatisfiedAt == nil || !got.DueAt.Equal(due) || got.Kind != MonitorReview {
		t.Fatalf("monitor truth changed across reopen: %+v", got)
	}
}

func TestListActiveWorkflowDefinitions(t *testing.T) {
	f := newFixture(t)
	f.workflow(t, "B", 1, "x")
	f.workflow(t, "A", 2, "a", "b")
	f.workflow(t, "A", 1, "a")
	inactive := f.workflow(t, "Old", 1, "z")
	if _, err := f.db.Exec("UPDATE workflow_definitions SET active = 0 WHERE id = ?", inactive.ID); err != nil {
		t.Fatal(err)
	}
	defs, err := f.tracking.ListActiveWorkflowDefinitions(f.ctx)
	if err != nil || len(defs) != 3 {
		t.Fatalf("defs: %+v err=%v", defs, err)
	}
	if defs[0].Name != "A" || defs[0].Version != 1 || defs[1].Version != 2 || len(defs[1].Steps) != 2 || defs[1].Steps[1].Key != "b" || defs[2].Name != "B" {
		t.Fatalf("unexpected order/steps: %+v", defs)
	}
}

// M11 owns Attention/Notification tables separately; monitor_points itself must
// never carry evaluation, scheduling or derived state.
func TestMonitorPointsCarryNoEvaluationOrSchedulingState(t *testing.T) {
	f := newFixture(t)
	cols, err := f.db.Query("SELECT name FROM pragma_table_info('monitor_points')")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cols.Close() }()
	for cols.Next() {
		var name string
		_ = cols.Scan(&name)
		for _, banned := range []string{"lead", "repeat", "next_evaluate", "notification", "overdue", "risk", "current"} {
			if contains(name, banned) {
				t.Fatalf("monitor_points.%s introduces evaluation/scheduling/derived state", name)
			}
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
