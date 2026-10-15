package tracking

import (
	"context"
	"database/sql"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/workitem"
)

type fixture struct {
	ctx       context.Context
	db        *sql.DB
	workItems *workitem.Service
	tracking  *Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	database := testsupport.OpenTestDatabase(t)
	workItems := workitem.NewService(database.DB, workitem.NewRepository(database.DB))
	return &fixture{ctx: context.Background(), db: database.DB, workItems: workItems, tracking: NewService(database.DB, workItems)}
}

func (f *fixture) workItem(t *testing.T, title string) *workitem.WorkItem {
	t.Helper()
	item, err := f.workItems.CreateLocalWorkItem(f.ctx, workitem.CreateLocalWorkItemInput{Title: title})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func (f *fixture) workflow(t *testing.T, name string, version int, keys ...string) *WorkflowDefinition {
	t.Helper()
	steps := make([]WorkflowStepInput, len(keys))
	for i, key := range keys {
		steps[i] = WorkflowStepInput{Key: key, Title: strings.ToUpper(key), Terminal: i == len(keys)-1}
	}
	def, err := f.tracking.CreateWorkflowDefinition(f.ctx, CreateWorkflowDefinitionInput{Name: name, Version: version, Steps: steps})
	if err != nil {
		t.Fatal(err)
	}
	return def
}

func (f *fixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.ctx, query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateWorkflowDefinitionPersistsOrderedSteps(t *testing.T) {
	f := newFixture(t)
	def := f.workflow(t, "  Release  ", 1, "plan", "build", "ship")
	if def.Name != "Release" || def.Version != 1 || !def.Active || len(def.Steps) != 3 {
		t.Fatalf("unexpected definition: %+v", def)
	}
	if id, err := uuid.Parse(def.ID); err != nil || id.Version() != 7 {
		t.Fatalf("expected UUIDv7 definition ID, got %q", def.ID)
	}
	loaded, err := f.tracking.GetWorkflowDefinition(f.ctx, def.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i, key := range []string{"plan", "build", "ship"} {
		step := loaded.Steps[i]
		if step.Key != key || step.Sequence != i+1 || step.Terminal != (i == 2) || step.WorkflowDefinitionID != def.ID {
			t.Fatalf("step %d unexpected: %+v", i, step)
		}
		if step.ID != def.Steps[i].ID {
			t.Fatal("step identity must be stable")
		}
	}
	if f.count(t, "SELECT COUNT(*) FROM revisions") != 0 || f.count(t, "SELECT COUNT(*) FROM entities") != 0 {
		t.Fatal("definitions are configuration: no Entity, no Revision")
	}
}

func TestCreateWorkflowDefinitionValidation(t *testing.T) {
	f := newFixture(t)
	ok := []WorkflowStepInput{{Key: "a", Title: "A"}}
	cases := map[string]CreateWorkflowDefinitionInput{
		"blank name":       {Name: "  ", Version: 1, Steps: ok},
		"zero version":     {Name: "n", Version: 0, Steps: ok},
		"negative version": {Name: "n", Version: -1, Steps: ok},
		"zero steps":       {Name: "n", Version: 1},
		"blank key":        {Name: "n", Version: 1, Steps: []WorkflowStepInput{{Key: " ", Title: "A"}}},
		"blank title":      {Name: "n", Version: 1, Steps: []WorkflowStepInput{{Key: "a", Title: " "}}},
		"duplicate keys":   {Name: "n", Version: 1, Steps: []WorkflowStepInput{{Key: "a", Title: "A"}, {Key: " a ", Title: "B"}}},
	}
	for name, input := range cases {
		if _, err := f.tracking.CreateWorkflowDefinition(f.ctx, input); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s: expected ErrInvalidInput, got %v", name, err)
		}
	}
	if f.count(t, "SELECT COUNT(*) FROM workflow_definitions") != 0 || f.count(t, "SELECT COUNT(*) FROM workflow_steps") != 0 {
		t.Fatal("invalid definitions must leave nothing behind")
	}
}

func TestDuplicateNameVersionRollsBackTheWholeDefinition(t *testing.T) {
	f := newFixture(t)
	f.workflow(t, "Release", 1, "a", "b")
	_, err := f.tracking.CreateWorkflowDefinition(f.ctx, CreateWorkflowDefinitionInput{
		Name: "Release", Version: 1, Steps: []WorkflowStepInput{{Key: "x", Title: "X"}, {Key: "y", Title: "Y"}},
	})
	if err == nil {
		t.Fatal("duplicate (name, version) must fail")
	}
	if f.count(t, "SELECT COUNT(*) FROM workflow_definitions") != 1 || f.count(t, "SELECT COUNT(*) FROM workflow_steps") != 2 {
		t.Fatal("failed creation must not leave a partial definition or steps")
	}
	if _, err := f.tracking.CreateWorkflowDefinition(f.ctx, CreateWorkflowDefinitionInput{
		Name: "Release", Version: 2, Steps: []WorkflowStepInput{{Key: "x", Title: "X"}},
	}); err != nil {
		t.Fatalf("a new version of the same process must be allowed: %v", err)
	}
}

func TestCreateTrackedItemWithoutWorkflowFails(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Release")
	itemType := " service "
	_, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "  API  ", ItemType: &itemType})
	if !errors.Is(err, ErrNoWorkflow) {
		t.Fatalf("expected ErrNoWorkflow, got %v", err)
	}
	if f.count(t, "SELECT COUNT(*) FROM tracked_items") != 0 {
		t.Fatal("no tracked item should be created")
	}
}

func TestCreateWorkflowRejections(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "One")
	def := f.workflow(t, "Release", 1, "a")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	// Cannot create second workflow for same work item
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); !errors.Is(err, ErrWorkflowAlreadyExists) {
		t.Fatalf("expected ErrWorkflowAlreadyExists, got %v", err)
	}
}

func TestCreateTrackedItemWithWorkflowCreatesAtomicNotStartedStatesUnderOneRevision(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Release")
	def := f.workflow(t, "Release", 1, "plan", "build", "ship")
	wf, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID})
	if err != nil {
		t.Fatal(err)
	}
	revisionsBefore := f.count(t, "SELECT COUNT(*) FROM revisions")
	entitiesBefore := f.count(t, "SELECT COUNT(*) FROM entities")

	item, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "API"})
	if err != nil {
		t.Fatal(err)
	}
	if item.WorkflowID != wf.ID {
		t.Fatalf("expected workflow ID %s, got %s", wf.ID, item.WorkflowID)
	}
	if f.count(t, "SELECT COUNT(*) FROM revisions") != revisionsBefore+1 || f.count(t, "SELECT COUNT(*) FROM entities") != entitiesBefore+1 {
		t.Fatal("expected exactly one Revision and one Entity")
	}
	var entityType, origin string
	if err := f.db.QueryRow("SELECT entity_type FROM entities WHERE id = ?", item.ID).Scan(&entityType); err != nil || entityType != "TRACKED_ITEM" {
		t.Fatalf("entity type %q err=%v", entityType, err)
	}
	if err := f.db.QueryRow("SELECT origin_type FROM revisions WHERE id = ?", item.LastRevisionID).Scan(&origin); err != nil || origin != "USER" {
		t.Fatalf("origin %q err=%v", origin, err)
	}
	view, err := f.tracking.GetWorkItemTracking(f.ctx, wi.ID)
	if err != nil || len(view.Items) != 1 {
		t.Fatalf("view: %+v err=%v", view, err)
	}
	states := view.Items[0].StepStates
	if len(states) != 3 {
		t.Fatalf("expected one state per step, got %d", len(states))
	}
	for i, state := range states {
		if state.State != StepNotStarted || state.WorkflowStepID != def.Steps[i].ID || state.LastRevisionID != item.LastRevisionID ||
			state.EnteredAt != nil || state.CompletedAt != nil {
			t.Fatalf("state %d unexpected: %+v", i, state)
		}
	}
	progress := view.Items[0].Progression
	if progress == nil || progress.TotalSteps != 3 || progress.PassedCount != 0 || *progress.CurrentStepKey != "plan" {
		t.Fatalf("unexpected initial progression: %+v", progress)
	}
}

func TestTrackedItemDependencyInvariantsProjectionAndHistory(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Dependency graph")
	def := f.workflow(t, "Delivery", 1, "plan", "build", "verify")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	a, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "API"})
	b, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "Schema"})
	c, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "Tests"})
	if _, err := f.tracking.AddDependency(f.ctx, TrackedItemRelationInput{FromTrackedItemID: a.ID, ToTrackedItemID: b.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.AddDependency(f.ctx, TrackedItemRelationInput{FromTrackedItemID: b.ID, ToTrackedItemID: c.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.AddDependency(f.ctx, TrackedItemRelationInput{FromTrackedItemID: c.ID, ToTrackedItemID: a.ID}); !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("expected cycle rejection, got %v", err)
	}
	if _, err := f.tracking.AddDependency(f.ctx, TrackedItemRelationInput{FromTrackedItemID: a.ID, ToTrackedItemID: a.ID}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected self rejection, got %v", err)
	}
	if _, err := f.tracking.AddDependency(f.ctx, TrackedItemRelationInput{FromTrackedItemID: a.ID, ToTrackedItemID: b.ID}); !errors.Is(err, ErrRelationExists) {
		t.Fatalf("expected duplicate rejection, got %v", err)
	}
	view, err := f.tracking.GetWorkItemTracking(f.ctx, wi.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Relations) != 2 || view.Relations[0].Type != RelationDependsOn {
		t.Fatalf("unexpected relation projection: %+v", view.Relations)
	}
	if err := f.tracking.RemoveDependency(f.ctx, TrackedItemRelationInput{FromTrackedItemID: a.ID, ToTrackedItemID: b.ID}); err != nil {
		t.Fatal(err)
	}
	if f.count(t, "SELECT COUNT(*) FROM history_events WHERE event_type IN ('TRACKED_ITEM_DEPENDENCY_ADDED','TRACKED_ITEM_DEPENDENCY_REMOVED')") != 3 {
		t.Fatal("dependency mutations must append truthful History")
	}
}

func TestTrackedItemUpdateStageArchiveAndHistory(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Recovery")
	def := f.workflow(t, "Delivery", 1, "plan", "build", "verify")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	parent, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "Parent"})
	typeTask := "TASK"
	item, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "Child"})
	updated, err := f.tracking.UpdateTrackedItem(f.ctx, UpdateTrackedItemInput{ID: item.ID, Title: "Regression tests", ItemType: &typeTask, ParentItemID: &parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Regression tests" || updated.ParentItemID == nil || *updated.ParentItemID != parent.ID {
		t.Fatalf("unexpected update: %+v", updated)
	}
	moved, err := f.tracking.SetTrackedItemStageState(f.ctx, SetTrackedItemStageStateInput{TrackedItemID: item.ID, WorkflowStepID: def.Steps[2].ID, State: StepBlocked})
	if err != nil {
		t.Fatal(err)
	}
	if moved.LastRevisionID == updated.LastRevisionID {
		t.Fatal("stage/state mutation must advance revision")
	}
	view, _ := f.tracking.GetWorkItemTracking(f.ctx, wi.ID)
	var found *TrackedItemView
	for i := range view.Items {
		if view.Items[i].Item.ID == item.ID {
			found = &view.Items[i]
		}
	}
	if found == nil || found.Progression == nil || found.Progression.CurrentStepID == nil || *found.Progression.CurrentStepID != def.Steps[2].ID || found.Progression.CurrentState == nil || *found.Progression.CurrentState != StepBlocked {
		t.Fatalf("unexpected stage projection: %+v", found)
	}
	if _, err := f.tracking.ArchiveTrackedItem(f.ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.GetTrackedItem(f.ctx, item.ID); !errors.Is(err, ErrTrackedItemNotFound) {
		t.Fatalf("archived item must be unavailable, got %v", err)
	}
	if f.count(t, "SELECT COUNT(*) FROM history_events WHERE subject_entity_id = ? AND event_type LIKE 'TRACKED_ITEM_%'", item.ID) != 4 {
		t.Fatal("create/update/stage/archive must each have History")
	}
}

func TestCreateTrackedItemRejectsInvalidReferencesWithoutOrphans(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "One")
	other := f.workItem(t, "Two")
	def := f.workflow(t, "Release", 1, "a")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	wf2, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: other.ID, WorkflowDefinitionID: def.ID})
	if err != nil {
		t.Fatal(err)
	}
	foreignParent, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: other.ID, Title: "Foreign"})
	if err != nil {
		t.Fatal(err)
	}
	unknown, foreign, foreignWF, blank := "missing", foreignParent.ID, wf2.ID, " "

	revisions, entities, items, states := f.count(t, "SELECT COUNT(*) FROM revisions"), f.count(t, "SELECT COUNT(*) FROM entities"), f.count(t, "SELECT COUNT(*) FROM tracked_items"), f.count(t, "SELECT COUNT(*) FROM tracked_item_step_states")
	cases := []struct {
		name  string
		input CreateTrackedItemInput
		want  error
	}{
		{"unknown work item", CreateTrackedItemInput{WorkItemID: "nope", Title: "x"}, ErrWorkItemUnavailable},
		{"blank title", CreateTrackedItemInput{WorkItemID: wi.ID, Title: " "}, ErrInvalidInput},
		{"blank work item id", CreateTrackedItemInput{WorkItemID: blank, Title: "x"}, ErrInvalidInput},
		{"unknown parent", CreateTrackedItemInput{WorkItemID: wi.ID, Title: "x", ParentItemID: &unknown}, ErrInvalidParent},
		{"parent from another work item", CreateTrackedItemInput{WorkItemID: wi.ID, Title: "x", ParentItemID: &foreign}, ErrInvalidParent},
		{"foreign workflow", CreateTrackedItemInput{WorkItemID: wi.ID, Title: "x", WorkflowID: &foreignWF}, ErrInvalidInput},
	}
	for _, tc := range cases {
		if _, err := f.tracking.CreateTrackedItem(f.ctx, tc.input); !errors.Is(err, tc.want) {
			t.Fatalf("%s: expected %v, got %v", tc.name, tc.want, err)
		}
	}
	if f.count(t, "SELECT COUNT(*) FROM revisions") != revisions || f.count(t, "SELECT COUNT(*) FROM entities") != entities ||
		f.count(t, "SELECT COUNT(*) FROM tracked_items") != items || f.count(t, "SELECT COUNT(*) FROM tracked_item_step_states") != states {
		t.Fatal("rejected creations left orphan Entity/Revision/item/state rows")
	}
}

func TestArchivedWorkItemCannotOwnNewTrackedItems(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Gone")
	if _, err := f.db.Exec("UPDATE entities SET archived_at = ? WHERE id = ?", time.Now().UTC().Format(time.RFC3339Nano), wi.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "x"}); !errors.Is(err, ErrWorkItemUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestParentHierarchyIsReadDeterministically(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Release")
	def := f.workflow(t, "Release", 1, "step1")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	root, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "root"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "child", ParentItemID: &root.ID})
	if err != nil {
		t.Fatal(err)
	}
	grandchild, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "grandchild", ParentItemID: &child.ID})
	if err != nil {
		t.Fatal(err)
	}
	for run := 0; run < 3; run++ {
		view, err := f.tracking.GetWorkItemTracking(f.ctx, wi.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(view.Items) != 3 || view.Items[0].Item.ID != root.ID || view.Items[1].Item.ID != child.ID || view.Items[2].Item.ID != grandchild.ID {
			t.Fatalf("unexpected order: %+v", view.Items)
		}
		if view.Items[0].Item.ParentItemID != nil || *view.Items[1].Item.ParentItemID != root.ID || *view.Items[2].Item.ParentItemID != child.ID {
			t.Fatal("parent identity not preserved")
		}
	}
}

func TestDatabaseRejectsCrossWorkItemParent(t *testing.T) {
	f := newFixture(t)
	a, b := f.workItem(t, "A"), f.workItem(t, "B")
	def := f.workflow(t, "Release", 1, "step1")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: a.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: b.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	parent, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: a.ID, Title: "p"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: b.ID, Title: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec("UPDATE tracked_items SET parent_item_id = ? WHERE entity_id = ?", parent.ID, child.ID); err == nil {
		t.Fatal("the schema itself must reject a parent from another WorkItem")
	}
}

func TestSetStepStateRealChangeCreatesOneRevisionAndAdvancesItemFreshness(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Release")
	def := f.workflow(t, "Release", 1, "plan", "build")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	item, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "API"})
	if err != nil {
		t.Fatal(err)
	}
	revisions := f.count(t, "SELECT COUNT(*) FROM revisions")
	proposals := f.count(t, "SELECT COUNT(*) FROM change_proposals")

	state, err := f.tracking.SetTrackedItemStepState(f.ctx, SetStepStateInput{TrackedItemID: item.ID, WorkflowStepID: def.Steps[0].ID, State: StepInProgress})
	if err != nil {
		t.Fatal(err)
	}
	if state.State != StepInProgress || state.EnteredAt == nil || state.CompletedAt != nil || state.LastRevisionID == item.LastRevisionID {
		t.Fatalf("unexpected state: %+v", state)
	}
	if f.count(t, "SELECT COUNT(*) FROM revisions") != revisions+1 {
		t.Fatal("a real change must create exactly one Revision")
	}
	var origin string
	if err := f.db.QueryRow("SELECT origin_type FROM revisions WHERE id = ?", state.LastRevisionID).Scan(&origin); err != nil || origin != "USER" {
		t.Fatalf("origin %q err=%v", origin, err)
	}
	reloaded, err := f.tracking.GetTrackedItem(f.ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.LastRevisionID != state.LastRevisionID || !reloaded.UpdatedAt.After(item.UpdatedAt) {
		t.Fatalf("item freshness must advance with the step change: %+v", reloaded)
	}
	if reloaded.Lifecycle != ItemActive || reloaded.Title != "API" || reloaded.CreatedAt != item.CreatedAt {
		t.Fatalf("unrelated item facts changed: %+v", reloaded)
	}
	// Other step untouched.
	view, _ := f.tracking.GetWorkItemTracking(f.ctx, wi.ID)
	if view.Items[0].StepStates[1].State != StepNotStarted || view.Items[0].StepStates[1].LastRevisionID != item.LastRevisionID {
		t.Fatalf("sibling step must be untouched: %+v", view.Items[0].StepStates[1])
	}
	if f.count(t, "SELECT COUNT(*) FROM change_proposals") != proposals {
		t.Fatal("a human Tracking command must not create a Proposal")
	}
	got, err := f.workItems.GetByID(f.ctx, wi.ID)
	if err != nil || got.Title != "Release" || got.LastRevisionID != wi.LastRevisionID || !got.UpdatedAt.Equal(wi.UpdatedAt) || got.Lifecycle != workitem.LifecycleActive {
		t.Fatalf("WorkItem must be untouched: %+v err=%v", got, err)
	}
}

func TestSetStepStateSameStateIsANoOp(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Release")
	def := f.workflow(t, "Release", 1, "plan")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	item, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "API"})
	revisions := f.count(t, "SELECT COUNT(*) FROM revisions")
	state, err := f.tracking.SetTrackedItemStepState(f.ctx, SetStepStateInput{TrackedItemID: item.ID, WorkflowStepID: def.Steps[0].ID, State: StepNotStarted})
	if err != nil || state.State != StepNotStarted || state.LastRevisionID != item.LastRevisionID {
		t.Fatalf("unexpected: %+v err=%v", state, err)
	}
	after, _ := f.tracking.GetTrackedItem(f.ctx, item.ID)
	if f.count(t, "SELECT COUNT(*) FROM revisions") != revisions || after.LastRevisionID != item.LastRevisionID || !after.UpdatedAt.Equal(item.UpdatedAt) {
		t.Fatal("same-state must create no Revision and change no timestamp")
	}
}

func TestSetStepStateTimestampRules(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Release")
	def := f.workflow(t, "Release", 1, "plan")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	item, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "API"})
	set := func(state StepState) *StepStateRecord {
		t.Helper()
		record, err := f.tracking.SetTrackedItemStepState(f.ctx, SetStepStateInput{TrackedItemID: item.ID, WorkflowStepID: def.Steps[0].ID, State: state})
		if err != nil {
			t.Fatal(err)
		}
		return record
	}
	started := set(StepInProgress)
	blocked := set(StepBlocked)
	if blocked.EnteredAt == nil || !blocked.EnteredAt.Equal(*started.EnteredAt) || blocked.CompletedAt != nil {
		t.Fatalf("entered_at must be preserved across non-initial states: %+v", blocked)
	}
	done := set(StepCompleted)
	if done.CompletedAt == nil || !done.EnteredAt.Equal(*started.EnteredAt) {
		t.Fatalf("COMPLETED sets completed_at: %+v", done)
	}
	reopened := set(StepInProgress)
	if reopened.CompletedAt != nil {
		t.Fatalf("leaving COMPLETED clears completed_at: %+v", reopened)
	}
	reset := set(StepNotStarted)
	if reset.EnteredAt != nil || reset.CompletedAt != nil {
		t.Fatalf("NOT_STARTED clears timestamps: %+v", reset)
	}
	// All-complete must not auto-complete the TrackedItem or WorkItem.
	set(StepCompleted)
	after, _ := f.tracking.GetTrackedItem(f.ctx, item.ID)
	if after.Lifecycle != ItemActive || after.CompletedAt != nil {
		t.Fatalf("workflow completion must not change TrackedItem lifecycle: %+v", after)
	}
	view, _ := f.tracking.GetWorkItemTracking(f.ctx, wi.ID)
	if !view.Items[0].Progression.Complete {
		t.Fatal("derived progression should report complete")
	}
}

func TestSetStepStateRejections(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Release")
	def := f.workflow(t, "Release", 1, "plan")
	other := f.workflow(t, "Other", 1, "x")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	bound, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "bound"})
	revisions := f.count(t, "SELECT COUNT(*) FROM revisions")
	cases := []struct {
		name  string
		input SetStepStateInput
		want  error
	}{
		{"unknown item", SetStepStateInput{TrackedItemID: "nope", WorkflowStepID: def.Steps[0].ID, State: StepCompleted}, ErrTrackedItemNotFound},
		{"unknown step", SetStepStateInput{TrackedItemID: bound.ID, WorkflowStepID: "nope", State: StepCompleted}, ErrStepNotInWorkflow},
		{"step of another workflow", SetStepStateInput{TrackedItemID: bound.ID, WorkflowStepID: other.Steps[0].ID, State: StepCompleted}, ErrStepNotInWorkflow},
		{"invalid state", SetStepStateInput{TrackedItemID: bound.ID, WorkflowStepID: def.Steps[0].ID, State: "DONE"}, ErrInvalidInput},
		{"blank ids", SetStepStateInput{State: StepCompleted}, ErrInvalidInput},
	}
	for _, tc := range cases {
		if _, err := f.tracking.SetTrackedItemStepState(f.ctx, tc.input); !errors.Is(err, tc.want) {
			t.Fatalf("%s: expected %v, got %v", tc.name, tc.want, err)
		}
	}
	if f.count(t, "SELECT COUNT(*) FROM revisions") != revisions {
		t.Fatal("rejected commands must not create Revisions")
	}
}

func TestExistingItemsStayPinnedToTheirWorkflowVersion(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Release")
	v1 := f.workflow(t, "Release", 1, "a", "b")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: v1.ID}); err != nil {
		t.Fatal(err)
	}
	item, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "API"})
	f.workflow(t, "Release", 2, "a", "b", "c")
	view, err := f.tracking.GetWorkItemTracking(f.ctx, wi.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Workflow == nil || view.Workflow.Definition.ID != v1.ID || view.Workflow.Definition.Version != 1 || view.Items[0].Progression.TotalSteps != 2 {
		t.Fatalf("item must remain pinned to v1: %+v", view)
	}
	_ = item
}

func TestTrackingReadModelIsBoundedAndSemantic(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Release")
	empty, err := f.tracking.GetWorkItemTracking(f.ctx, wi.ID)
	if err != nil || len(empty.Items) != 0 || empty.Workflow != nil {
		t.Fatalf("empty WorkItem: %+v err=%v", empty, err)
	}
	if _, err := f.tracking.GetWorkItemTracking(f.ctx, " "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("got %v", err)
	}
}

func TestTrackingTruthSurvivesReopeningTheFileBackedDatabase(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Release")
	def := f.workflow(t, "Release", 1, "plan", "build", "ship")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	item, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "API"})
	if err != nil {
		t.Fatal(err)
	}
	for i, state := range []StepState{StepCompleted, StepBlocked} {
		if _, err := f.tracking.SetTrackedItemStepState(f.ctx, SetStepStateInput{TrackedItemID: item.ID, WorkflowStepID: def.Steps[i].ID, State: state}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := f.tracking.GetWorkItemTracking(f.ctx, wi.ID)
	if err != nil {
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
	after, err := NewService(reopened.DB, workitem.NewService(reopened.DB, workitem.NewRepository(reopened.DB))).GetWorkItemTracking(f.ctx, wi.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, a := before.Items[0], after.Items[0]
	if a.Item.ID != b.Item.ID || a.Item.LastRevisionID != b.Item.LastRevisionID || after.Workflow == nil || len(after.Workflow.Definition.Steps) != 3 {
		t.Fatalf("tracking truth changed across reopen: %+v vs %+v", a, b)
	}
	for i := range b.StepStates {
		if a.StepStates[i].State != b.StepStates[i].State || a.StepStates[i].LastRevisionID != b.StepStates[i].LastRevisionID {
			t.Fatalf("step state %d changed across reopen", i)
		}
	}
	if *a.Progression.CurrentStepKey != "build" || *a.Progression.CurrentState != StepBlocked || a.Progression.PassedCount != 1 || a.Progression.ProgressPercent != 33 {
		t.Fatalf("derived progression differs after reopen: %+v", a.Progression)
	}
}

// Structural guards.

func TestNoPersistedCurrentStepAnywhere(t *testing.T) {
	f := newFixture(t)
	for _, table := range []string{"tracked_items", "tracked_item_step_states", "workflow_definitions", "workflow_steps"} {
		rows, err := f.db.Query("SELECT name FROM pragma_table_info(?)", table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(name, "current") {
				t.Fatalf("%s.%s: current workflow position must be derived, never persisted", table, name)
			}
		}
		_ = rows.Close()
	}
	for _, field := range []string{"CurrentStep", "CurrentStepID", "CurrentStepKey"} {
		src, _ := os.ReadFile("model.go")
		if idx := strings.Index(string(src), "type TrackedItem struct"); idx >= 0 {
			block := string(src)[idx : idx+strings.Index(string(src)[idx:], "}")]
			if strings.Contains(block, field) {
				t.Fatalf("TrackedItem has %s", field)
			}
		}
	}
}

func TestTrackingMutationIsReachableOnlyThroughProposalBoundary(t *testing.T) {
	for _, dir := range []string{"../assistant"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, file := range files {
			parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range parsed.Imports {
				if strings.Contains(spec.Path.Value, "internal/tracking") {
					t.Fatalf("%s imports Tracking: Assistant must remain read-model-only", file)
				}
			}
		}
	}
	proposalSource, err := os.ReadFile("../proposal/apply.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(proposalSource), "executeTrackingOperationTx") {
		t.Fatal("Proposal Apply must own the bounded Tracking mutation bridge")
	}
	files, _ := filepath.Glob("*.go")
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, _ := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		for _, spec := range parsed.Imports {
			for _, banned := range []string{"internal/assistant", "internal/proposal", "internal/ai\"", "internal/openai", "internal/plugin"} {
				if strings.Contains(spec.Path.Value, banned) {
					t.Fatalf("%s imports %s", file, banned)
				}
			}
		}
	}
}
