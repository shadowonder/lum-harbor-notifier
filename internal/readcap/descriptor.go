// Package readcap is the M15 AI/Plugin Readiness Foundation's Read
// Capability boundary: a small, non-domain, non-AI application layer that
// lets a future caller (M16) discover a permitted set of read-only
// capabilities and execute exactly one of them at a time, using only a
// capability ID and plain JSON — never a capability-specific Go type. It
// owns no truth, opens no transaction, and never mutates anything.
//
// Two origins of capability exist. CORE (Slice 1) delegates directly to an
// existing, already-tested domain read seam (internal/workitem,
// internal/tracking, internal/planner, internal/foundation). PLUGIN (Slice
// 2) delegates to a resolved Plugin's read.query capability through the
// narrow PluginReadSource interface, satisfied by
// internal/plugin/readadapter — this package never imports internal/plugin
// itself.
//
// This is deliberately not a generic tool-calling framework: there is no
// reflection, no JSON Schema library, no plugin-style capability registry
// beyond one PluginReadSource, and no permissions DSL. Adding a CORE
// capability means writing one more Go function in this package and one
// more Descriptor entry, exactly like the three below.
package readcap

// Origin distinguishes where a capability's implementation lives. CORE
// (Slice 1) is backed directly by a Go domain service in this repository.
// PLUGIN (Slice 2) is backed by a Plugin's declared read.query capability.
type Origin string

// OriginCore marks a capability implemented directly by a Core Go domain
// service in this repository.
const OriginCore Origin = "CORE"

// OriginPlugin marks a capability backed by a Plugin's declared read.query
// capability, invoked through internal/plugin/readadapter (M15 Slice 2).
const OriginPlugin Origin = "PLUGIN"

// Capability IDs are stable strings, not Go types, so a future caller (a
// tool-calling loop, a config file, a Plugin manifest) can name one without
// importing this package's Go symbols.
const (
	// CapabilityWorkItemRead reads one authoritative WorkItem plus its
	// bounded Tracking summary. See WorkItemReadInput/WorkItemReadOutput.
	CapabilityWorkItemRead = "workitem.read"
	// CapabilityResolveExternal deterministically resolves one external
	// system identity to its bound Lum Harbor entity. See
	// ResolveExternalInput/ResolveExternalOutput.
	CapabilityResolveExternal = "entity.resolve_external"
	// CapabilitySearch performs a bounded candidate search across
	// user-facing searchable entities. See SearchInput/SearchOutput.
	CapabilitySearch = "entity.search"
	// CapabilityAttentionReadForWorkItem reads active Attention issues and severity rank
	// for a specific WorkItem. See AttentionReadForWorkItemInput/AttentionReadForWorkItemOutput.
	CapabilityAttentionReadForWorkItem = "attention.read_for_workitem"
	// CapabilityHistoryReadForWorkItem reads recent authoritative history events
	// for a specific WorkItem. See HistoryReadForWorkItemInput/HistoryReadForWorkItemOutput.
	CapabilityHistoryReadForWorkItem = "history.read_for_workitem"
	// CapabilityWorkItemList lists or overviews existing WorkItems.
	// See WorkItemListInput/WorkItemListOutput.
	CapabilityWorkItemList = "workitem.list"
)

// Descriptor is the machine-readable semantics of one permitted read
// capability: what it is, what it expects, and what it returns, in plain
// text a future caller (human, config, or model) can use to decide whether
// and how to call it. It carries no executable value — Descriptor is data,
// Execute is behavior.
type Descriptor struct {
	// ID is the stable capability identifier passed in Request.CapabilityID.
	ID string
	// Description is a short, human-readable summary of what this
	// capability does.
	Description string
	// InputSemantics describes the shape and validation rules of the Go
	// type Execute expects in Request.Input for this capability.
	InputSemantics string
	// ResultSemantics describes the shape and guarantees of the Go type
	// Execute returns in Result.Output for this capability.
	ResultSemantics string
	// Origin is CORE for every Slice 1 capability.
	Origin Origin
}

// descriptors is the fixed, deterministic Slice 1 CORE capability set.
// Order is stable and is itself part of the discovery contract: callers may
// rely on ListDescriptors returning capabilities in this exact order.
var descriptors = []Descriptor{
	{
		ID:              CapabilityWorkItemRead,
		Description:     "Read current authoritative truth for one known Work Item: content/lifecycle plus its Workflow definition and overall current step, Tracked Items with parent/progression facts, same-Workflow dependencies, and Monitor Points exposed by the bounded Tracking view. Use after resolving a Work Item ID, including to answer which Tracked Item blocks another. This does not read Planner Todos, Attention, Notifications, or History.",
		InputSemantics:  `JSON object {"workItemId": string}. workItemId is a required, non-empty stable internal Work Item entity ID; resolve a title or external identifier first.`,
		ResultSemantics: "WorkItemReadOutput{WorkItem, Tracking}. WorkItem is current content/lifecycle truth. Tracking is nil when the Work Item has no Workflow; otherwise it contains the Workflow's overall current step, ordered steps, Tracked Item hierarchy and derived progression, dependency edges, and Monitor Points. Workflow current step and Tracked Item progression are independent. Unknown or archived Work Items return ErrEntityNotFound, never invented or zero-value truth.",
		Origin:          OriginCore,
	},
	{
		ID:              CapabilityResolveExternal,
		Description:     "Resolve one exact external-system record identifier to its bound Lum Harbor entity ID and kind. Use only when the user refers to an existing external-system record, foreign ticket, or foreign reference to locate an existing bound entity. Do not use for human titles, keywords, or the proposed name/title of a new entity being created.",
		InputSemantics:  `JSON object {"externalId": string, "system": string (optional)}. externalId is required and non-empty. system narrows the owning external system; omit it only when the external ID is expected to be unique across systems.`,
		ResultSemantics: "ResolveExternalOutput{EntityID, EntityType}: one exact identity binding only, not entity content. No binding returns ErrEntityNotFound and ambiguity fails closed; this capability never falls back to title search. Use the returned internal ID in a later domain read when more facts are needed.",
		Origin:          OriginCore,
	},
	{
		ID:              CapabilitySearch,
		Description:     "Find candidate Work Items or Planner Todos by an exact internal entity ID or literal case-insensitive title substring. Use when the user names an existing item but no stable internal ID is known. Do not use for external-system identifiers or as a full domain read.",
		InputSemantics:  `JSON object {"query": string, "limit": integer (optional)}. query is required, non-empty, and limited to 200 characters. limit defaults to 10 and is capped at 20.`,
		ResultSemantics: "SearchOutput{Candidates}: a bounded deterministic list of {EntityID, EntityKind, DisplayLabel, MatchHint}; an exact internal-ID match is first. Candidates are identity hints, not full current truth. An empty list is authoritative negative evidence for this search and must not be replaced with an invented ID.",
		Origin:          OriginCore,
	},
	{
		ID:              CapabilityAttentionReadForWorkItem,
		Description:     "Read current Attention issues for one known Work Item to explain what needs attention, including overdue Monitor Point and Planner-reminder-derived conditions. Use for current warning/severity/disposition questions. Attention is derived state; this does not read or mutate its canonical Todo, reminder, Monitor Point, Workflow, or Tracked Item truth.",
		InputSemantics:  `JSON object {"workItemId": string}. workItemId is a required, non-empty stable internal Work Item ID; resolve the target first.`,
		ResultSemantics: "AttentionReadForWorkItemOutput{WorkItemID, OpenIssueCount, ActionableIssueCount, SnoozedIssueCount, HighestActiveSeverity, Issues}: current bounded issue summaries with type, severity, disposition, reason, recommended action, and opened time. A valid empty Issues list means no current issue was found; it does not prove the underlying work is complete.",
		Origin:          OriginCore,
	},
	{
		ID:              CapabilityHistoryReadForWorkItem,
		Description:     "Read recent durable business events for one known Work Item to explain what happened and who or what acted. Use for timeline or recent-change questions after resolving the Work Item. History is explanatory and must not be treated as the Work Item's current state.",
		InputSemantics:  `JSON object {"workItemId": string}. workItemId is a required, non-empty stable internal Work Item ID. Resolve titles or external references before calling.`,
		ResultSemantics: "HistoryReadForWorkItemOutput{WorkItemID, Events, Truncated}: at most 10 newest event summaries with type, summary, actor, and occurrence time. Truncated reports omitted older facts. Empty Events is valid negative evidence; use a current-state capability rather than inferring current truth from history.",
		Origin:          OriginCore,
	},
	{
		ID:              CapabilityWorkItemList,
		Description:     "List a bounded overview of existing Work Items when the user asks what projects/outcomes exist and provides no particular title or ID. Do not use for title matching or for Workflow, Tracked Item, Planner, Attention, or History details.",
		InputSemantics:  `JSON object {"limit": integer (optional)}. limit must be positive, defaults to 20, and is capped at 50. An empty object is valid.`,
		ResultSemantics: "WorkItemListOutput{Items, Total}: up to the requested bound of {ID, Title, Lifecycle, CreatedAt, UpdatedAt}; Total reports the full current count. An empty Items list with Total zero is authoritative valid-empty evidence. Follow with workitem.read only when one returned Work Item needs details.",
		Origin:          OriginCore,
	},
}
