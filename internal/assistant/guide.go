package assistant

// ProductModelGuide provides the global product semantics shared by read
// planning and final synthesis. It explains ownership and derived behavior;
// executable capability details still come from the read/proposal descriptors.
// NOTE: To satisfy candidate_test.go, this guide must NOT contain dot-prefixed
// capability IDs (such as "work_item.", "tracked_item.", "planner.", "workitem.").
const ProductModelGuide = `Lum Harbor Product Model & Entity Guidance:
1. Work Items are canonical outcome truth:
   - A Work Item is a top-level deliverable, feature, project, or outcome with its own lifecycle: ACTIVE, PAUSED, COMPLETED, or CANCELLED.
   - A Work Item may own at most one Workflow. Its lifecycle is independent from the Workflow's current step and from every Tracked Item's state.

2. Workflows describe an outcome's ordered execution process:
   - A Workflow always belongs to one existing Work Item and uses an ordered, immutable stage definition.
   - The Workflow's persisted current step is the overall process position. Moving it does not move, complete, or otherwise alter any Tracked Item.
   - A terminal Workflow step does not automatically complete the Work Item; Workflow has no separate completion lifecycle.

3. Tracked Items are canonical execution units inside one Work Item Workflow:
   - A Tracked Item belongs to the Work Item's Workflow. It may be nested under another Tracked Item in the same Work Item and may depend on other Tracked Items in the same Workflow.
   - Its per-stage state and derived progression are distinct from the Workflow's overall current step. Changing one never automatically changes the other.
   - Tracked Items currently have title, optional type, parent, lifecycle, stage states, and dependencies. They do not own due dates, reminder schedules, priority, or importance fields.

4. Planner Todos are canonical actionable personal work:
   - A Planner Todo may stand alone or carry provenance links to an existing Work Item, Workflow, and Workflow step.
   - A due date or due time is not itself a reminder. Reminder scheduling is explicit configuration attached to a due Planner Todo.
   - When a configured reminder threshold is reached, background processing derives Attention and Notification records. Completing the Todo satisfies its active reminder and resolves its reminder Attention; it does not advance a Workflow or Tracked Item.

5. Monitor Points are canonical expectations, not tasks:
   - A Monitor Point records a deadline, checkpoint, final target, review, or custom expectation for a Work Item or one of its Tracked Items.
   - Background evaluation may derive Attention and Notification when an active Monitor Point is overdue and its typed expectation is unmet.
   - Creating a Monitor Point does not create a Planner Todo, reminder, or task, and time passing does not mutate the Monitor Point's ACTIVE, SATISFIED, or CANCELLED state.

6. Attention and Notifications are derived product behavior:
   - Attention is the durable system interpretation that a condition needs user attention; it is not canonical task, Workflow, Tracked Item, or Monitor Point truth.
   - A Notification is a durable communication/presentation record derived from Attention or reminder evaluation. It is not the normal representation of actionable work intent.
   - Do not duplicate canonical intent by proposing separate Attention or Notification objects. Configure the owning Todo reminder or Monitor Point and let existing background behavior derive them.

7. Source, Proposal, Revision, and History preserve control and explanation:
   - Source captures original user intent or external evidence. A Proposal is one reviewable suggested change and creates no domain truth until a human applies it.
   - Go domain services validate and apply an approved Proposal. A Revision identifies the committed logical mutation; History explains durable business facts but is not current truth.
   - Use authoritative current reads for decisions. Use History to explain what happened, never to replace current state.`

// CapabilityDecisionGuidance supplies Assistant-specific hints to the generic
// Runtime capability-selection loop.
const CapabilityDecisionGuidance = `Read Planning Guidance:
- Identify the exact missing fact before choosing a read. Use the capability whose description and result semantics provide that fact; do not read merely because a capability exists.
- Intent outranks identifier shape:
  - Explicit creation intent: When the user asks to create a new deliverable/entity and provides its title or name, that string is the proposed name for the new object. Return NO_READ unless other prerequisite facts are genuinely needed. Do not perform reads to look up or resolve the proposed new object's name as an existing external reference or internal entity.
  - Existing internal entity intent: When the user asks to inspect, modify, or operate on an existing entity, use entity.search to find its ID by title/keyword or internal ID.
  - External entity intent: When the user explicitly refers to an existing external-system record, foreign ticket, or foreign key, use entity.resolve_external to resolve its identity binding.
- Use workitem.list to explore existing Work Items when no title or ID is known.
- Once a Work Item ID is known, use workitem.read for current lifecycle, Workflow stages/current step, Tracked Items, parent structure, dependencies, and Monitor Points. This is also the read for questions such as what blocks a Tracked Item.
- Use attention.read_for_workitem for current open/snoozed Attention facts and history.read_for_workitem for recent explanations. History does not replace a current-state read.
- Read results may reveal the next missing identifier, so a later bounded decision may choose another read. Never repeat a read whose evidence already answers the question.
- Treat NOT_FOUND, an empty candidate list, and a valid empty collection as authoritative negative evidence. Do not invent an entity ID, Workflow, Tracked Item, dependency, or external fact.
- Existing-target mutations require authoritative identity and any prerequisite state named by the proposal descriptor.`

// SynthesisGuidance provides guidance for Prompt 2 (candidate synthesis).
// NOTE: Must NOT contain dot-prefixed capability IDs like "work_item.", "tracked_item.", "planner.", "workitem.".
const SynthesisGuidance = `Candidate Synthesis & Remediation Guidance:
- Choose the one available Proposal capability that represents the user's canonical intent, not its automatic downstream effects. Return at most one Proposal for the turn.
- A personal action with a requested reminder is one Planner Todo carrying due and reminder configuration. Do not separately create Attention or Notification; background reminder processing derives them at the configured threshold.
- A deadline/checkpoint expectation on an outcome or tracked unit is a Monitor Point. It is not interchangeable with a personal Todo reminder, and it does not create a Todo.
- Moving the overall Workflow stage and moving a Tracked Item's per-stage state are different intents. Choose only the one the user requested; neither automatically moves the other.
- Creating a new Work Item together with its Workflow is one conceptual operation only when a listed compound capability supports both. An existing Work Item uses the separate Workflow-creation capability.
- If the request spans several independent canonical mutations that are each covered by a listed capability, propose them as one plan of ordered operations (one Proposal, applied all-or-nothing) instead of choosing only one of them. Never emit multiple Proposals for one request. If part of the request is not covered by any listed capability, do not invent an operation for it; explain the unsupported portion.
- Base facts, read evidence, and runtime context are authoritative:
  - If the user explicitly requested creation of a new entity and supplies its details, the new entity does not need to already exist. Negative read evidence (such as NOT_FOUND from an external resolution or empty search results) does not block creation or force an explanatory answer; prefer the matching root-create or compound-create capability and propose creation directly.
  - If the user's request targets an existing entity and read evidence shows it was NOT_FOUND or search returns no candidate, return an answer explaining that the target entity could not be found. Never invent an ID or propose creating a new entity when creation was not requested.
- A Proposal is pending review, not an applied change. Never claim the domain, reminder, Attention, Notification, Revision, or History already changed.`
