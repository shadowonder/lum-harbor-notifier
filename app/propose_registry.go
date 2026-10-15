package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/proposalcap"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// Generic dispatch-level sentinels. These describe outcomes of the dispatch
// mechanism itself (unknown ID, malformed arguments, no actual change), not
// any particular capability's vocabulary - exactly like proposal.ErrBaseChanged
// and proposal.ErrTargetUnavailable already are generic across every
// existing operation.
var (
	errUnknownCapability  = errors.New("unknown proposal capability")
	errInvalidArguments   = errors.New("invalid proposal capability arguments")
	errCapabilityNoChange = errors.New("capability produced no change")
)

// proposeHandlerFunc is backend-owned preparation for one Proposal capability:
// authoritatively resolve/validate whatever the arguments reference and return
// the typed proposal.Service intent. It persists nothing; Execute turns the
// intent into a singular Proposal and PrepareOperation into an aggregate child.
// base is untrusted UI context (may be nil); a handler must never treat it as
// permission - only as an optional default hint - for any capability that
// also accepts a stable target ID argument.
type proposeHandlerFunc func(ctx context.Context, a *App, base *assistant.WorkItemSnapshot, args map[string]any, reason *string, prep proposalPreparation) (proposal.OperationInput, error)

// proposalPreparation is the context one capability's arguments are resolved
// in. A singular Proposal admits no symbolic reference. An aggregate child
// admits one only in an argument its handler explicitly declares, and the
// value must be the whole argument; every other argument stays literal.
type proposalPreparation struct {
	aggregateChild bool
}

// reference returns a declared reference argument's value when this is an
// aggregate child and the whole value claims to be a symbolic reference.
// Syntax, direction, and output type are validated by proposal preflight.
func (p proposalPreparation) reference(args map[string]any, name string) (string, bool) {
	if !p.aggregateChild {
		return "", false
	}
	value, ok := trimmedStringArg(args, name)
	if !ok || !proposal.IsReference(value) {
		return "", false
	}
	return value, true
}

type proposeCapabilityRegistration struct {
	descriptor proposalcap.Descriptor
	handler    proposeHandlerFunc
}

// ProposeProvenance carries optional AI run and source revision lineage into
// proposal creation handlers without altering the capability handler signature.
type ProposeProvenance struct {
	AIRunID           *string
	SourceRevisionIDs []string
}

type proposeProvenanceKey struct{}

// ContextWithProposeProvenance attaches AI Run and Source Revision provenance to context.
func ContextWithProposeProvenance(ctx context.Context, prov ProposeProvenance) context.Context {
	return context.WithValue(ctx, proposeProvenanceKey{}, prov)
}

// ProposeProvenanceFromContext extracts AI Run and Source Revision provenance from context,
// or returns zero provenance if none was set.
func ProposeProvenanceFromContext(ctx context.Context) ProposeProvenance {
	if v, ok := ctx.Value(proposeProvenanceKey{}).(ProposeProvenance); ok {
		return v
	}
	return ProposeProvenance{}
}

// ProposeRegistry is the single application-owned source of truth for which
// Proposal capabilities exist, what they need, and how each one executes. It
// implements proposalcap.Source for internal/assistant (descriptor listing)
// and provides the generic lookup -> validate -> invoke execution path.
// Adding a capability means adding one registration here; internal/assistant
// (prompt2.go, candidate.go, engine.go) and this file's own Execute/
// ListDescriptors never change for it.
type ProposeRegistry struct {
	entries map[string]proposeCapabilityRegistration
}

// NewProposeRegistry registers every production Proposal capability. The
// registry remains flat: one backend-owned descriptor and one typed handler
// per reviewable operation.
func NewProposeRegistry() *ProposeRegistry {
	r := &ProposeRegistry{entries: map[string]proposeCapabilityRegistration{}}
	for _, reg := range []proposeCapabilityRegistration{
		workItemUpdateDetailsCapability(),
		trackedItemCreateCapability(),
		trackedItemUpdateCapability(),
		trackedItemStageStateCapability(),
		trackedItemArchiveCapability(),
		trackedItemDependencyAddCapability(),
		trackedItemDependencyRemoveCapability(),
		plannerItemCreateCapability(),
		plannerItemSetLocalNoteCapability(),
		workItemSetLifecycleCapability(),
		workflowCreateCapability(),
		workItemCreateCapability(),
		workItemCreateWithWorkflowCapability(),
		monitorPointCreateCapability(),
		monitorPointSetStateCapability(),
		workflowSetCurrentStepCapability(),
	} {
		r.register(reg)
	}
	return r
}

func (r *ProposeRegistry) register(reg proposeCapabilityRegistration) {
	if _, exists := r.entries[reg.descriptor.ID]; exists {
		panic(fmt.Sprintf("duplicate proposal capability id %q", reg.descriptor.ID))
	}
	r.entries[reg.descriptor.ID] = reg
}

// ListDescriptors implements proposalcap.Source.
func (r *ProposeRegistry) ListDescriptors() []proposalcap.Descriptor {
	out := make([]proposalcap.Descriptor, 0, len(r.entries))
	for _, reg := range r.entries {
		out = append(out, reg.descriptor)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Execute is the entire generic dispatch mechanism: lookup the capability by
// ID, validate raw arguments against its own descriptor, and invoke its
// handler. It never contains capability-specific logic.
func (r *ProposeRegistry) Execute(ctx context.Context, a *App, capabilityID string, base *assistant.WorkItemSnapshot, rawArgs json.RawMessage, reason *string) (*proposal.Proposal, error) {
	input, err := r.prepare(ctx, a, capabilityID, base, rawArgs, reason, proposalPreparation{})
	if err != nil {
		return nil, err
	}
	return a.proposalService.Propose(ctx, input)
}

// PrepareOperation resolves one capability call as an aggregate child: the
// same lookup, argument validation, and handler as Execute, then typed
// preparation instead of persistence. It mutates nothing.
func (r *ProposeRegistry) PrepareOperation(ctx context.Context, a *App, capabilityID string, base *assistant.WorkItemSnapshot, rawArgs json.RawMessage) (proposal.PreparedOperation, error) {
	input, err := r.prepare(ctx, a, capabilityID, base, rawArgs, nil, proposalPreparation{aggregateChild: true})
	if err != nil {
		return proposal.PreparedOperation{}, err
	}
	return a.proposalService.PrepareOperation(ctx, input)
}

// PrepareAggregatePlan prepares an ordered list of capability calls and runs
// whole-plan preflight. Nothing is persisted; the result is ready for
// proposal.Service.CreateAggregateProposal.
func (r *ProposeRegistry) PrepareAggregatePlan(ctx context.Context, a *App, base *assistant.WorkItemSnapshot, calls []proposalcap.Call) ([]proposal.PreparedOperation, error) {
	if len(calls) == 0 || len(calls) > proposal.MaxAggregateOperations {
		return nil, fmt.Errorf("%w: %d operations, expected 1 to %d", proposal.ErrInvalidAggregatePlan, len(calls), proposal.MaxAggregateOperations)
	}
	prepared := make([]proposal.PreparedOperation, 0, len(calls))
	for index, call := range calls {
		operation, err := r.PrepareOperation(ctx, a, call.CapabilityID, base, call.Arguments)
		if err != nil {
			return nil, fmt.Errorf("operation %d (%s): %w", index, call.CapabilityID, err)
		}
		prepared = append(prepared, operation)
	}
	return proposal.PreflightAggregatePlan(prepared)
}

func (r *ProposeRegistry) prepare(ctx context.Context, a *App, capabilityID string, base *assistant.WorkItemSnapshot, rawArgs json.RawMessage, reason *string, prep proposalPreparation) (proposal.OperationInput, error) {
	reg, ok := r.entries[capabilityID]
	if !ok {
		return nil, fmt.Errorf("%w: %q", errUnknownCapability, capabilityID)
	}
	values, err := proposalcap.ValidateArguments(reg.descriptor.Arguments, rawArgs)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidArguments, err)
	}
	return reg.handler(ctx, a, base, values, reason, prep)
}

// --- shared helpers -------------------------------------------------------

func trimmedStringArg(args map[string]any, name string) (string, bool) {
	v, ok := proposalcap.AsString(args, name)
	if !ok {
		return "", false
	}
	trimmed := strings.TrimSpace(v)
	return trimmed, trimmed != ""
}

func optionalStringArg(args map[string]any, name string) *string {
	if v, ok := trimmedStringArg(args, name); ok {
		return &v
	}
	return nil
}

func normalizeOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalStringEqual(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// loadTrackingView authoritatively loads the current Tracking state for
// workItemID directly from the domain service - never from UI context or a
// stale snapshot - so a TrackedItem capability works identically whether or
// not that WorkItem happens to be the one currently selected in the UI.
func loadTrackingView(ctx context.Context, a *App, workItemID string) (*tracking.WorkItemTracking, error) {
	if a.trackingService == nil {
		return nil, fmt.Errorf("%w: tracking is not initialized", errInvalidArguments)
	}
	view, err := a.trackingService.GetWorkItemTracking(ctx, workItemID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", proposal.ErrTargetUnavailable, err)
	}
	if view.Workflow == nil {
		return nil, fmt.Errorf("%w: work item %s has no workflow", errInvalidArguments, workItemID)
	}
	return view, nil
}

// findTrackedItemByTitle resolves a case-insensitive, unambiguous TrackedItem
// title within an authoritatively loaded Tracking view.
func findTrackedItemByTitle(view *tracking.WorkItemTracking, title string) (*tracking.TrackedItem, error) {
	var found *tracking.TrackedItem
	for i := range view.Items {
		if strings.EqualFold(strings.TrimSpace(view.Items[i].Item.Title), title) {
			if found != nil {
				return nil, fmt.Errorf("%w: ambiguous tracked item title %q", errInvalidArguments, title)
			}
			item := view.Items[i].Item
			found = &item
		}
	}
	if found == nil {
		return nil, fmt.Errorf("%w: tracked item %q is not available", errInvalidArguments, title)
	}
	return found, nil
}

// findWorkflowStepByTitle resolves a case-insensitive Workflow step title or key.
func findWorkflowStepByTitle(view *tracking.WorkItemTracking, titleOrKey string) (*tracking.WorkflowStep, error) {
	if view.Workflow == nil {
		return nil, fmt.Errorf("%w: work item has no workflow", errInvalidArguments)
	}
	for i := range view.Workflow.Definition.Steps {
		step := view.Workflow.Definition.Steps[i]
		if strings.EqualFold(step.Title, titleOrKey) || strings.EqualFold(step.Key, titleOrKey) {
			return &step, nil
		}
	}
	return nil, fmt.Errorf("%w: workflow stage %q is not available", errInvalidArguments, titleOrKey)
}

// findMonitorPointByTitle resolves a case-insensitive, testable MonitorPoint title.
func findMonitorPointByTitle(view *tracking.WorkItemTracking, title string) (*tracking.MonitorPoint, error) {
	var found *tracking.MonitorPoint
	for i := range view.MonitorPoints {
		if strings.EqualFold(strings.TrimSpace(view.MonitorPoints[i].Title), title) {
			if found != nil {
				return nil, fmt.Errorf("%w: ambiguous monitor point title %q", errInvalidArguments, title)
			}
			pt := view.MonitorPoints[i]
			found = &pt
		}
	}
	if found == nil {
		return nil, fmt.Errorf("%w: monitor point %q is not available", errInvalidArguments, title)
	}
	return found, nil
}

// --- work_item.update_details ---------------------------------------------

func workItemUpdateDetailsCapability() proposeCapabilityRegistration {
	return proposeCapabilityRegistration{
		descriptor: proposalcap.Descriptor{
			ID:                      proposal.OperationTypeWorkItemUpdateDetails,
			Summary:                 "Propose changing the canonical title and/or description of one existing Work Item; Apply changes content only, not lifecycle, Workflow stage, tracked work, Planner Todos, or derived Attention.",
			UseWhen:                 "Use when the user asks to rename, retitle, or revise the description of a known Work Item. The target must exist and be selected or supplied by stable ID. Do not use for lifecycle changes or Workflow/Tracked Item changes.",
			RequiresWorkItemContext: true,
			SourceInterpretEligible: false,
			Arguments: map[string]proposalcap.ArgumentSpec{
				"proposed_title":       {Type: proposalcap.ArgString, Required: false, Description: "Optional: the new Work Item title. Omit to preserve the current title."},
				"proposed_description": {Type: proposalcap.ArgString, Required: false, Description: "Optional: the new Work Item description. Omit to preserve the current description."},
				"work_item_id":         {Type: proposalcap.ArgString, Required: false, Description: "Optional: the stable ID of the target Work Item. Defaults to currently selected Work Item if omitted."},
			},
		},
		handler: handleWorkItemUpdateDetails,
	}
}

func handleWorkItemUpdateDetails(ctx context.Context, a *App, base *assistant.WorkItemSnapshot, args map[string]any, reason *string, prep proposalPreparation) (proposal.OperationInput, error) {
	var targetID string
	var expectedBase *proposal.ExpectedWorkItemBase

	if wid, ok := trimmedStringArg(args, "work_item_id"); ok && wid != "none" && wid != "null" {
		item, err := a.workitemService.GetByID(ctx, wid)
		if err != nil {
			return nil, fmt.Errorf("%w: target work item %s: %v", proposal.ErrTargetUnavailable, wid, err)
		}
		targetID = item.ID
		expectedBase = &proposal.ExpectedWorkItemBase{
			LastRevisionID: item.LastRevisionID,
			Title:          item.Title,
			Description:    item.Description,
		}
	} else if base != nil {
		targetID = base.ID
		expectedBase = &proposal.ExpectedWorkItemBase{
			LastRevisionID: base.LastRevisionID,
			Title:          base.Title,
			Description:    base.Description,
		}
	} else {
		return nil, fmt.Errorf("%w: a target Work Item is required (specify work_item_id or select an item)", errInvalidArguments)
	}

	var title string
	if _, present := args["proposed_title"]; present {
		t, ok := trimmedStringArg(args, "proposed_title")
		if !ok {
			return nil, fmt.Errorf("%w: proposed_title must not be blank", errInvalidArguments)
		}
		title = t
	} else {
		title = expectedBase.Title
	}

	var description *string
	if _, present := args["proposed_description"]; present {
		description = normalizeOptional(optionalStringArg(args, "proposed_description"))
	} else {
		description = expectedBase.Description
	}

	// No-change dedup is this capability's own domain knowledge (comparing
	// proposed values to current WorkItem truth), not Assistant-layer
	// knowledge.
	if title == expectedBase.Title && optionalStringEqual(description, normalizeOptional(expectedBase.Description)) {
		return nil, errCapabilityNoChange
	}

	prov := ProposeProvenanceFromContext(ctx)
	return proposal.ProposeWorkItemUpdateInput{
		TargetWorkItemID:    targetID,
		ProposedTitle:       title,
		ProposedDescription: description,
		Reason:              reason,
		Producer:            proposal.ProducerAI,
		ExpectedBase:        expectedBase,
		AIRunID:             prov.AIRunID,
		SourceRevisionIDs:   prov.SourceRevisionIDs,
	}, nil
}

// --- tracked_item.* ---------------------------------------------------

func workItemIDArgSpec() proposalcap.ArgumentSpec {
	return proposalcap.ArgumentSpec{Type: proposalcap.ArgString, Required: true, Description: "The stable ID of the parent Work Item. Resolve it via a prior read if the user referred to the Work Item by title."}
}

func trackedItemCreateCapability() proposeCapabilityRegistration {
	return proposeCapabilityRegistration{
		descriptor: proposalcap.Descriptor{
			ID:                      proposal.OperationTypeTrackedItemCreate,
			Summary:                 "Propose adding one canonical execution unit to an existing Work Item Workflow, optionally nested under an existing same-WorkItem Tracked Item. Apply does not create a Planner Todo, due date, reminder, or move the Workflow's overall stage.",
			UseWhen:                 "Use when the user asks to add one tracked task/sub-deliverable to a known Work Item that already has a Workflow. Do not use for personal actionable Todos, reminder scheduling, or creating a Workflow.",
			SourceInterpretEligible: true,
			Arguments: map[string]proposalcap.ArgumentSpec{
				"work_item_id":      {Type: proposalcap.ArgString, Required: true, Description: workItemIDArgSpec().Description + " Accepts an earlier-operation reference: in a multi-operation plan, \"$op[N].work_item_id\" names a Work Item created together with its Workflow by earlier operation N."},
				"title":             {Type: proposalcap.ArgString, Required: true, Description: "The title of the new tracked item."},
				"item_type":         {Type: proposalcap.ArgString, Required: false, Description: "Optional free-form item type label."},
				"parent_item_title": {Type: proposalcap.ArgString, Required: false, Description: "Title of an existing tracked item this one nests under, if any."},
			},
		},
		handler: handleTrackedItemCreate,
	}
}

func handleTrackedItemCreate(ctx context.Context, a *App, base *assistant.WorkItemSnapshot, args map[string]any, reason *string, prep proposalPreparation) (proposal.OperationInput, error) {
	workItemID, ok := trimmedStringArg(args, "work_item_id")
	if !ok {
		return nil, fmt.Errorf("%w: work_item_id is required", errInvalidArguments)
	}
	title, _ := trimmedStringArg(args, "title")
	itemType := optionalStringArg(args, "item_type")
	payload := proposal.TrackedItemOperationPayload{OperationKind: proposal.OperationTypeTrackedItemCreate, Version: proposal.OperationVersionV1, WorkItemID: &workItemID, Title: &title, ItemType: itemType}
	prov := ProposeProvenanceFromContext(ctx)
	input := proposal.ProposeTrackedItemOperationInput{
		Payload:           payload,
		Reason:            reason,
		Producer:          proposal.ProducerAI,
		AIRunID:           prov.AIRunID,
		SourceRevisionIDs: prov.SourceRevisionIDs,
	}
	// work_item_id is this capability's declared reference argument: a Work
	// Item created earlier in the same aggregate has no Tracking view yet.
	if _, isRef := prep.reference(args, "work_item_id"); isRef {
		if _, hasParent := trimmedStringArg(args, "parent_item_title"); hasParent {
			return nil, fmt.Errorf("%w: parent_item_title cannot be resolved in a work item created by the same proposal", errInvalidArguments)
		}
		return input, nil
	}
	view, err := loadTrackingView(ctx, a, workItemID)
	if err != nil {
		return nil, err
	}
	if parentTitle, ok := trimmedStringArg(args, "parent_item_title"); ok {
		parent, err := findTrackedItemByTitle(view, parentTitle)
		if err != nil {
			return nil, err
		}
		input.Payload.ParentItemID = &parent.ID
	}
	return input, nil
}

func trackedItemUpdateCapability() proposeCapabilityRegistration {
	return proposeCapabilityRegistration{
		descriptor: proposalcap.Descriptor{
			ID:                      proposal.OperationTypeTrackedItemUpdate,
			Summary:                 "Propose changing one existing Tracked Item's title, optional type, or same-WorkItem parent while preserving its Workflow and stage states.",
			UseWhen:                 "Use when the user asks to rename, reclassify, or re-parent a known Tracked Item in a known Work Item Workflow. Do not use to move its stage/state, change dependencies, schedule a reminder, or move the overall Workflow.",
			SourceInterpretEligible: true,
			Arguments: map[string]proposalcap.ArgumentSpec{
				"work_item_id":      workItemIDArgSpec(),
				"target_item_title": {Type: proposalcap.ArgString, Required: true, Description: "The current title of the tracked item to update."},
				"proposed_title":    {Type: proposalcap.ArgString, Required: true, Description: "The new title."},
				"item_type":         {Type: proposalcap.ArgString, Required: false, Description: "Optional new item type label."},
				"parent_item_title": {Type: proposalcap.ArgString, Required: false, Description: "Title of the new parent tracked item, if changing."},
			},
		},
		handler: handleTrackedItemUpdate,
	}
}

func handleTrackedItemUpdate(ctx context.Context, a *App, base *assistant.WorkItemSnapshot, args map[string]any, reason *string, prep proposalPreparation) (proposal.OperationInput, error) {
	workItemID, ok := trimmedStringArg(args, "work_item_id")
	if !ok {
		return nil, fmt.Errorf("%w: work_item_id is required", errInvalidArguments)
	}
	view, err := loadTrackingView(ctx, a, workItemID)
	if err != nil {
		return nil, err
	}
	targetTitle, ok := trimmedStringArg(args, "target_item_title")
	if !ok {
		return nil, fmt.Errorf("%w: target_item_title is required", errInvalidArguments)
	}
	item, err := findTrackedItemByTitle(view, targetTitle)
	if err != nil {
		return nil, err
	}
	proposedTitle, _ := trimmedStringArg(args, "proposed_title")

	payload := proposal.TrackedItemOperationPayload{
		OperationKind: proposal.OperationTypeTrackedItemUpdate, Version: proposal.OperationVersionV1,
		WorkItemID: &workItemID, TrackedItemID: &item.ID, ExpectedLastRevisionID: &item.LastRevisionID,
		Title: &proposedTitle, ItemType: item.ItemType, ParentItemID: item.ParentItemID,
	}
	if itemType := optionalStringArg(args, "item_type"); itemType != nil {
		payload.ItemType = itemType
	}
	if parentTitle, ok := trimmedStringArg(args, "parent_item_title"); ok {
		parent, err := findTrackedItemByTitle(view, parentTitle)
		if err != nil {
			return nil, err
		}
		payload.ParentItemID = &parent.ID
	}
	prov := ProposeProvenanceFromContext(ctx)
	return proposal.ProposeTrackedItemOperationInput{
		Payload:           payload,
		Reason:            reason,
		Producer:          proposal.ProducerAI,
		AIRunID:           prov.AIRunID,
		SourceRevisionIDs: prov.SourceRevisionIDs,
	}, nil
}

func trackedItemStageStateCapability() proposeCapabilityRegistration {
	return proposeCapabilityRegistration{
		descriptor: proposalcap.Descriptor{
			ID:                      proposal.OperationTypeTrackedItemStageState,
			Summary:                 "Propose setting one Tracked Item's per-stage state in its existing Workflow. Apply updates that item's progression only and never moves the Workflow's overall current step or the Work Item lifecycle.",
			UseWhen:                 "Use when the user asks to move, advance, block, wait, fail, skip, or complete a specific Tracked Item at a named Workflow stage. Do not use for moving the overall Workflow stage.",
			SourceInterpretEligible: true,
			Arguments: map[string]proposalcap.ArgumentSpec{
				"work_item_id":      workItemIDArgSpec(),
				"target_item_title": {Type: proposalcap.ArgString, Required: true, Description: "The title of the tracked item to move."},
				"stage_title":       {Type: proposalcap.ArgString, Required: true, Description: "The title of the target Workflow stage."},
				"state": {
					Type: proposalcap.ArgEnum, Required: true, Description: "The target step state.",
					EnumValues: []string{"NOT_STARTED", "IN_PROGRESS", "COMPLETED", "WAITING", "BLOCKED", "FAILED", "UNKNOWN", "SKIPPED"},
				},
			},
		},
		handler: handleTrackedItemStageState,
	}
}

func handleTrackedItemStageState(ctx context.Context, a *App, base *assistant.WorkItemSnapshot, args map[string]any, reason *string, prep proposalPreparation) (proposal.OperationInput, error) {
	workItemID, ok := trimmedStringArg(args, "work_item_id")
	if !ok {
		return nil, fmt.Errorf("%w: work_item_id is required", errInvalidArguments)
	}
	view, err := loadTrackingView(ctx, a, workItemID)
	if err != nil {
		return nil, err
	}
	targetTitle, ok := trimmedStringArg(args, "target_item_title")
	if !ok {
		return nil, fmt.Errorf("%w: target_item_title is required", errInvalidArguments)
	}
	item, err := findTrackedItemByTitle(view, targetTitle)
	if err != nil {
		return nil, err
	}
	stageTitle, ok := trimmedStringArg(args, "stage_title")
	if !ok {
		return nil, fmt.Errorf("%w: stage_title is required", errInvalidArguments)
	}
	step, err := findWorkflowStepByTitle(view, stageTitle)
	if err != nil {
		return nil, err
	}
	state, _ := trimmedStringArg(args, "state")

	payload := proposal.TrackedItemOperationPayload{
		OperationKind: proposal.OperationTypeTrackedItemStageState, Version: proposal.OperationVersionV1,
		WorkItemID: &workItemID, TrackedItemID: &item.ID, ExpectedLastRevisionID: &item.LastRevisionID,
		WorkflowStepID: &step.ID, State: &state,
	}
	prov := ProposeProvenanceFromContext(ctx)
	return proposal.ProposeTrackedItemOperationInput{
		Payload:           payload,
		Reason:            reason,
		Producer:          proposal.ProducerAI,
		AIRunID:           prov.AIRunID,
		SourceRevisionIDs: prov.SourceRevisionIDs,
	}, nil
}

func trackedItemArchiveCapability() proposeCapabilityRegistration {
	return proposeCapabilityRegistration{
		descriptor: proposalcap.Descriptor{
			ID:                      proposal.OperationTypeTrackedItemArchive,
			Summary:                 "Propose archiving one existing Tracked Item so it leaves active tracking; this does not delete its Work Item, Workflow, History, or related Planner truth.",
			UseWhen:                 "Use when the user explicitly asks to remove or archive a known Tracked Item. Do not use for completing a task, changing its stage state, or cancelling the parent Work Item.",
			SourceInterpretEligible: true,
			Arguments: map[string]proposalcap.ArgumentSpec{
				"work_item_id":      workItemIDArgSpec(),
				"target_item_title": {Type: proposalcap.ArgString, Required: true, Description: "The title of the tracked item to archive."},
			},
		},
		handler: handleTrackedItemArchive,
	}
}

func handleTrackedItemArchive(ctx context.Context, a *App, base *assistant.WorkItemSnapshot, args map[string]any, reason *string, prep proposalPreparation) (proposal.OperationInput, error) {
	workItemID, ok := trimmedStringArg(args, "work_item_id")
	if !ok {
		return nil, fmt.Errorf("%w: work_item_id is required", errInvalidArguments)
	}
	view, err := loadTrackingView(ctx, a, workItemID)
	if err != nil {
		return nil, err
	}
	targetTitle, ok := trimmedStringArg(args, "target_item_title")
	if !ok {
		return nil, fmt.Errorf("%w: target_item_title is required", errInvalidArguments)
	}
	item, err := findTrackedItemByTitle(view, targetTitle)
	if err != nil {
		return nil, err
	}
	payload := proposal.TrackedItemOperationPayload{
		OperationKind: proposal.OperationTypeTrackedItemArchive, Version: proposal.OperationVersionV1,
		WorkItemID: &workItemID, TrackedItemID: &item.ID, ExpectedLastRevisionID: &item.LastRevisionID,
	}
	prov := ProposeProvenanceFromContext(ctx)
	return proposal.ProposeTrackedItemOperationInput{
		Payload:           payload,
		Reason:            reason,
		Producer:          proposal.ProducerAI,
		AIRunID:           prov.AIRunID,
		SourceRevisionIDs: prov.SourceRevisionIDs,
	}, nil
}

func trackedItemDependencyArgSpecs() map[string]proposalcap.ArgumentSpec {
	return map[string]proposalcap.ArgumentSpec{
		"work_item_id":       workItemIDArgSpec(),
		"target_item_title":  {Type: proposalcap.ArgString, Required: true, Description: "The title of the tracked item the dependency is added to or removed from."},
		"related_item_title": {Type: proposalcap.ArgString, Required: true, Description: "The title of the tracked item it depends on."},
	}
}

func trackedItemDependencyAddCapability() proposeCapabilityRegistration {
	return proposeCapabilityRegistration{
		descriptor: proposalcap.Descriptor{
			ID:                      proposal.OperationTypeTrackedItemDependencyAdd,
			Summary:                 "Propose one directed DEPENDS_ON relation between two active Tracked Items in the same Workflow; Apply rejects duplicates, cross-Workflow edges, and dependency cycles.",
			UseWhen:                 "Use when the user says one known Tracked Item depends on or is blocked by another in the same Work Item Workflow. Do not use for hierarchy/parenting, ordering Workflow stages, or cross-WorkItem relations.",
			SourceInterpretEligible: true,
			Arguments:               trackedItemDependencyArgSpecs(),
		},
		handler: handleTrackedItemDependencyAdd,
	}
}

func handleTrackedItemDependencyAdd(ctx context.Context, a *App, base *assistant.WorkItemSnapshot, args map[string]any, reason *string, prep proposalPreparation) (proposal.OperationInput, error) {
	return handleTrackedItemDependency(ctx, a, args, reason, proposal.OperationTypeTrackedItemDependencyAdd)
}

func trackedItemDependencyRemoveCapability() proposeCapabilityRegistration {
	return proposeCapabilityRegistration{
		descriptor: proposalcap.Descriptor{
			ID:                      proposal.OperationTypeTrackedItemDependencyRemove,
			Summary:                 "Propose removing one existing directed DEPENDS_ON relation between two Tracked Items in the same Workflow; the items and their states remain unchanged.",
			UseWhen:                 "Use when the user explicitly asks to remove a known dependency edge. Do not use to archive either item, change hierarchy, or change stage state.",
			SourceInterpretEligible: true,
			Arguments:               trackedItemDependencyArgSpecs(),
		},
		handler: handleTrackedItemDependencyRemove,
	}
}

func handleTrackedItemDependencyRemove(ctx context.Context, a *App, base *assistant.WorkItemSnapshot, args map[string]any, reason *string, prep proposalPreparation) (proposal.OperationInput, error) {
	return handleTrackedItemDependency(ctx, a, args, reason, proposal.OperationTypeTrackedItemDependencyRemove)
}

func handleTrackedItemDependency(ctx context.Context, a *App, args map[string]any, reason *string, operationKind string) (proposal.OperationInput, error) {
	workItemID, ok := trimmedStringArg(args, "work_item_id")
	if !ok {
		return nil, fmt.Errorf("%w: work_item_id is required", errInvalidArguments)
	}
	view, err := loadTrackingView(ctx, a, workItemID)
	if err != nil {
		return nil, err
	}
	targetTitle, ok := trimmedStringArg(args, "target_item_title")
	if !ok {
		return nil, fmt.Errorf("%w: target_item_title is required", errInvalidArguments)
	}
	item, err := findTrackedItemByTitle(view, targetTitle)
	if err != nil {
		return nil, err
	}
	relatedTitle, ok := trimmedStringArg(args, "related_item_title")
	if !ok {
		return nil, fmt.Errorf("%w: related_item_title is required", errInvalidArguments)
	}
	related, err := findTrackedItemByTitle(view, relatedTitle)
	if err != nil {
		return nil, err
	}
	payload := proposal.TrackedItemOperationPayload{
		OperationKind: operationKind, Version: proposal.OperationVersionV1,
		WorkItemID: &workItemID, TrackedItemID: &item.ID, ExpectedLastRevisionID: &item.LastRevisionID, ToTrackedItemID: &related.ID,
	}
	prov := ProposeProvenanceFromContext(ctx)
	return proposal.ProposeTrackedItemOperationInput{
		Payload:           payload,
		Reason:            reason,
		Producer:          proposal.ProducerAI,
		AIRunID:           prov.AIRunID,
		SourceRevisionIDs: prov.SourceRevisionIDs,
	}, nil
}

// --- planner.item.create ---------------------------------------------------

func plannerItemCreateCapability() proposeCapabilityRegistration {
	return proposeCapabilityRegistration{
		descriptor: proposalcap.Descriptor{
			ID:                      proposal.OperationTypePlannerItemCreate,
			Summary:                 "Propose one actionable Planner Todo, optionally due, linked to existing Work Item/Workflow provenance, and configured with reminder offsets. On Apply the Todo and requested reminder configuration are created together; background evaluation later derives Attention and Notification at reached thresholds.",
			UseWhen:                 "Use for personal or contextual actionable work, including 'remind me' requests. A reminder requires a due target; supply the user's requested offset, while remind=true without an offset means at due time. Do not create separate Attention or Notification truth, and do not use this as a Workflow Tracked Item or Monitor Point. Never use it to add a note or context to a Planner item that already exists.",
			RequiresWorkItemContext: false,
			SourceInterpretEligible: true,
			Arguments: map[string]proposalcap.ArgumentSpec{
				"title":                    {Type: proposalcap.ArgString, Required: true, Description: "The Planner item's title or task to remember."},
				"notes":                    {Type: proposalcap.ArgString, Required: false, Description: "Optional notes or details for the item."},
				"due_date":                 {Type: proposalcap.ArgDate, Required: false, Description: "Optional local calendar date (YYYY-MM-DD)."},
				"due_time":                 {Type: proposalcap.ArgString, Required: false, Description: "Optional local clock time (e.g. 15:00 or 3:00 PM). Do not guess if omitted."},
				"timezone":                 {Type: proposalcap.ArgString, Required: false, Description: "Optional timezone name or offset (e.g. 'Eastern', '-04:00'). Omit to use the host system timezone."},
				"remind":                   {Type: proposalcap.ArgBoolean, Required: false, Description: "Optional flag: true to create an active reminder. Requires a due date/time; without an explicit offset it means at due time."},
				"reminder_offset_minutes":  {Type: proposalcap.ArgInteger, Required: false, Description: "Optional single non-negative minute offset before the due time (for example 60 means one hour before; 0 means at due time)."},
				"reminder_offsets_minutes": {Type: proposalcap.ArgStringList, Required: false, Description: "Optional list of reminder offsets in minutes or strings before due time (e.g. [\"4320\", \"2880\", \"1440\"] or [\"3d\", \"2d\", \"1d\"] or [\"0\"] for at due time)."},
				"work_item_id":             {Type: proposalcap.ArgString, Required: false, Description: "Optional internal Core Work Item ID if this todo originates from an internal work item. Accepts an earlier-operation reference (\"$op[N].work_item_id\") in a multi-operation plan."},
				"workflow_id":              {Type: proposalcap.ArgString, Required: false, Description: "Optional internal Core Workflow ID if this todo originates from an internal workflow. External source workflow IDs must not be used. Accepts an earlier-operation reference (\"$op[N].workflow_id\") in a multi-operation plan."},
				"workflow_step_id":         {Type: proposalcap.ArgString, Required: false, Description: "Optional internal Core Workflow Step ID if this todo originates from an internal workflow step."},
				"tracked_item_id":          {Type: proposalcap.ArgString, Required: false, Description: "Optional internal Core TrackedItem ID this Todo acts on. It must belong to the supplied Work Item and Workflow. Accepts an earlier-operation reference (\"$op[N].tracked_item_id\") naming a Tracked Item created by earlier operation N; its Work Item and Workflow are then implied."},
			},
		},
		handler: handlePlannerItemCreate,
	}
}

func parseReminderOffsets(args map[string]any) []int {
	var offsets []int
	seen := make(map[int]bool)

	addOffset := func(sec int) {
		if sec >= 0 && !seen[sec] {
			seen[sec] = true
			offsets = append(offsets, sec)
		}
	}

	parseItem := func(v any) {
		switch val := v.(type) {
		case float64:
			addOffset(int(val) * 60)
		case int:
			addOffset(val * 60)
		case string:
			clean := strings.TrimSpace(strings.ToLower(val))
			if clean == "0" || clean == "at due time" || clean == "at due" || clean == "due time" || clean == "now" {
				addOffset(0)
				return
			}
			multiplier := 60 // default minutes
			if strings.HasSuffix(clean, "d") || strings.Contains(clean, "day") {
				multiplier = 86400
			} else if strings.HasSuffix(clean, "h") || strings.Contains(clean, "hour") {
				multiplier = 3600
			} else if strings.HasSuffix(clean, "m") || strings.Contains(clean, "min") {
				multiplier = 60
			} else if strings.HasSuffix(clean, "s") || strings.Contains(clean, "sec") {
				multiplier = 1
			}
			var num int
			for _, ch := range clean {
				if ch >= '0' && ch <= '9' {
					num = num*10 + int(ch-'0')
				} else if num > 0 {
					break
				}
			}
			if num > 0 || clean == "0" {
				addOffset(num * multiplier)
			}
		}
	}

	if rawList, ok := args["reminder_offsets_minutes"].([]any); ok {
		for _, item := range rawList {
			parseItem(item)
		}
	}
	if rawList, ok := args["reminder_offsets"].([]any); ok {
		for _, item := range rawList {
			if v, ok := item.(float64); ok {
				addOffset(int(v))
			} else if v, ok := item.(int); ok {
				addOffset(v)
			} else {
				parseItem(item)
			}
		}
	}
	if v, ok := args["reminder_offset_minutes"]; ok {
		parseItem(v)
	}
	if len(offsets) == 0 {
		if remind, ok := args["remind"].(bool); ok && remind {
			addOffset(0)
		}
	}

	sort.Slice(offsets, func(i, j int) bool {
		return offsets[i] > offsets[j]
	})
	return offsets
}

var defaultTrackingReminderOffsetsSeconds = []int{3 * 24 * 60 * 60, 2 * 24 * 60 * 60, 24 * 60 * 60}

func handlePlannerItemCreate(ctx context.Context, a *App, base *assistant.WorkItemSnapshot, args map[string]any, reason *string, prep proposalPreparation) (proposal.OperationInput, error) {
	title, ok := trimmedStringArg(args, "title")
	if !ok {
		return nil, fmt.Errorf("%w: title is required", errInvalidArguments)
	}

	var details *string
	if n := optionalStringArg(args, "notes"); n != nil {
		details = n
	} else if d := optionalStringArg(args, "details"); d != nil {
		details = d
	}

	dueDateStr := optionalStringArg(args, "due_date")
	dueTimeStr := optionalStringArg(args, "due_time")
	tzStr := optionalStringArg(args, "timezone")
	dueAtStr := optionalStringArg(args, "due_at")

	var domainDueDate *string
	var domainDueAt *string

	// Direct due_at backward compatibility:
	if dueAtStr != nil && *dueAtStr != "" {
		normalizedDueAt, err := normalizeRFC3339(*dueAtStr, time.Local)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errInvalidArguments, err)
		}
		domainDueAt = &normalizedDueAt
	} else if dueDateStr != nil && *dueDateStr != "" {
		if dueTimeStr != nil && *dueTimeStr != "" {
			// CASE 2 or 3: Combine date + clock time + timezone
			combinedDueAt, err := combineDateAndTime(*dueDateStr, *dueTimeStr, tzStr, time.Local)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", errInvalidArguments, err)
			}
			domainDueAt = &combinedDueAt
			domainDueDate = nil
		} else {
			// CASE 1: Date only
			cleanDate, err := normalizeDateOnly(*dueDateStr)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", errInvalidArguments, err)
			}
			domainDueDate = &cleanDate
			domainDueAt = nil
		}
	} else if dueTimeStr != nil && *dueTimeStr != "" {
		// Clock time given without explicit date -> default to today in system local time
		todayDate := time.Now().Format("2006-01-02")
		combinedDueAt, err := combineDateAndTime(todayDate, *dueTimeStr, tzStr, time.Local)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errInvalidArguments, err)
		}
		domainDueAt = &combinedDueAt
		domainDueDate = nil
	}
	// CASE 4: Neither due_date nor due_time provided -> domainDueDate = nil, domainDueAt = nil

	offsets := parseReminderOffsets(args)
	prov := ProposeProvenanceFromContext(ctx)
	input := proposal.ProposePlannerItemCreateInput{
		Title:             title,
		Details:           details,
		DueDate:           domainDueDate,
		DueAt:             domainDueAt,
		OffsetsSeconds:    offsets,
		Reason:            reason,
		Producer:          proposal.ProducerAI,
		AIRunID:           prov.AIRunID,
		SourceRevisionIDs: prov.SourceRevisionIDs,
	}
	// work_item_id, workflow_id and tracked_item_id are this capability's
	// declared reference arguments; a reference passes through unresolved.
	if ref, isRef := prep.reference(args, "work_item_id"); isRef {
		input.WorkItemID = &ref
	} else if workItemID, ok := trimmedStringArg(args, "work_item_id"); ok {
		input.WorkItemID = resolveCoreWorkItemID(ctx, a, workItemID)
	} else if base != nil && base.ID != "" {
		input.WorkItemID = &base.ID
	}
	if ref, isRef := prep.reference(args, "workflow_id"); isRef {
		input.WorkflowID = &ref
	} else if workflowID, ok := trimmedStringArg(args, "workflow_id"); ok {
		input.WorkflowID = resolveCoreWorkflowID(ctx, a, workflowID)
	}
	if workflowStepID, ok := trimmedStringArg(args, "workflow_step_id"); ok {
		input.WorkflowStepID = resolveCoreWorkflowStepID(ctx, a, input.WorkflowID, workflowStepID)
	}
	if trackedItemID, ok := trimmedStringArg(args, "tracked_item_id"); ok {
		input.TrackedItemID = &trackedItemID
		if len(input.OffsetsSeconds) == 0 && (input.DueAt != nil || input.DueDate != nil) {
			input.OffsetsSeconds = append([]int(nil), defaultTrackingReminderOffsetsSeconds...)
		}
	}
	return input, nil
}

func resolveCoreWorkflowID(ctx context.Context, a *App, rawID string) *string {
	clean := strings.TrimSpace(rawID)
	if clean == "" || clean == "none" || clean == "null" {
		return nil
	}
	if a != nil && a.trackingService != nil {
		if wf, err := a.trackingService.ResolveWorkflowReference(ctx, clean); err == nil && wf != nil {
			return &wf.ID
		}
	}
	// External workflow identity that has not been explicitly resolved to a Core Workflow
	// must remain unset/nil (External Source identity != Core domain identity).
	return nil
}

func resolveCoreWorkflowStepID(ctx context.Context, a *App, workflowID *string, rawStepID string) *string {
	clean := strings.TrimSpace(rawStepID)
	if clean == "" || clean == "none" || clean == "null" {
		return nil
	}
	if workflowID == nil {
		return nil
	}
	if a != nil && a.trackingService != nil {
		if step, err := a.trackingService.GetWorkflowStep(ctx, clean); err == nil && step != nil {
			return &step.ID
		}
	}
	return nil
}

func resolveCoreWorkItemID(ctx context.Context, a *App, rawID string) *string {
	clean := strings.TrimSpace(rawID)
	if clean == "" || clean == "none" || clean == "null" {
		return nil
	}
	if a != nil && a.workitemService != nil {
		if wi, err := a.workitemService.GetByID(ctx, clean); err == nil && wi != nil {
			return &wi.ID
		}
	}
	return nil
}

// parseClockTime parses human/model clock time strings deterministically.
// Accepts: "15:04", "15:04:05", "3:04 PM", "3:04PM", "3 PM", "3PM", "15",
// case-insensitively, with or without spaces before AM/PM.
func parseClockTime(s string) (hour int, min int, sec int, err error) {
	clean := strings.TrimSpace(s)
	if clean == "" {
		return 0, 0, 0, errors.New("empty clock time")
	}
	upper := strings.ToUpper(clean)

	layouts := []string{
		"15:04:05",
		"15:04",
		"3:04:05 PM",
		"3:04:05PM",
		"3:04 PM",
		"3:04PM",
		"3 PM",
		"3PM",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, upper); err == nil {
			return t.Hour(), t.Minute(), t.Second(), nil
		}
	}

	if len(upper) <= 2 {
		if h, err := strconv.Atoi(upper); err == nil && h >= 0 && h <= 23 {
			return h, 0, 0, nil
		}
	}

	return 0, 0, 0, fmt.Errorf("cannot parse clock time %q", s)
}

// resolveLocation maps timezone names or offsets to *time.Location.
// Defaults to defaultLoc (host system timezone) when omitted or unspecified.
func resolveLocation(tzStr *string, defaultLoc *time.Location) (*time.Location, error) {
	if defaultLoc == nil {
		defaultLoc = time.Local
	}
	if tzStr == nil || strings.TrimSpace(*tzStr) == "" {
		return defaultLoc, nil
	}
	tz := strings.TrimSpace(*tzStr)
	switch strings.ToLower(tz) {
	case "default", "system", "local":
		return defaultLoc, nil
	case "eastern", "est", "edt", "us/eastern":
		return time.LoadLocation("America/New_York")
	case "central", "cst", "cdt", "us/central":
		return time.LoadLocation("America/Chicago")
	case "mountain", "mst", "mdt", "us/mountain":
		return time.LoadLocation("America/Denver")
	case "pacific", "pst", "pdt", "us/pacific":
		return time.LoadLocation("America/Los_Angeles")
	case "utc", "gmt":
		return time.UTC, nil
	}

	if loc, err := time.LoadLocation(tz); err == nil {
		return loc, nil
	}

	if strings.HasPrefix(tz, "+") || strings.HasPrefix(tz, "-") {
		dummy := fmt.Sprintf("2026-01-01T00:00:00%s", tz)
		if t, err := time.Parse(time.RFC3339, dummy); err == nil {
			return t.Location(), nil
		}
	}

	return nil, fmt.Errorf("unknown timezone %q", tz)
}

// combineDateAndTime combines a local calendar date, clock time, and optional timezone
// into a standardized RFC3339 timestamp.
func combineDateAndTime(dateStr, timeStr string, tzStr *string, defaultLoc *time.Location) (string, error) {
	cleanDate, err := normalizeDateOnly(dateStr)
	if err != nil {
		return "", err
	}
	dateParts := strings.Split(cleanDate, "-")
	if len(dateParts) != 3 {
		return "", fmt.Errorf("invalid date %q", cleanDate)
	}
	year, _ := strconv.Atoi(dateParts[0])
	month, _ := strconv.Atoi(dateParts[1])
	day, _ := strconv.Atoi(dateParts[2])

	hour, min, sec, err := parseClockTime(timeStr)
	if err != nil {
		return "", err
	}

	loc, err := resolveLocation(tzStr, defaultLoc)
	if err != nil {
		return "", err
	}

	t := time.Date(year, time.Month(month), day, hour, min, sec, 0, loc)
	return t.Format(time.RFC3339), nil
}

func normalizeDateOnly(s string) (string, error) {
	clean := strings.TrimSpace(s)
	if len(clean) > 10 {
		if idx := strings.Index(clean, "T"); idx > 0 {
			clean = clean[:idx]
		} else if idx := strings.Index(clean, " "); idx > 0 {
			clean = clean[:idx]
		}
	}
	if _, err := time.Parse("2006-01-02", clean); err != nil {
		return "", fmt.Errorf("invalid date format %q: expected YYYY-MM-DD", s)
	}
	return clean, nil
}

func normalizeRFC3339(s string, defaultLoc *time.Location) (string, error) {
	clean := strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, clean); err == nil {
		return t.Format(time.RFC3339), nil
	}
	if defaultLoc == nil {
		defaultLoc = time.Local
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04:05", clean, defaultLoc); err == nil {
		return t.Format(time.RFC3339), nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", clean, defaultLoc); err == nil {
		return t.Format(time.RFC3339), nil
	}
	return "", fmt.Errorf("cannot parse datetime %q as RFC3339", s)
}

// --- workitem.set_lifecycle -------------------------------------------------

func workItemSetLifecycleCapability() proposeCapabilityRegistration {
	return proposeCapabilityRegistration{
		descriptor: proposalcap.Descriptor{
			ID:                      proposal.OperationTypeWorkItemSetLifecycle,
			Summary:                 "Propose changing one existing Work Item's canonical lifecycle to ACTIVE, PAUSED, COMPLETED, or CANCELLED. Apply does not move its Workflow, change Tracked Items, or complete linked Planner Todos.",
			UseWhen:                 "Use when the user asks to pause, resume, complete, or cancel a known Work Item, resolved by stable ID. Do not use for Workflow stage movement or Tracked Item state.",
			RequiresWorkItemContext: false,
			SourceInterpretEligible: true,
			Arguments: map[string]proposalcap.ArgumentSpec{
				"work_item_id": {Type: proposalcap.ArgString, Required: true, Description: "The stable ID of the Work Item. Resolve it via a prior read if the user referred to it by title."},
				"target_state": {
					Type: proposalcap.ArgEnum, Required: true, Description: "The target lifecycle state.",
					EnumValues: []string{"ACTIVE", "PAUSED", "COMPLETED", "CANCELLED"},
				},
			},
		},
		handler: handleWorkItemSetLifecycle,
	}
}

func handleWorkItemSetLifecycle(ctx context.Context, a *App, base *assistant.WorkItemSnapshot, args map[string]any, reason *string, prep proposalPreparation) (proposal.OperationInput, error) {
	workItemID, ok := trimmedStringArg(args, "work_item_id")
	if !ok {
		return nil, fmt.Errorf("%w: work_item_id is required", errInvalidArguments)
	}
	targetState, ok := trimmedStringArg(args, "target_state")
	if !ok {
		return nil, fmt.Errorf("%w: target_state is required", errInvalidArguments)
	}
	prov := ProposeProvenanceFromContext(ctx)
	return proposal.ProposeWorkItemLifecycleInput{
		TargetWorkItemID:  workItemID,
		TargetState:       targetState,
		Reason:            reason,
		Producer:          proposal.ProducerAI,
		AIRunID:           prov.AIRunID,
		SourceRevisionIDs: prov.SourceRevisionIDs,
	}, nil
}

// --- tracking.workflow_create -----------------------------------------------

func workflowCreateCapability() proposeCapabilityRegistration {
	return proposeCapabilityRegistration{
		descriptor: proposalcap.Descriptor{
			ID:                      proposal.OperationTypeWorkflowCreate,
			Summary:                 "Propose creating one immutable ordered-stage Workflow definition and attaching its Workflow instance to an existing Work Item that has no Workflow. Apply starts at the first stage and creates no Tracked Items or Planner Todos.",
			UseWhen:                 "Use when the user asks to define a process for a known existing Work Item and supplies the ordered stages. Do not use if the Work Item already has a Workflow or when the Work Item itself must also be created.",
			RequiresWorkItemContext: false,
			SourceInterpretEligible: true,
			Arguments: map[string]proposalcap.ArgumentSpec{
				"work_item_id":    {Type: proposalcap.ArgString, Required: true, Description: "The stable ID of the target Work Item."},
				"definition_name": {Type: proposalcap.ArgString, Required: true, Description: "A short name for the workflow/process definition."},
				"steps":           {Type: proposalcap.ArgStringList, Required: true, Description: "The ordered list of stage titles, e.g. [\"Development\", \"Review\", \"Production\"]."},
			},
		},
		handler: handleWorkflowCreate,
	}
}

func handleWorkflowCreate(ctx context.Context, a *App, base *assistant.WorkItemSnapshot, args map[string]any, reason *string, prep proposalPreparation) (proposal.OperationInput, error) {
	workItemID, ok := trimmedStringArg(args, "work_item_id")
	if !ok {
		return nil, fmt.Errorf("%w: work_item_id is required", errInvalidArguments)
	}
	definitionName, ok := trimmedStringArg(args, "definition_name")
	if !ok {
		return nil, fmt.Errorf("%w: definition_name is required", errInvalidArguments)
	}
	steps, ok := proposalcap.AsStringList(args, "steps")
	if !ok || len(steps) == 0 {
		return nil, fmt.Errorf("%w: steps must be a non-empty list", errInvalidArguments)
	}
	prov := ProposeProvenanceFromContext(ctx)
	return proposal.ProposeWorkflowCreateInput{
		TargetWorkItemID:  workItemID,
		DefinitionName:    definitionName,
		Steps:             steps,
		Reason:            reason,
		Producer:          proposal.ProducerAI,
		AIRunID:           prov.AIRunID,
		SourceRevisionIDs: prov.SourceRevisionIDs,
	}, nil
}

// --- work_item.create -------------------------------------------------------

func workItemCreateCapability() proposeCapabilityRegistration {
	return proposeCapabilityRegistration{
		descriptor: proposalcap.Descriptor{
			ID:                      proposal.OperationTypeWorkItemCreate,
			Summary:                 "Propose creating one new top-level Work Item outcome in ACTIVE lifecycle. Apply creates no Workflow, Tracked Items, Todo, reminder, Monitor Point, Attention, or Notification.",
			UseWhen:                 "Use when the user asks to create a new deliverable/project and does not also request its Workflow stages. Do not use for a personal Todo or when the combined Work Item plus Workflow capability matches the whole request.",
			RequiresWorkItemContext: false,
			SourceInterpretEligible: false,
			Arguments: map[string]proposalcap.ArgumentSpec{
				"title":       {Type: proposalcap.ArgString, Required: true, Description: "The title of the new Work Item."},
				"description": {Type: proposalcap.ArgString, Required: false, Description: "Optional description or objective of the Work Item."},
			},
		},
		handler: handleWorkItemCreate,
	}
}

func handleWorkItemCreate(ctx context.Context, a *App, base *assistant.WorkItemSnapshot, args map[string]any, reason *string, prep proposalPreparation) (proposal.OperationInput, error) {
	title, ok := trimmedStringArg(args, "title")
	if !ok {
		return nil, fmt.Errorf("%w: title is required", errInvalidArguments)
	}
	description := normalizeOptional(optionalStringArg(args, "description"))
	prov := ProposeProvenanceFromContext(ctx)
	return proposal.ProposeWorkItemCreateInput{
		Title:             title,
		Description:       description,
		Reason:            reason,
		Producer:          proposal.ProducerAI,
		AIRunID:           prov.AIRunID,
		SourceRevisionIDs: prov.SourceRevisionIDs,
	}, nil
}

// --- work_item.create_with_workflow -----------------------------------------

func workItemCreateWithWorkflowCapability() proposeCapabilityRegistration {
	return proposeCapabilityRegistration{
		descriptor: proposalcap.Descriptor{
			ID:                      proposal.OperationTypeWorkItemCreateWithWorkflow,
			Summary:                 "Propose atomically creating one new ACTIVE Work Item plus its immutable ordered-stage Workflow. Apply creates both as one reviewed operation and one Revision, but creates no initial Tracked Items, Todos, reminders, or Monitor Points.",
			UseWhen:                 "Use only when the user's single intent is to create a new deliverable and its Workflow stages together. Do not use for an existing Work Item or imply that requested tracked work/timing is also included.",
			RequiresWorkItemContext: false,
			SourceInterpretEligible: false,
			Arguments: map[string]proposalcap.ArgumentSpec{
				"title":         {Type: proposalcap.ArgString, Required: true, Description: "The title of the new Work Item."},
				"description":   {Type: proposalcap.ArgString, Required: false, Description: "Optional description or objective of the Work Item."},
				"workflow_name": {Type: proposalcap.ArgString, Required: true, Description: "A short name for the workflow definition."},
				"stages":        {Type: proposalcap.ArgStringList, Required: true, Description: "The ordered list of stage titles, e.g. [\"define\", \"develop\", \"test\", \"stage\", \"adoption\", \"production\"]."},
			},
		},
		handler: handleWorkItemCreateWithWorkflow,
	}
}

func handleWorkItemCreateWithWorkflow(ctx context.Context, a *App, base *assistant.WorkItemSnapshot, args map[string]any, reason *string, prep proposalPreparation) (proposal.OperationInput, error) {
	title, ok := trimmedStringArg(args, "title")
	if !ok {
		return nil, fmt.Errorf("%w: title is required", errInvalidArguments)
	}
	workflowName, ok := trimmedStringArg(args, "workflow_name")
	if !ok {
		return nil, fmt.Errorf("%w: workflow_name is required", errInvalidArguments)
	}
	stages, ok := proposalcap.AsStringList(args, "stages")
	if !ok || len(stages) == 0 {
		return nil, fmt.Errorf("%w: stages must be a non-empty list", errInvalidArguments)
	}
	description := normalizeOptional(optionalStringArg(args, "description"))
	prov := ProposeProvenanceFromContext(ctx)
	return proposal.ProposeWorkItemCreateWithWorkflowInput{
		Title:             title,
		Description:       description,
		WorkflowName:      workflowName,
		Stages:            stages,
		Reason:            reason,
		Producer:          proposal.ProducerAI,
		AIRunID:           prov.AIRunID,
		SourceRevisionIDs: prov.SourceRevisionIDs,
	}, nil
}

// --- monitor_point.create ---------------------------------------------------

func monitorPointCreateCapability() proposeCapabilityRegistration {
	return proposeCapabilityRegistration{
		descriptor: proposalcap.Descriptor{
			ID:                      proposal.OperationTypeMonitorPointCreate,
			Summary:                 "Propose one authoritative deadline/checkpoint expectation on an existing Work Item or Tracked Item. Apply creates the Monitor Point only; background evaluation may later derive Attention and Notification if it becomes overdue and unmet.",
			UseWhen:                 "Use when the user wants the product to monitor an outcome or tracked unit against a due expectation. Do not use for a personal 'remind me' action (use a due Planner Todo with reminder), and do not create Attention or Notification directly.",
			RequiresWorkItemContext: false,
			SourceInterpretEligible: false,
			Arguments: map[string]proposalcap.ArgumentSpec{
				"work_item_id": {Type: proposalcap.ArgString, Required: false, Description: "The stable ID of the Work Item. Defaults to the currently selected Work Item if omitted."},
				"title":        {Type: proposalcap.ArgString, Required: true, Description: "The title or description of this monitoring target."},
				"kind": {
					Type: proposalcap.ArgEnum, Required: false, Description: "The monitor point kind. Defaults to DEADLINE if omitted.",
					EnumValues: []string{"FINAL_TARGET", "CHECKPOINT", "DEADLINE", "REVIEW", "CUSTOM"},
				},
				"due_date":            {Type: proposalcap.ArgDate, Required: false, Description: "Target calendar date (YYYY-MM-DD)."},
				"due_time":            {Type: proposalcap.ArgString, Required: false, Description: "Target clock time (e.g. '17:00' or '5:00 PM'). Defaults to 17:00 if omitted when date is given."},
				"due_at":              {Type: proposalcap.ArgString, Required: false, Description: "Exact RFC3339 timestamp (e.g. '2026-10-15T17:00:00Z'). Used if due_date/due_time are omitted."},
				"timezone":            {Type: proposalcap.ArgString, Required: false, Description: "Timezone name (e.g. 'America/New_York', 'Eastern', 'UTC'). Defaults to host local timezone if omitted."},
				"subject_item_title":  {Type: proposalcap.ArgString, Required: false, Description: "Title of the tracked item if monitoring a specific item. Omit to monitor the Work Item itself."},
				"expected_step_title": {Type: proposalcap.ArgString, Required: false, Description: "Workflow stage title or key that must be completed (required if subject_item_title is provided)."},
				"expected_lifecycle": {
					Type: proposalcap.ArgEnum, Required: false, Description: "Expected WorkItem lifecycle state (used when monitoring the Work Item itself). Defaults to COMPLETED.",
					EnumValues: []string{"ACTIVE", "PAUSED", "COMPLETED", "CANCELLED"},
				},
			},
		},
		handler: handleMonitorPointCreate,
	}
}

func handleMonitorPointCreate(ctx context.Context, a *App, base *assistant.WorkItemSnapshot, args map[string]any, reason *string, prep proposalPreparation) (proposal.OperationInput, error) {
	var workItemID string
	if wid, ok := trimmedStringArg(args, "work_item_id"); ok && wid != "none" && wid != "null" {
		workItemID = wid
	} else if base != nil && base.ID != "" {
		workItemID = base.ID
	} else {
		return nil, fmt.Errorf("%w: work_item_id is required (specify work_item_id or select an item)", errInvalidArguments)
	}

	title, ok := trimmedStringArg(args, "title")
	if !ok {
		return nil, fmt.Errorf("%w: title is required", errInvalidArguments)
	}

	kindStr := "DEADLINE"
	if k, ok := trimmedStringArg(args, "kind"); ok {
		kindStr = strings.ToUpper(k)
	}
	if !tracking.MonitorKind(kindStr).Valid() {
		return nil, fmt.Errorf("%w: unsupported monitor kind %q", errInvalidArguments, kindStr)
	}

	dueDateStr := optionalStringArg(args, "due_date")
	dueTimeStr := optionalStringArg(args, "due_time")
	tzStr := optionalStringArg(args, "timezone")
	dueAtStr := optionalStringArg(args, "due_at")

	loc, err := resolveLocation(tzStr, time.Local)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errInvalidArguments, err)
	}
	tzName := loc.String()

	var dueAt time.Time
	if dueAtStr != nil && *dueAtStr != "" {
		t, err := time.Parse(time.RFC3339, *dueAtStr)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid due_at: %v", errInvalidArguments, err)
		}
		dueAt = t.UTC()
	} else if dueDateStr != nil && *dueDateStr != "" {
		timePart := "17:00"
		if dueTimeStr != nil && *dueTimeStr != "" {
			timePart = *dueTimeStr
		}
		combined, err := combineDateAndTime(*dueDateStr, timePart, tzStr, loc)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errInvalidArguments, err)
		}
		t, err := time.Parse(time.RFC3339, combined)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errInvalidArguments, err)
		}
		dueAt = t.UTC()
	} else {
		return nil, fmt.Errorf("%w: due_date or due_at is required", errInvalidArguments)
	}

	trackingView, err := a.trackingService.GetWorkItemTracking(ctx, workItemID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", proposal.ErrTargetUnavailable, err)
	}

	input := proposal.ProposeMonitorPointCreateInput{
		WorkItemID: workItemID,
		Kind:       kindStr,
		Title:      title,
		DueAt:      dueAt,
		Timezone:   tzName,
		Reason:     reason,
		Producer:   proposal.ProducerAI,
	}

	if subjectItemTitle, ok := trimmedStringArg(args, "subject_item_title"); ok {
		item, err := findTrackedItemByTitle(trackingView, subjectItemTitle)
		if err != nil {
			return nil, err
		}
		expectedStepTitle, ok := trimmedStringArg(args, "expected_step_title")
		if !ok {
			return nil, fmt.Errorf("%w: expected_step_title is required when subject_item_title is specified", errInvalidArguments)
		}
		step, err := findWorkflowStepByTitle(trackingView, expectedStepTitle)
		if err != nil {
			return nil, err
		}
		input.SubjectTrackedItemID = &item.ID
		input.SubjectTrackedItemTitle = &item.Title
		input.ExpectedStepID = &step.ID
		input.ExpectedStepTitle = &step.Title
	} else {
		lifecycle := "COMPLETED"
		if l, ok := trimmedStringArg(args, "expected_lifecycle"); ok {
			lifecycle = strings.ToUpper(l)
		}
		if !workitem.LifecycleState(lifecycle).Valid() {
			return nil, fmt.Errorf("%w: unsupported lifecycle state %q", errInvalidArguments, lifecycle)
		}
		input.ExpectedWorkItemLifecycle = &lifecycle
	}

	prov := ProposeProvenanceFromContext(ctx)
	input.AIRunID = prov.AIRunID
	input.SourceRevisionIDs = prov.SourceRevisionIDs

	return input, nil
}

// --- monitor_point.set_state ------------------------------------------------

func monitorPointSetStateCapability() proposeCapabilityRegistration {
	return proposeCapabilityRegistration{
		descriptor: proposalcap.Descriptor{
			ID:                      proposal.OperationTypeMonitorPointSetState,
			Summary:                 "Propose transitioning one existing ACTIVE Monitor Point to SATISFIED or CANCELLED. Apply changes the expectation state; later reconciliation resolves related derived Attention without deleting prior Notifications.",
			UseWhen:                 "Use when the user explicitly marks a known monitoring expectation met or cancelled. Do not use to complete a Work Item, Tracked Item, or Planner Todo, or merely to dismiss a Notification.",
			RequiresWorkItemContext: false,
			SourceInterpretEligible: false,
			Arguments: map[string]proposalcap.ArgumentSpec{
				"work_item_id":        {Type: proposalcap.ArgString, Required: false, Description: "Optional: the stable ID of the parent Work Item if known or selected."},
				"monitor_point_id":    {Type: proposalcap.ArgString, Required: false, Description: "The stable ID of the monitor point. If omitted, monitor_point_title is used."},
				"monitor_point_title": {Type: proposalcap.ArgString, Required: false, Description: "The title of the monitor point to transition."},
				"target_state": {
					Type: proposalcap.ArgEnum, Required: true, Description: "The target state.",
					EnumValues: []string{"SATISFIED", "CANCELLED"},
				},
			},
		},
		handler: handleMonitorPointSetState,
	}
}

func handleMonitorPointSetState(ctx context.Context, a *App, base *assistant.WorkItemSnapshot, args map[string]any, reason *string, prep proposalPreparation) (proposal.OperationInput, error) {
	targetState, ok := trimmedStringArg(args, "target_state")
	if !ok {
		return nil, fmt.Errorf("%w: target_state is required", errInvalidArguments)
	}
	targetState = strings.ToUpper(targetState)
	if targetState != string(tracking.MonitorSatisfied) && targetState != string(tracking.MonitorCancelled) {
		return nil, fmt.Errorf("%w: target_state must be SATISFIED or CANCELLED, got %q", errInvalidArguments, targetState)
	}

	var monitorID string
	if mid, ok := trimmedStringArg(args, "monitor_point_id"); ok && mid != "none" && mid != "null" {
		monitorID = mid
	} else if title, ok := trimmedStringArg(args, "monitor_point_title"); ok {
		var workItemID string
		if wid, ok := trimmedStringArg(args, "work_item_id"); ok && wid != "none" && wid != "null" {
			workItemID = wid
		} else if base != nil && base.ID != "" {
			workItemID = base.ID
		} else {
			return nil, fmt.Errorf("%w: work_item_id or selected work item is required to resolve monitor_point_title", errInvalidArguments)
		}
		trackingView, err := a.trackingService.GetWorkItemTracking(ctx, workItemID)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", proposal.ErrTargetUnavailable, err)
		}
		pt, err := findMonitorPointByTitle(trackingView, title)
		if err != nil {
			return nil, err
		}
		monitorID = pt.ID
	} else {
		return nil, fmt.Errorf("%w: monitor_point_id or monitor_point_title is required", errInvalidArguments)
	}

	pt, err := a.trackingService.GetMonitorPoint(ctx, monitorID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", proposal.ErrTargetUnavailable, err)
	}
	if string(pt.State) == targetState {
		return nil, errCapabilityNoChange
	}

	prov := ProposeProvenanceFromContext(ctx)
	return proposal.ProposeMonitorPointSetStateInput{
		MonitorPointID:    monitorID,
		TargetState:       targetState,
		Reason:            reason,
		Producer:          proposal.ProducerAI,
		AIRunID:           prov.AIRunID,
		SourceRevisionIDs: prov.SourceRevisionIDs,
	}, nil
}

// --- tracking.workflow_set_current_step -------------------------------------

func workflowSetCurrentStepCapability() proposeCapabilityRegistration {
	return proposeCapabilityRegistration{
		descriptor: proposalcap.Descriptor{
			ID:                      proposal.OperationTypeWorkflowSetCurrentStep,
			Summary:                 "Propose moving an existing Workflow's persisted overall current step forward or backward. Apply changes only the Workflow position; it does not alter Tracked Item stage states, Work Item lifecycle, Todos, or reminders.",
			UseWhen:                 "Use when the user asks to move the overall process/deliverable to a named Workflow stage. Do not use when the request targets one Tracked Item's state.",
			RequiresWorkItemContext: false,
			SourceInterpretEligible: false,
			Arguments: map[string]proposalcap.ArgumentSpec{
				"work_item_id": {Type: proposalcap.ArgString, Required: false, Description: "The stable ID of the Work Item. Defaults to currently selected Work Item if omitted."},
				"step_title":   {Type: proposalcap.ArgString, Required: true, Description: "The title or key of the workflow stage to transition into."},
			},
		},
		handler: handleWorkflowSetCurrentStep,
	}
}

func handleWorkflowSetCurrentStep(ctx context.Context, a *App, base *assistant.WorkItemSnapshot, args map[string]any, reason *string, prep proposalPreparation) (proposal.OperationInput, error) {
	var workItemID string
	if wid, ok := trimmedStringArg(args, "work_item_id"); ok && wid != "none" && wid != "null" {
		workItemID = wid
	} else if base != nil && base.ID != "" {
		workItemID = base.ID
	} else {
		return nil, fmt.Errorf("%w: work_item_id is required (specify work_item_id or select an item)", errInvalidArguments)
	}

	stepTitleOrKey, ok := trimmedStringArg(args, "step_title")
	if !ok {
		return nil, fmt.Errorf("%w: step_title is required", errInvalidArguments)
	}

	view, err := loadTrackingView(ctx, a, workItemID)
	if err != nil {
		return nil, err
	}
	step, err := findWorkflowStepByTitle(view, stepTitleOrKey)
	if err != nil {
		return nil, err
	}

	if view.Workflow.Workflow.CurrentStepID == step.ID {
		return nil, errCapabilityNoChange
	}

	prov := ProposeProvenanceFromContext(ctx)
	return proposal.ProposeWorkflowSetCurrentStepInput{
		WorkItemID:        workItemID,
		WorkflowID:        view.Workflow.Workflow.ID,
		TargetStepID:      step.ID,
		TargetStepTitle:   step.Title,
		Reason:            reason,
		Producer:          proposal.ProducerAI,
		AIRunID:           prov.AIRunID,
		SourceRevisionIDs: prov.SourceRevisionIDs,
	}, nil
}

// --- planner.item.set_local_note -------------------------------------------

func plannerItemSetLocalNoteCapability() proposeCapabilityRegistration {
	return proposeCapabilityRegistration{
		descriptor: proposalcap.Descriptor{
			ID:                      proposal.OperationTypePlannerItemSetLocalNote,
			Summary:                 "Propose adding or replacing the Lum Harbor note on one existing externally synced Planner item (for example an Apple Reminder). Apply changes only that local note on the same item; the external title, notes, due date, and completion stay owned by the external app and are never changed.",
			UseWhen:                 "Use when the user asks to add a note, description, or context to an existing synced/external Planner item. Identify it by planner_item_id (resolve via entity.search) or by its exact planner_item_title. Do not create a new Todo for this, and do not use it to rename, reschedule, or complete an item.",
			RequiresWorkItemContext: false,
			SourceInterpretEligible: false,
			Arguments: map[string]proposalcap.ArgumentSpec{
				"planner_item_id":    {Type: proposalcap.ArgString, Required: false, Description: "The stable ID of the existing Planner item. Preferred when known."},
				"planner_item_title": {Type: proposalcap.ArgString, Required: false, Description: "The existing Planner item's title, used when planner_item_id is unknown. Must identify exactly one item."},
				"local_note":         {Type: proposalcap.ArgString, Required: true, Description: "The note text to attach in Lum Harbor."},
				"mode": {
					Type: proposalcap.ArgEnum, Required: false, Description: "append (default) adds the text after any existing Lum Harbor note; replace overwrites it.",
					EnumValues: []string{"append", "replace"},
				},
			},
		},
		handler: handlePlannerItemSetLocalNote,
	}
}

func handlePlannerItemSetLocalNote(ctx context.Context, a *App, _ *assistant.WorkItemSnapshot, args map[string]any, reason *string, _ proposalPreparation) (proposal.OperationInput, error) {
	if a.plannerService == nil {
		return nil, fmt.Errorf("%w: planner service is not initialized", errInvalidArguments)
	}
	text, ok := trimmedStringArg(args, "local_note")
	if !ok {
		return nil, fmt.Errorf("%w: local_note must not be blank", errInvalidArguments)
	}
	item, err := resolvePlannerItemTarget(ctx, a, args)
	if err != nil {
		return nil, err
	}
	if item.Authority != planner.AuthorityExternal {
		return nil, fmt.Errorf("%w: %q is a Lum Harbor Todo; local notes apply only to externally synced Planner items", errInvalidArguments, item.Title)
	}

	note := text
	current := planner.NormalizeLocalNote(item.LocalNote)
	if mode, _ := trimmedStringArg(args, "mode"); mode != "replace" && current != nil {
		if strings.Contains(*current, text) {
			return nil, errCapabilityNoChange
		}
		note = *current + "\n\n" + text
	}
	if current != nil && *current == note {
		return nil, errCapabilityNoChange
	}

	prov := ProposeProvenanceFromContext(ctx)
	return proposal.ProposePlannerItemSetLocalNoteInput{
		PlannerItemID:     item.ID,
		LocalNote:         &note,
		Reason:            reason,
		Producer:          proposal.ProducerAI,
		AIRunID:           prov.AIRunID,
		SourceRevisionIDs: prov.SourceRevisionIDs,
	}, nil
}

// resolvePlannerItemTarget resolves one existing Planner item by stable ID or,
// failing that, by title: an exact case-insensitive title match wins, else a
// single substring match. Anything else is reported rather than guessed.
func resolvePlannerItemTarget(ctx context.Context, a *App, args map[string]any) (*planner.PlannerItem, error) {
	if id, ok := trimmedStringArg(args, "planner_item_id"); ok && id != "none" && id != "null" {
		item, err := a.plannerService.GetByID(ctx, id)
		if err == nil {
			return item, nil
		}
		if _, hasTitle := trimmedStringArg(args, "planner_item_title"); !hasTitle {
			return nil, fmt.Errorf("%w: planner item %s: %v", proposal.ErrTargetUnavailable, id, err)
		}
	}
	title, ok := trimmedStringArg(args, "planner_item_title")
	if !ok {
		return nil, fmt.Errorf("%w: planner_item_id or planner_item_title is required", errInvalidArguments)
	}
	matches, err := a.plannerService.SearchByTitle(ctx, title, 20)
	if err != nil {
		return nil, fmt.Errorf("resolve planner item %q: %w", title, err)
	}
	var exact []planner.PlannerItem
	for _, item := range matches {
		if strings.EqualFold(strings.TrimSpace(item.Title), title) {
			exact = append(exact, item)
		}
	}
	switch {
	case len(exact) == 1:
		return &exact[0], nil
	case len(exact) > 1:
		return nil, fmt.Errorf("%w: %d Planner items are titled %q; specify planner_item_id", errInvalidArguments, len(exact), title)
	case len(matches) == 1:
		return &matches[0], nil
	case len(matches) == 0:
		return nil, fmt.Errorf("%w: no Planner item matches %q", proposal.ErrTargetUnavailable, title)
	default:
		return nil, fmt.Errorf("%w: %d Planner items match %q; specify planner_item_id", errInvalidArguments, len(matches), title)
	}
}
