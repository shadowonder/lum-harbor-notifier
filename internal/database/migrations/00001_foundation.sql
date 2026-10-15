-- +goose Up
-- entities is Lum Harbor's global identity registry.
--
-- Business tables such as work_items and planner_items will use an
-- entity ID from this table instead of inventing unrelated primary-key
-- schemes for every domain object.
CREATE TABLE entities (
    id TEXT PRIMARY KEY,
    entity_type TEXT NOT NULL,
    created_at TEXT NOT NULL,
    archived_at TEXT
);

-- revisions represents one successful logical mutation batch.
--
-- It is not a snapshot or rollback system. Later, when the user applies
-- a Proposal or manually changes domain state, the resulting related
-- mutations can share one revision ID for traceability.
CREATE TABLE revisions (
    revision_no INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    origin_type TEXT NOT NULL CHECK (
        origin_type IN (
            'USER',
            'AI',
            'SYNC',
            'IMPORT',
            'SYSTEM'
        )
    ),
    summary TEXT,
    applied_at TEXT NOT NULL
);

CREATE TABLE external_refs (
    id TEXT PRIMARY KEY,
    entity_id TEXT NOT NULL,
    system TEXT NOT NULL,
    external_id TEXT NOT NULL,
    relationship_type TEXT NOT NULL CHECK (
        relationship_type IN (
            'AUTHORITATIVE',
            'MIRROR'
        )
    ),
    last_seen_at TEXT,
    FOREIGN KEY (entity_id) REFERENCES entities(id),
    UNIQUE(system, external_id)
);

-- ============================================================
-- Source Layer
-- ============================================================
-- source_records is the stable identity and current head for one external
-- object. Full payload history belongs to source_revisions.
CREATE TABLE source_records (
    id TEXT PRIMARY KEY CHECK (length(trim(id)) > 0),
    extension_id TEXT NOT NULL CHECK (length(trim(extension_id)) > 0),
    instance_id TEXT NOT NULL CHECK (length(trim(instance_id)) > 0),
    source_kind TEXT NOT NULL CHECK (length(trim(source_kind)) > 0),
    external_id TEXT NOT NULL CHECK (length(trim(external_id)) > 0),
    current_revision_id TEXT,
    current_content_hash TEXT,
    source_updated_at TEXT,
    first_seen_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(extension_id, instance_id, source_kind, external_id),
    CHECK (
        (current_revision_id IS NULL AND current_content_hash IS NULL)
        OR
        (current_revision_id IS NOT NULL AND current_content_hash IS NOT NULL)
    ),
    CHECK (current_content_hash IS NULL OR length(current_content_hash) = 64)
);

-- source_revisions is immutable observation history. Repeated hashes are
-- intentionally allowed because an A -> B -> A sequence is three changes.
CREATE TABLE source_revisions (
    revision_no INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE CHECK (length(trim(id)) > 0),
    source_record_id TEXT NOT NULL,
    content_hash TEXT NOT NULL CHECK (length(content_hash) = 64),
    payload_json TEXT NOT NULL CHECK (json_valid(payload_json)),
    source_updated_at TEXT,
    observed_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (source_record_id) REFERENCES source_records(id)
);

-- source_sync_state is the durable per-instance capability checkpoint. It is
-- deliberately separate from Scheduler diagnostics and plugin configuration.
CREATE TABLE source_sync_state (
    extension_id TEXT NOT NULL CHECK (length(trim(extension_id)) > 0),
    instance_id TEXT NOT NULL CHECK (length(trim(instance_id)) > 0),
    capability_id TEXT NOT NULL CHECK (length(trim(capability_id)) > 0),
    cursor TEXT,
    checkpoint_version INTEGER NOT NULL CHECK (checkpoint_version >= 1),
    last_success_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(extension_id, instance_id, capability_id)
);

-- source_processing_state tracks the durable processing lifecycle of an immutable SourceRevision.
CREATE TABLE source_processing_state (
    source_revision_id TEXT PRIMARY KEY CHECK (length(trim(source_revision_id)) > 0),
    state TEXT NOT NULL CHECK (state IN ('PENDING', 'PROCESSING', 'COMPLETED', 'FAILED')),
    mode TEXT NOT NULL CHECK (mode IN ('SEMANTIC_PROPOSAL', 'DETERMINISTIC_SYNC', 'DIRECT_MUTATION')),
    started_at TEXT,
    completed_at TEXT,
    last_error TEXT CHECK (last_error IS NULL OR length(last_error) <= 4096),
    proposal_id TEXT,
    revision_id TEXT,
    created_at TEXT NOT NULL CHECK (length(trim(created_at)) > 0),
    updated_at TEXT NOT NULL CHECK (length(trim(updated_at)) > 0),
    FOREIGN KEY (source_revision_id) REFERENCES source_revisions(id),
    FOREIGN KEY (proposal_id) REFERENCES change_proposals(id),
    FOREIGN KEY (revision_id) REFERENCES revisions(id)
);

-- ============================================================
-- AI Run Lineage
-- ============================================================
-- ai_runs records one durable AI execution attempt. A processing_id groups
-- the clean and interpret runs without making either run implicit. The
-- canonical request is the pipeline input to ai.Service, not a provider wire
-- request after middleware transformation.
CREATE TABLE ai_runs (
    id TEXT PRIMARY KEY CHECK (length(trim(id)) > 0),
    processing_id TEXT NOT NULL CHECK (length(trim(processing_id)) > 0),
    request_id TEXT NOT NULL UNIQUE CHECK (length(trim(request_id)) > 0),
    action TEXT NOT NULL CHECK (length(trim(action)) > 0),
    status TEXT NOT NULL CHECK (status IN ('RUNNING', 'SUCCEEDED', 'FAILED')),
    canonical_request_json TEXT NOT NULL CHECK (json_valid(canonical_request_json)),
    output_json TEXT CHECK (output_json IS NULL OR json_valid(output_json)),
    error_text TEXT CHECK (error_text IS NULL OR length(error_text) <= 4096),
    provider_invoked INTEGER CHECK (provider_invoked IS NULL OR provider_invoked IN (0, 1)),
    actual_provider_id TEXT,
    actual_provider_name TEXT,
    actual_provider_source TEXT CHECK (
        actual_provider_source IS NULL
        OR actual_provider_source IN ('BUILTIN', 'PLUGIN')
    ),
    response_origin_type TEXT CHECK (
        response_origin_type IS NULL
        OR response_origin_type IN ('PROVIDER', 'PRE_MIDDLEWARE')
    ),
    response_origin_id TEXT,
    response_origin_name TEXT,
    upstream_run_id TEXT,
    started_at TEXT NOT NULL,
    completed_at TEXT,
    FOREIGN KEY (upstream_run_id) REFERENCES ai_runs(id),
    CHECK (upstream_run_id IS NULL OR upstream_run_id <> id),
    CHECK (
        (provider_invoked = 1
            AND actual_provider_id IS NOT NULL
            AND actual_provider_name IS NOT NULL
            AND actual_provider_source IS NOT NULL)
        OR
        ((provider_invoked IS NULL OR provider_invoked = 0)
            AND actual_provider_id IS NULL
            AND actual_provider_name IS NULL
            AND actual_provider_source IS NULL)
    ),
    CHECK (
        (provider_invoked IS NULL
            AND response_origin_type IS NULL
            AND response_origin_id IS NULL
            AND response_origin_name IS NULL)
        OR
        (provider_invoked IS NOT NULL
            AND response_origin_type IS NOT NULL
            AND response_origin_id IS NOT NULL
            AND response_origin_name IS NOT NULL)
    ),
    CHECK (
        provider_invoked IS NULL
        OR (provider_invoked = 1 AND response_origin_type = 'PROVIDER')
        OR (provider_invoked = 0 AND response_origin_type = 'PRE_MIDDLEWARE')
    ),
    CHECK (
        (status = 'RUNNING'
            AND completed_at IS NULL
            AND output_json IS NULL
            AND error_text IS NULL
            AND provider_invoked IS NULL)
        OR
        (status = 'SUCCEEDED'
            AND completed_at IS NOT NULL
            AND output_json IS NOT NULL
            AND error_text IS NULL
            AND provider_invoked IS NOT NULL)
        OR
        (status = 'FAILED'
            AND completed_at IS NOT NULL
            AND output_json IS NULL
            AND error_text IS NOT NULL
            AND length(trim(error_text)) > 0)
    )
);

-- Every source-processing AI run links directly to the exact immutable source
-- revision it consumed. Interpret runs also retain their clean run through
-- ai_runs.upstream_run_id rather than relying on timestamp ordering.
CREATE TABLE ai_run_sources (
    ai_run_id TEXT NOT NULL,
    source_revision_id TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('PRIMARY_SOURCE')),
    PRIMARY KEY (ai_run_id, source_revision_id, role),
    FOREIGN KEY (ai_run_id) REFERENCES ai_runs(id),
    FOREIGN KEY (source_revision_id) REFERENCES source_revisions(id)
);

-- revision_sources links a committed domain Revision to the exact Source revisions
-- that served as its evidence or trigger.
CREATE TABLE revision_sources (
    revision_id TEXT NOT NULL,
    source_revision_id TEXT NOT NULL,
    role TEXT CHECK (role IS NULL OR role IN ('PRIMARY_SOURCE', 'SUPPORTING', 'TRIGGER', 'EVIDENCE', 'USER_REQUEST')),
    PRIMARY KEY (revision_id, source_revision_id),
    FOREIGN KEY (revision_id) REFERENCES revisions(id),
    FOREIGN KEY (source_revision_id) REFERENCES source_revisions(id)
);

-- Protect immutable history and ensure a record head can only reference one
-- of that record's own revisions without introducing a cyclic foreign key.
-- +goose StatementBegin
CREATE TRIGGER prevent_source_revision_update
BEFORE UPDATE ON source_revisions
BEGIN
    SELECT RAISE(ABORT, 'source revisions are immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER prevent_source_revision_delete
BEFORE DELETE ON source_revisions
BEGIN
    SELECT RAISE(ABORT, 'source revisions are immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER validate_source_record_head
BEFORE UPDATE OF current_revision_id ON source_records
WHEN NEW.current_revision_id IS NOT NULL
     AND NOT EXISTS (
        SELECT 1
        FROM source_revisions
        WHERE id = NEW.current_revision_id
          AND source_record_id = NEW.id
          AND content_hash = NEW.current_content_hash
     )
BEGIN
    SELECT RAISE(ABORT, 'source record head must belong to the record');
END;
-- +goose StatementEnd

-- ============================================================
-- Work Items
-- ============================================================
-- work_items stores Lum Harbor's authoritative long-term outcome/objective
-- state. Entity-level archival remains the single archive authority.
CREATE TABLE work_items (
    entity_id TEXT PRIMARY KEY,
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    description TEXT,
    lifecycle_state TEXT NOT NULL CHECK (
        lifecycle_state IN (
            'ACTIVE',
            'PAUSED',
            'COMPLETED',
            'CANCELLED'
        )
    ),
    completed_at TEXT,
    last_revision_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (entity_id) REFERENCES entities(id),
    FOREIGN KEY (last_revision_id) REFERENCES revisions(id),
    CHECK (
        (lifecycle_state = 'COMPLETED' AND completed_at IS NOT NULL)
        OR
        (lifecycle_state <> 'COMPLETED' AND completed_at IS NULL)
    )
);

-- ============================================================
-- Tracking Core V1 (M10 Slice 1)
-- ============================================================
-- workflow_definitions are reusable, versioned process definitions. Once
-- created, their steps are immutable; a changed process is a new
-- (name, version). Definitions are configuration, not Entities, and carry no
-- Revision. There is intentionally no delete/update path.
CREATE TABLE workflow_definitions (
    id TEXT PRIMARY KEY CHECK (length(trim(id)) > 0),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    version INTEGER NOT NULL CHECK (version > 0),
    active INTEGER NOT NULL CHECK (active IN (0, 1)),
    created_at TEXT NOT NULL,
    UNIQUE (name, version)
);

-- workflow_steps carry semantic facts only: stable key, title, order and a
-- terminal fact. No presentation data (coordinates, lanes, colors).
CREATE TABLE workflow_steps (
    id TEXT PRIMARY KEY CHECK (length(trim(id)) > 0),
    workflow_definition_id TEXT NOT NULL,
    step_key TEXT NOT NULL CHECK (length(trim(step_key)) > 0),
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    sequence_no INTEGER NOT NULL CHECK (sequence_no > 0),
    terminal INTEGER NOT NULL CHECK (terminal IN (0, 1)),
    FOREIGN KEY (workflow_definition_id) REFERENCES workflow_definitions(id),
    UNIQUE (workflow_definition_id, step_key),
    UNIQUE (workflow_definition_id, sequence_no),
    -- Composite target so workflows.current_step_id can be constrained to the
    -- Workflow's own WorkflowDefinition in the database.
    UNIQUE (id, workflow_definition_id)
);

-- workflows binds exactly one WorkflowDefinition to at most one WorkItem (0..1).
-- A WorkItem can never create a second Workflow (UNIQUE work_item_id).
-- A Workflow owns two mutable authoritative facts independent of any
-- TrackedItem: current_step_id (the Workflow's overall position, always a real
-- step of its own WorkflowDefinition via the composite FK) and started_at (the
-- actual Workflow start, defaulting to created_at at creation). TrackedItem
-- progression never mutates current_step_id, and the Workflow may move backward.
-- There is deliberately NO Workflow lifecycle/completion column here.
CREATE TABLE workflows (
    id TEXT PRIMARY KEY CHECK (length(trim(id)) > 0),
    work_item_id TEXT NOT NULL UNIQUE,
    workflow_definition_id TEXT NOT NULL,
    current_step_id TEXT NOT NULL,
    started_at TEXT NOT NULL CHECK (length(trim(started_at)) > 0),
    last_revision_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (work_item_id) REFERENCES work_items(entity_id),
    FOREIGN KEY (workflow_definition_id) REFERENCES workflow_definitions(id),
    FOREIGN KEY (current_step_id, workflow_definition_id) REFERENCES workflow_steps(id, workflow_definition_id),
    FOREIGN KEY (last_revision_id) REFERENCES revisions(id),
    UNIQUE (id, work_item_id)
);

-- tracked_items are the units tracked inside one WorkItem and its single
-- Workflow. The optional parent must belong to the same WorkItem (composite FK).
-- Entity-level archival is the single archive authority, so there is no ARCHIVED
-- lifecycle value. There is deliberately NO current-step column: a TrackedItem's
-- position is derived from ordered workflow_steps and tracked_item_step_states.
-- The overall Workflow position is the separate persisted workflows.current_step_id.
CREATE TABLE tracked_items (
    entity_id TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL,
    parent_item_id TEXT,
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    item_type TEXT CHECK (item_type IS NULL OR length(trim(item_type)) > 0),
    lifecycle_state TEXT NOT NULL CHECK (
        lifecycle_state IN (
            'ACTIVE',
            'COMPLETED',
            'CANCELLED'
        )
    ),
    completed_at TEXT,
    last_revision_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (entity_id) REFERENCES entities(id),
    FOREIGN KEY (work_item_id) REFERENCES work_items(entity_id),
    FOREIGN KEY (workflow_id, work_item_id) REFERENCES workflows(id, work_item_id),
    FOREIGN KEY (parent_item_id, work_item_id) REFERENCES tracked_items(entity_id, work_item_id),
    FOREIGN KEY (last_revision_id) REFERENCES revisions(id),
    UNIQUE (entity_id, work_item_id),
    UNIQUE (entity_id, workflow_id),
    CHECK (
        (lifecycle_state = 'COMPLETED' AND completed_at IS NOT NULL)
        OR
        (lifecycle_state <> 'COMPLETED' AND completed_at IS NULL)
    )
);

-- tracked_item_step_states is the authoritative per-step workflow state.
CREATE TABLE tracked_item_step_states (
    tracked_item_id TEXT NOT NULL,
    workflow_step_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (
        state IN (
            'NOT_STARTED',
            'IN_PROGRESS',
            'COMPLETED',
            'WAITING',
            'BLOCKED',
            'FAILED',
            'UNKNOWN',
            'SKIPPED'
        )
    ),
    entered_at TEXT,
    completed_at TEXT,
    last_revision_id TEXT NOT NULL,
    PRIMARY KEY (tracked_item_id, workflow_step_id),
    FOREIGN KEY (tracked_item_id) REFERENCES tracked_items(entity_id),
    FOREIGN KEY (workflow_step_id) REFERENCES workflow_steps(id),
    FOREIGN KEY (last_revision_id) REFERENCES revisions(id)
);

-- A bounded dependency model for TrackedItems. Only the forward DEPENDS_ON
-- fact is stored; callers derive the inverse "blocks" projection. Both ends
-- are constrained to the same Workflow and cycles are rejected by the
-- Tracking service before insert.
CREATE TABLE tracked_item_relations (
    workflow_id TEXT NOT NULL,
    from_tracked_item_id TEXT NOT NULL,
    to_tracked_item_id TEXT NOT NULL,
    relation_type TEXT NOT NULL CHECK (relation_type = 'DEPENDS_ON'),
    last_revision_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (from_tracked_item_id, to_tracked_item_id, relation_type),
    FOREIGN KEY (workflow_id) REFERENCES workflows(id),
    FOREIGN KEY (from_tracked_item_id, workflow_id) REFERENCES tracked_items(entity_id, workflow_id),
    FOREIGN KEY (to_tracked_item_id, workflow_id) REFERENCES tracked_items(entity_id, workflow_id),
    FOREIGN KEY (last_revision_id) REFERENCES revisions(id),
    CHECK (from_tracked_item_id <> to_tracked_item_id)
);

-- monitor_points are authoritative time-based expectations: "by due_at, the
-- subject is expected to satisfy the typed condition". Subject is the owning
-- WorkItem (subject_tracked_item_id IS NULL) or one of its TrackedItems (the
-- composite FK forces the same WorkItem). The expected condition is typed and
-- consistent with the subject: a WorkItem subject expects a WorkItem lifecycle;
-- a TrackedItem subject expects one workflow step to be COMPLETED. State is
-- authoritative (ACTIVE/SATISFIED/CANCELLED); overdue/at-risk are derived by a
-- future evaluator and never stored. No scheduling/notification columns exist.
CREATE TABLE monitor_points (
    entity_id TEXT PRIMARY KEY,
    work_item_id TEXT NOT NULL,
    subject_tracked_item_id TEXT,
    monitor_kind TEXT NOT NULL CHECK (
        monitor_kind IN (
            'FINAL_TARGET',
            'CHECKPOINT',
            'DEADLINE',
            'REVIEW',
            'CUSTOM'
        )
    ),
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    due_at TEXT NOT NULL,
    timezone TEXT NOT NULL CHECK (length(trim(timezone)) > 0),
    expected_kind TEXT NOT NULL CHECK (
        expected_kind IN (
            'WORK_ITEM_LIFECYCLE',
            'TRACKED_ITEM_STEP_COMPLETED'
        )
    ),
    expected_work_item_lifecycle TEXT,
    expected_step_id TEXT,
    state TEXT NOT NULL CHECK (
        state IN (
            'ACTIVE',
            'SATISFIED',
            'CANCELLED'
        )
    ),
    satisfied_at TEXT,
    last_revision_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (entity_id) REFERENCES entities(id),
    FOREIGN KEY (work_item_id) REFERENCES work_items(entity_id),
    FOREIGN KEY (subject_tracked_item_id, work_item_id) REFERENCES tracked_items(entity_id, work_item_id),
    FOREIGN KEY (expected_step_id) REFERENCES workflow_steps(id),
    FOREIGN KEY (last_revision_id) REFERENCES revisions(id),
    CHECK (
        (
            subject_tracked_item_id IS NULL
            AND expected_kind = 'WORK_ITEM_LIFECYCLE'
            AND expected_work_item_lifecycle IS NOT NULL
            AND expected_work_item_lifecycle IN ('ACTIVE', 'PAUSED', 'COMPLETED', 'CANCELLED')
            AND expected_step_id IS NULL
        )
        OR
        (
            subject_tracked_item_id IS NOT NULL
            AND expected_kind = 'TRACKED_ITEM_STEP_COMPLETED'
            AND expected_step_id IS NOT NULL
            AND expected_work_item_lifecycle IS NULL
        )
    ),
    CHECK (
        (state = 'SATISFIED' AND satisfied_at IS NOT NULL)
        OR
        (state <> 'SATISFIED' AND satisfied_at IS NULL)
    )
);

-- ============================================================
-- Attention & Notification Foundation V1 (M11 Slice 1)
-- ============================================================
-- attention_issues are persistent, unresolved problems derived deterministically
-- from authoritative truth. AttentionIssue != Notification. The lifecycle is only
-- OPEN/RESOLVED; snooze is an orthogonal timestamp (snoozed_until), never a
-- lifecycle state, and its expiry needs no state rewrite. At most one OPEN issue
-- exists per dedupe_key (partial unique index); a later recurrence after
-- resolution is a new issue.
CREATE TABLE attention_issues (
    entity_id TEXT PRIMARY KEY,
    work_item_id TEXT,
    subject_tracked_item_id TEXT,
    monitor_point_id TEXT,
    planner_item_id TEXT,
    issue_type TEXT NOT NULL CHECK (issue_type IN ('MONITOR_OVERDUE_UNMET', 'TODO_REMINDER_DUE')),
    severity TEXT NOT NULL CHECK (severity IN ('INFO', 'WARNING', 'CRITICAL')),
    state TEXT NOT NULL CHECK (state IN ('OPEN', 'RESOLVED')),
    reason TEXT NOT NULL CHECK (length(trim(reason)) > 0),
    reason_json TEXT NOT NULL CHECK (json_valid(reason_json)),
    recommended_action TEXT,
    opened_at TEXT NOT NULL,
    resolved_at TEXT,
    snoozed_until TEXT,
    dedupe_key TEXT NOT NULL CHECK (length(trim(dedupe_key)) > 0),
    last_revision_id TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (entity_id) REFERENCES entities(id),
    FOREIGN KEY (work_item_id) REFERENCES work_items(entity_id),
    FOREIGN KEY (subject_tracked_item_id, work_item_id) REFERENCES tracked_items(entity_id, work_item_id),
    FOREIGN KEY (monitor_point_id) REFERENCES monitor_points(entity_id),
    FOREIGN KEY (planner_item_id) REFERENCES planner_items(entity_id),
    FOREIGN KEY (last_revision_id) REFERENCES revisions(id),
    UNIQUE (entity_id, work_item_id),
    CHECK (
        (state = 'RESOLVED' AND resolved_at IS NOT NULL AND snoozed_until IS NULL)
        OR
        (state = 'OPEN' AND resolved_at IS NULL)
    ),
    CHECK (
        (issue_type = 'MONITOR_OVERDUE_UNMET' AND monitor_point_id IS NOT NULL AND work_item_id IS NOT NULL)
        OR
        (issue_type = 'TODO_REMINDER_DUE' AND planner_item_id IS NOT NULL)
    )
);

-- notifications are durable communications. read_at / dismissed_at are
-- independent interaction timestamps: they never resolve an AttentionIssue and
-- never change domain truth. state is delivery only: PENDING = durably created
-- but NOT yet delivered to the in-app Notification Center; SENT = delivered by
-- the deterministic IN_APP dispatcher (the only product-visible state);
-- CANCELLED = became obsolete before first delivery because its AttentionIssue
-- resolved (never sent, never shown). The reason snapshot preserves why it was
-- created, independent of later truth.
CREATE TABLE notifications (
    entity_id TEXT PRIMARY KEY,
    attention_issue_id TEXT NOT NULL,
    work_item_id TEXT,
    subject_tracked_item_id TEXT,
    planner_item_id TEXT,
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    message TEXT NOT NULL CHECK (length(trim(message)) > 0),
    reason_snapshot_json TEXT NOT NULL CHECK (json_valid(reason_snapshot_json)),
    state TEXT NOT NULL CHECK (state IN ('PENDING', 'SENT', 'CANCELLED')),
    channel TEXT NOT NULL CHECK (channel IN ('IN_APP')),
    sent_at TEXT,
    presented_at TEXT,
    read_at TEXT,
    dismissed_at TEXT,
    dedupe_key TEXT NOT NULL CHECK (length(trim(dedupe_key)) > 0),
    last_revision_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (entity_id) REFERENCES entities(id),
    FOREIGN KEY (attention_issue_id) REFERENCES attention_issues(entity_id),
    FOREIGN KEY (attention_issue_id, work_item_id) REFERENCES attention_issues(entity_id, work_item_id),
    FOREIGN KEY (subject_tracked_item_id, work_item_id) REFERENCES tracked_items(entity_id, work_item_id),
    FOREIGN KEY (planner_item_id) REFERENCES planner_items(entity_id),
    FOREIGN KEY (last_revision_id) REFERENCES revisions(id),
    UNIQUE (dedupe_key),
    CHECK (
        (state = 'SENT' AND sent_at IS NOT NULL)
        OR
        (state IN ('PENDING', 'CANCELLED') AND sent_at IS NULL)
    )
);

-- notification_external_deliveries is M17's durable delivery-boundary truth:
-- one row means "an external delivery capability (e.g. a macOS system
-- notification Plugin) confirmed it successfully ensured delivery of this
-- Notification." It exists solely for restart-safe idempotency across the
-- unavoidable external-I/O / local-DB commit gap and is written only after a
-- successful capability response - never speculatively before. Absence of a
-- row means "not yet confirmed delivered" (eligible for the next attempt),
-- never a durable failure state; failure is not persisted in V1. This table
-- is owned by the delivery boundary, not by Notification/Attention Core: it
-- is never read or written by internal/attention, and it never gains a
-- macOS-specific or other platform-specific column. notifications.state,
-- notifications.channel, and every other existing Notification/Attention
-- column/semantic are unchanged by this table's existence.
CREATE TABLE notification_external_deliveries (
    notification_id TEXT PRIMARY KEY,
    delivered_at TEXT NOT NULL CHECK (length(trim(delivered_at)) > 0),
    FOREIGN KEY (notification_id) REFERENCES notifications(entity_id)
);

-- ============================================================
-- Change Proposals
-- ============================================================
-- change_proposals stores persistent, reviewable mutation intents
-- targeting domain state. A proposal is suggested change, not current truth.
-- It is a review/control-plane object and does not create an entity row.
CREATE TABLE change_proposals (
    id TEXT PRIMARY KEY CHECK (length(trim(id)) > 0),
    -- target_entity_id is nullable ONLY for root-create operation types that
    -- propose a brand-new entity with no pre-existing target to reference
    -- (currently 'planner.item.create', 'work_item.create' and
    -- 'work_item.create_with_workflow'), and for the aggregate parent
    -- 'proposal.aggregate', whose children may span several targets and where
    -- the column is only a navigation hint. Every other operation type must
    -- still supply a real, existing target entity. See the CHECK below.
    target_entity_id TEXT,
    operation_type TEXT NOT NULL CHECK (length(trim(operation_type)) > 0),
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    summary TEXT,
    reason TEXT,
    status TEXT NOT NULL CHECK (
        status IN (
            'PENDING',
            'APPLIED',
            'REJECTED',
            'STALE'
        )
    ),
    operation_payload_json TEXT NOT NULL CHECK (json_valid(operation_payload_json)),
    applied_revision_id TEXT,
    ai_run_id TEXT,
    created_at TEXT NOT NULL CHECK (length(trim(created_at)) > 0),
    updated_at TEXT NOT NULL CHECK (length(trim(updated_at)) > 0),
    resolved_at TEXT,
    FOREIGN KEY (target_entity_id) REFERENCES entities(id),
    FOREIGN KEY (applied_revision_id) REFERENCES revisions(id),
    FOREIGN KEY (ai_run_id) REFERENCES ai_runs(id),
    CHECK (
        (status = 'PENDING' AND resolved_at IS NULL)
        OR
        (status <> 'PENDING' AND resolved_at IS NOT NULL)
    ),
    CHECK (
        (status = 'APPLIED' AND applied_revision_id IS NOT NULL)
        OR
        (status <> 'APPLIED' AND applied_revision_id IS NULL)
    ),
    CHECK (
        target_entity_id IS NOT NULL
        OR operation_type IN ('planner.item.create', 'work_item.create', 'work_item.create_with_workflow', 'proposal.aggregate')
    )
);

-- change_proposal_sources links a ChangeProposal to the exact immutable Source revisions
-- that served as its evidence or trigger.
CREATE TABLE change_proposal_sources (
    proposal_id TEXT NOT NULL,
    source_revision_id TEXT NOT NULL,
    role TEXT CHECK (role IS NULL OR role IN ('PRIMARY_SOURCE', 'SUPPORTING', 'TRIGGER', 'EVIDENCE', 'USER_REQUEST')),
    PRIMARY KEY (proposal_id, source_revision_id),
    FOREIGN KEY (proposal_id) REFERENCES change_proposals(id),
    FOREIGN KEY (source_revision_id) REFERENCES source_revisions(id)
);

-- proposal_operations stores the ordered typed child operations of one
-- aggregate ChangeProposal (operation_type = 'proposal.aggregate'). A Proposal
-- with zero rows here is a legacy singular Proposal whose one operation lives in
-- change_proposals.operation_payload_json. Each child reuses one closed typed
-- Proposal operation type and its versioned payload; aggregates never nest.
-- Lifecycle, review, AI Run and Source links stay on the parent row: children
-- have no status and are written once together with the parent.
CREATE TABLE proposal_operations (
    id TEXT PRIMARY KEY CHECK (length(trim(id)) > 0),
    proposal_id TEXT NOT NULL,
    sequence_no INTEGER NOT NULL CHECK (sequence_no >= 0),
    operation_type TEXT NOT NULL CHECK (
        length(trim(operation_type)) > 0
        AND operation_type <> 'proposal.aggregate'
    ),
    -- Concrete existing target only; NULL for root creates and for children
    -- whose target is produced by an earlier child of the same Proposal.
    target_entity_id TEXT,
    operation_payload_json TEXT NOT NULL CHECK (json_valid(operation_payload_json)),
    FOREIGN KEY (proposal_id) REFERENCES change_proposals(id),
    FOREIGN KEY (target_entity_id) REFERENCES entities(id),
    UNIQUE (proposal_id, sequence_no)
);

-- proposal_operation_results is the complete ordered result set of one applied
-- child operation: each row names one Foundation Revision the child committed
-- and, when that Revision created or changed a Foundation Entity, which one. A
-- Revision is a mutation batch, not an entity version: one Revision may cover
-- several Entities (a Todo and its Reminder) and some Revisions have no Entity
-- (a Workflow), so entity_id is nullable and the key is the per-operation order.
-- The domain type stays in entities.entity_type. For aggregate Proposals,
-- change_proposals.applied_revision_id is only a compatibility primary pointer.
CREATE TABLE proposal_operation_results (
    proposal_operation_id TEXT NOT NULL,
    sequence_no INTEGER NOT NULL CHECK (sequence_no >= 0),
    entity_id TEXT,
    revision_id TEXT NOT NULL,
    result_role TEXT NOT NULL CHECK (result_role IN ('PRIMARY', 'SECONDARY')),
    PRIMARY KEY (proposal_operation_id, sequence_no),
    FOREIGN KEY (proposal_operation_id) REFERENCES proposal_operations(id),
    FOREIGN KEY (entity_id) REFERENCES entities(id),
    FOREIGN KEY (revision_id) REFERENCES revisions(id)
);

-- ============================================================
-- Planner
-- ============================================================
-- planner_items stores Lum Harbor's own actionable planning state.
--
-- A Planner Item is not necessarily a copy of an Apple Reminder
-- or Calendar Event. It represents something Lum Harbor wants to
-- show and manage inside the Planner.
--
-- Examples:
--   - TODO
--   - CHECKPOINT
--   - FOLLOW_UP
--   - REMINDER
--
-- External Calendar / Reminder identifiers will be connected through
-- external_refs instead of adding provider-specific columns here.
CREATE TABLE planner_items (
    entity_id TEXT PRIMARY KEY,
    authority_type TEXT NOT NULL CHECK (
        authority_type IN (
            'LOCAL',
            'EXTERNAL'
        )
    ),
    work_item_id TEXT,
    workflow_id TEXT,
    workflow_step_id TEXT,
    related_entity_id TEXT,
    monitor_point_id TEXT,
    item_type TEXT NOT NULL CHECK (
        item_type IN (
            'TODO',
            'CHECKPOINT',
            'FOLLOW_UP',
            'REMINDER'
        )
    ),
    title TEXT NOT NULL,
    details TEXT,
    status TEXT NOT NULL CHECK (
        status IN (
            'OPEN',
            'COMPLETED'
        )
    ),
    scheduled_at TEXT,
    due_at TEXT,
    scheduled_date TEXT,
    due_date TEXT,
    context_status TEXT NOT NULL CHECK (
        context_status IN (
            'COMPLETE',
            'INCOMPLETE',
            'UNKNOWN'
        )
    ),
    missing_context_json TEXT,
    last_revision_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    completed_at TEXT,
    -- local_note is Lum Harbor-owned context attached to the item. It is never
    -- written by external sync: for an EXTERNAL item, title/details/due/
    -- status/completed_at stay owned by the external system while local_note
    -- stays owned by Lum Harbor (changed only through Proposal Apply).
    local_note TEXT,
    FOREIGN KEY (entity_id) REFERENCES entities(id),
    FOREIGN KEY (work_item_id) REFERENCES work_items(entity_id),
    FOREIGN KEY (workflow_id) REFERENCES workflows(id),
    FOREIGN KEY (workflow_step_id) REFERENCES workflow_steps(id),
    FOREIGN KEY (related_entity_id) REFERENCES entities(id),
    FOREIGN KEY (last_revision_id) REFERENCES revisions(id),

    CHECK (
        scheduled_at IS NULL
        OR scheduled_date IS NULL
    ),

    CHECK (
        due_at IS NULL
        OR due_date IS NULL
    ),

    CHECK (
        (
            status = 'OPEN'
            AND completed_at IS NULL
        )
        OR
        (
            status = 'COMPLETED'
            AND completed_at IS NOT NULL
        )
    )
);

-- ============================================================
-- Calendar Events
-- ============================================================
-- calendar_events stores scheduled occurrences shown in the
-- Planner calendar.
--
-- External provider identity is stored in external_refs rather
-- than provider-specific columns in this table.
CREATE TABLE calendar_events (
    entity_id TEXT PRIMARY KEY,

    authority_type TEXT NOT NULL CHECK (
        authority_type IN (
            'LOCAL',
            'EXTERNAL'
        )
    ),

    title TEXT NOT NULL,
    details TEXT,
    location TEXT,
    start_at TEXT,
    end_at TEXT,
    start_date TEXT,
    end_date TEXT,

    is_all_day INTEGER NOT NULL DEFAULT 0 CHECK (
        is_all_day IN (0, 1)
    ),

    timezone TEXT,

    last_revision_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,

    FOREIGN KEY (entity_id) REFERENCES entities(id),
    FOREIGN KEY (last_revision_id) REFERENCES revisions(id),

    CHECK (
        (
            is_all_day = 0
            AND start_at IS NOT NULL
            AND end_at IS NOT NULL
            AND start_date IS NULL
            AND end_date IS NULL
        )
        OR
        (
            is_all_day = 1
            AND start_at IS NULL
            AND end_at IS NULL
            AND start_date IS NOT NULL
            AND end_date IS NOT NULL
        )
    ),

    CHECK (
        start_at IS NULL OR end_at > start_at
    ),

    CHECK (
        start_date IS NULL OR end_date > start_date
    )
);

-- ============================================================
-- Todo Reminders (M18)
-- ============================================================
-- todo_reminders is the long-lived watcher for a Planner Todo.
-- At most one reminder configuration exists per PlannerItem (UNIQUE constraint).
-- state:
--   ACTIVE: currently monitored until completion or cancellation
--   SATISFIED: the bound Todo completed, stopping all future thresholds
--   CANCELLED: the user disabled/removed the reminder
-- offsets_seconds_json: JSON array of integer seconds before due (e.g. [86400, 172800, 259200])
-- fired_offsets_json: JSON array of offsets that have already fired (restart-safe idempotency)
CREATE TABLE todo_reminders (
    entity_id TEXT PRIMARY KEY,
    planner_item_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('ACTIVE', 'SATISFIED', 'CANCELLED')),
    offsets_seconds_json TEXT NOT NULL CHECK (json_valid(offsets_seconds_json)),
    fired_offsets_json TEXT NOT NULL CHECK (json_valid(fired_offsets_json)),
    remind_mode TEXT NOT NULL DEFAULT 'BEFORE_DUE' CHECK (remind_mode IN ('BEFORE_DUE', 'EXACT_TIME')),
    last_revision_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    satisfied_at TEXT,
    FOREIGN KEY (entity_id) REFERENCES entities(id),
    FOREIGN KEY (planner_item_id) REFERENCES planner_items(entity_id),
    FOREIGN KEY (last_revision_id) REFERENCES revisions(id),
    UNIQUE (planner_item_id)
);

-- tracking_reevaluation_requests is the narrow durable handoff from a user
-- completing one tracking-linked Todo to asynchronous AI reevaluation. It is
-- not a generic event/outbox and never changes Source processing state.
CREATE TABLE tracking_reevaluation_requests (
    id TEXT PRIMARY KEY CHECK (length(trim(id)) > 0),
    planner_item_id TEXT NOT NULL,
    completion_revision_id TEXT NOT NULL UNIQUE,
    tracked_item_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('PENDING', 'PROCESSING', 'COMPLETED', 'FAILED')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    started_at TEXT,
    completed_at TEXT,
    last_error TEXT CHECK (last_error IS NULL OR length(last_error) <= 4096),
    proposal_id TEXT,
    ai_run_id TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (planner_item_id) REFERENCES planner_items(entity_id),
    FOREIGN KEY (completion_revision_id) REFERENCES revisions(id),
    FOREIGN KEY (tracked_item_id) REFERENCES tracked_items(entity_id),
    FOREIGN KEY (proposal_id) REFERENCES change_proposals(id),
    FOREIGN KEY (ai_run_id) REFERENCES ai_runs(id),
    CHECK (
        (state = 'PENDING' AND started_at IS NULL AND completed_at IS NULL AND proposal_id IS NULL AND ai_run_id IS NULL)
        OR (state = 'PROCESSING' AND started_at IS NOT NULL AND completed_at IS NULL AND proposal_id IS NULL AND ai_run_id IS NULL)
        OR (state = 'COMPLETED' AND started_at IS NOT NULL AND completed_at IS NOT NULL AND last_error IS NULL)
        OR (state = 'FAILED' AND started_at IS NOT NULL AND completed_at IS NOT NULL AND last_error IS NOT NULL AND length(trim(last_error)) > 0)
    )
);

-- ============================================================
-- History / Provenance V1 (M12 Slices 1-2)
-- ============================================================
-- history_events are immutable, business-semantic facts. They explain
-- committed operations but never own or reconstruct current domain state.
-- HistoryEvent is deliberately not a Foundation Entity. A Revision may have
-- zero, one or many events, while an event may have no Revision.
CREATE TABLE history_events (
    id TEXT PRIMARY KEY CHECK (length(trim(id)) > 0),
    event_type TEXT NOT NULL CHECK (
        event_type IN (
            'MONITOR_CREATED',
            'MONITOR_STATE_CHANGED',
            'ATTENTION_OPENED',
            'ATTENTION_RESOLVED',
            'ATTENTION_SNOOZED',
            'NOTIFICATION_CREATED',
            'NOTIFICATION_SENT',
            'NOTIFICATION_PRESENTED',
            'NOTIFICATION_DELIVERED',
            'NOTIFICATION_READ',
            'NOTIFICATION_CANCELLED',
            'PROPOSAL_CREATED',
            'PROPOSAL_APPLIED',
            'PROPOSAL_REJECTED',
            'PROPOSAL_STALE',
            'WORK_ITEM_CREATED',
            'WORK_ITEM_DETAILS_CHANGED',
            'WORK_ITEM_LIFECYCLE_CHANGED',
            'TRACKED_ITEM_CREATED',
            'TRACKED_ITEM_UPDATED',
            'TRACKED_ITEM_STAGE_STATE_CHANGED',
            'WORKFLOW_CREATED',
            'WORKFLOW_CURRENT_STEP_CHANGED',
            'TRACKED_ITEM_ARCHIVED',
            'TRACKED_ITEM_DEPENDENCY_ADDED',
            'TRACKED_ITEM_DEPENDENCY_REMOVED',
            'PLANNER_ITEM_CREATED',
            'PLANNER_ITEM_COMPLETED',
            'PLANNER_ITEM_EXTERNAL_UPDATED',
            'PLANNER_ITEM_LOCAL_NOTE_CHANGED',
            'PLANNER_ITEM_REMINDER_SET',
            'PLANNER_ITEM_REMINDER_REMOVED',
            'SOURCE_REVISION_OBSERVED',
            'AI_RUN_SUCCEEDED',
            'AI_RUN_FAILED'
        )
    ),
    scope TEXT NOT NULL CHECK (scope IN ('TRACKING', 'ATTENTION', 'NOTIFICATION', 'PROPOSAL', 'WORK_ITEM', 'PLANNER', 'SOURCE', 'AI')),
    actor_type TEXT NOT NULL CHECK (actor_type IN ('USER', 'SYSTEM', 'RULE', 'AI', 'SOURCE')),
    summary TEXT NOT NULL CHECK (length(trim(summary)) > 0 AND length(summary) <= 512),
    subject_entity_id TEXT,
    work_item_id TEXT,
    revision_id TEXT,
    proposal_id TEXT,
    ai_run_id TEXT,
    source_revision_id TEXT,
    correlation_id TEXT NOT NULL CHECK (length(trim(correlation_id)) > 0),
    causation_event_id TEXT,
    before_json TEXT CHECK (
        before_json IS NULL
        OR (json_valid(before_json) AND json_type(before_json) = 'object')
    ),
    after_json TEXT CHECK (
        after_json IS NULL
        OR (json_valid(after_json) AND json_type(after_json) = 'object')
    ),
    occurred_at TEXT NOT NULL CHECK (length(trim(occurred_at)) > 0),
    recorded_at TEXT NOT NULL CHECK (length(trim(recorded_at)) > 0),
    FOREIGN KEY (subject_entity_id) REFERENCES entities(id),
    FOREIGN KEY (work_item_id) REFERENCES work_items(entity_id),
    FOREIGN KEY (revision_id) REFERENCES revisions(id),
    FOREIGN KEY (proposal_id) REFERENCES change_proposals(id),
    FOREIGN KEY (ai_run_id) REFERENCES ai_runs(id),
    FOREIGN KEY (source_revision_id) REFERENCES source_revisions(id),
    FOREIGN KEY (causation_event_id) REFERENCES history_events(id),
    CHECK (causation_event_id IS NULL OR causation_event_id <> id)
);

-- History is append-only. Future retention/redaction requires a separate,
-- explicit design rather than rewriting committed provenance.
-- +goose StatementBegin
CREATE TRIGGER prevent_history_event_update
BEFORE UPDATE ON history_events
BEGIN
    SELECT RAISE(ABORT, 'history events are immutable');
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER prevent_history_event_delete
BEFORE DELETE ON history_events
BEGIN
    SELECT RAISE(ABORT, 'history events are immutable');
END;
-- +goose StatementEnd

CREATE INDEX idx_external_refs_entity_id ON external_refs(entity_id);

CREATE INDEX idx_source_revisions_record ON source_revisions(source_record_id, revision_no);

CREATE INDEX idx_ai_runs_processing ON ai_runs(processing_id, started_at);

CREATE INDEX idx_ai_run_sources_source ON ai_run_sources(source_revision_id);

CREATE UNIQUE INDEX idx_external_refs_authoritative_entity ON external_refs(entity_id)
WHERE
    relationship_type = 'AUTHORITATIVE';

CREATE INDEX idx_planner_items_scheduled_at ON planner_items(scheduled_at);

CREATE INDEX idx_planner_items_due_at ON planner_items(due_at);

CREATE INDEX idx_planner_items_work_item_id ON planner_items(work_item_id);

CREATE INDEX idx_planner_items_workflow_id ON planner_items(workflow_id);

CREATE INDEX idx_planner_items_related_entity_id ON planner_items(related_entity_id);

CREATE INDEX idx_planner_items_monitor_point_id ON planner_items(monitor_point_id);

CREATE INDEX idx_planner_items_status ON planner_items(status);

CREATE INDEX idx_planner_items_scheduled_date ON planner_items(scheduled_date);

CREATE INDEX idx_planner_items_due_date ON planner_items(due_date);

CREATE INDEX idx_calendar_events_start_at ON calendar_events(start_at);

CREATE INDEX idx_calendar_events_start_date ON calendar_events(start_date);

CREATE INDEX idx_history_events_recent ON history_events(occurred_at, id);

CREATE INDEX idx_history_events_correlation ON history_events(correlation_id, occurred_at, id);

CREATE INDEX idx_history_events_subject ON history_events(subject_entity_id, occurred_at, id);

CREATE INDEX idx_history_events_work_item ON history_events(work_item_id, occurred_at, id);

CREATE INDEX idx_history_events_revision ON history_events(revision_id);

CREATE INDEX idx_history_events_causation ON history_events(causation_event_id);

CREATE INDEX idx_history_events_source_revision ON history_events(source_revision_id);

-- At most one OPEN AttentionIssue per dedupe key (concurrency backstop).
CREATE UNIQUE INDEX idx_attention_issues_open_dedupe ON attention_issues(dedupe_key) WHERE state = 'OPEN';

CREATE INDEX idx_attention_issues_work_item ON attention_issues(work_item_id, state, opened_at);

CREATE INDEX idx_attention_issues_monitor_point ON attention_issues(monitor_point_id);

CREATE INDEX idx_attention_issues_planner_item ON attention_issues(planner_item_id);

CREATE INDEX idx_notifications_issue ON notifications(attention_issue_id);

CREATE INDEX idx_notifications_work_item_created ON notifications(work_item_id, created_at, entity_id);

CREATE INDEX idx_notifications_planner_item ON notifications(planner_item_id);

CREATE INDEX idx_todo_reminders_active ON todo_reminders(state);

CREATE INDEX idx_tracking_reevaluation_pending ON tracking_reevaluation_requests(state, created_at, id);

-- At most one ACTIVE FINAL_TARGET per subject, so a subject never has two
-- current final targets.
CREATE UNIQUE INDEX idx_monitor_points_active_final_target
ON monitor_points(work_item_id, COALESCE(subject_tracked_item_id, ''))
WHERE monitor_kind = 'FINAL_TARGET' AND state = 'ACTIVE';

CREATE INDEX idx_monitor_points_work_item_due ON monitor_points(work_item_id, due_at, created_at, entity_id);

CREATE INDEX idx_monitor_points_subject_tracked_item ON monitor_points(subject_tracked_item_id);

CREATE INDEX idx_tracked_items_work_item_id ON tracked_items(work_item_id, created_at, entity_id);

CREATE INDEX idx_tracked_items_parent_item_id ON tracked_items(parent_item_id);

CREATE INDEX idx_tracked_items_workflow_id ON tracked_items(workflow_id);

CREATE INDEX idx_tracked_item_relations_workflow ON tracked_item_relations(workflow_id, created_at);

CREATE INDEX idx_tracked_item_relations_target ON tracked_item_relations(to_tracked_item_id, from_tracked_item_id);

CREATE INDEX idx_workflows_workflow_definition_id ON workflows(workflow_definition_id);

CREATE INDEX idx_workflows_current_step_id ON workflows(current_step_id);

CREATE INDEX idx_tracked_item_step_states_step ON tracked_item_step_states(workflow_step_id);

CREATE INDEX idx_change_proposals_status_created ON change_proposals(status, created_at, id);

CREATE INDEX idx_change_proposals_target_entity ON change_proposals(target_entity_id);

CREATE INDEX idx_change_proposals_ai_run ON change_proposals(ai_run_id);

CREATE INDEX idx_change_proposal_sources_source ON change_proposal_sources(source_revision_id);

-- At most one PRIMARY result per child operation.
CREATE UNIQUE INDEX idx_proposal_operation_results_primary ON proposal_operation_results(proposal_operation_id)
WHERE result_role = 'PRIMARY';

CREATE INDEX idx_proposal_operation_results_revision ON proposal_operation_results(revision_id);

CREATE INDEX idx_revision_sources_source ON revision_sources(source_revision_id);

CREATE INDEX idx_source_processing_state_pending ON source_processing_state(state, mode, created_at);

-- +goose Down

DROP INDEX IF EXISTS idx_tracking_reevaluation_pending;
DROP INDEX IF EXISTS idx_source_processing_state_pending;
DROP INDEX IF EXISTS idx_change_proposal_sources_source;
DROP INDEX IF EXISTS idx_proposal_operation_results_revision;
DROP INDEX IF EXISTS idx_proposal_operation_results_primary;
DROP INDEX IF EXISTS idx_revision_sources_source;
DROP INDEX IF EXISTS idx_change_proposals_ai_run;

DROP INDEX IF EXISTS idx_history_events_source_revision;
DROP INDEX IF EXISTS idx_history_events_causation;
DROP INDEX IF EXISTS idx_history_events_revision;
DROP INDEX IF EXISTS idx_history_events_work_item;
DROP INDEX IF EXISTS idx_history_events_subject;
DROP INDEX IF EXISTS idx_history_events_correlation;
DROP INDEX IF EXISTS idx_history_events_recent;

-- ============================================================
-- Change Proposal indexes
-- ============================================================

DROP INDEX IF EXISTS idx_change_proposals_target_entity;

DROP INDEX IF EXISTS idx_todo_reminders_active;
DROP INDEX IF EXISTS idx_notifications_planner_item;
DROP INDEX IF EXISTS idx_notifications_work_item_created;
DROP INDEX IF EXISTS idx_notifications_issue;
DROP INDEX IF EXISTS idx_attention_issues_planner_item;
DROP INDEX IF EXISTS idx_attention_issues_monitor_point;
DROP INDEX IF EXISTS idx_attention_issues_work_item;
DROP INDEX IF EXISTS idx_attention_issues_open_dedupe;
DROP INDEX IF EXISTS idx_monitor_points_subject_tracked_item;
DROP INDEX IF EXISTS idx_monitor_points_work_item_due;
DROP INDEX IF EXISTS idx_monitor_points_active_final_target;
DROP INDEX IF EXISTS idx_tracked_item_step_states_step;
DROP INDEX IF EXISTS idx_workflows_current_step_id;
DROP INDEX IF EXISTS idx_workflows_workflow_definition_id;
DROP INDEX IF EXISTS idx_tracked_items_workflow_id;
DROP INDEX IF EXISTS idx_tracked_item_relations_target;
DROP INDEX IF EXISTS idx_tracked_item_relations_workflow;
DROP INDEX IF EXISTS idx_tracked_items_parent_item_id;
DROP INDEX IF EXISTS idx_tracked_items_work_item_id;
DROP INDEX IF EXISTS idx_change_proposals_status_created;

-- ============================================================
-- Calendar Event indexes
-- ============================================================

DROP INDEX IF EXISTS idx_calendar_events_start_at;
DROP INDEX IF EXISTS idx_calendar_events_start_date;

-- ============================================================
-- Planner indexes
-- ============================================================

DROP INDEX IF EXISTS idx_planner_items_scheduled_at;
DROP INDEX IF EXISTS idx_planner_items_due_at;
DROP INDEX IF EXISTS idx_planner_items_work_item_id;
DROP INDEX IF EXISTS idx_planner_items_workflow_id;
DROP INDEX IF EXISTS idx_planner_items_related_entity_id;
DROP INDEX IF EXISTS idx_planner_items_monitor_point_id;
DROP INDEX IF EXISTS idx_planner_items_status;
DROP INDEX IF EXISTS idx_planner_items_scheduled_date;
DROP INDEX IF EXISTS idx_planner_items_due_date;

-- ============================================================
-- External Ref indexes
-- ============================================================

DROP INDEX IF EXISTS idx_external_refs_authoritative_entity;
DROP INDEX IF EXISTS idx_external_refs_entity_id;

-- ============================================================
-- Source Layer indexes
-- ============================================================

DROP INDEX IF EXISTS idx_ai_run_sources_source;
DROP INDEX IF EXISTS idx_ai_runs_processing;

DROP INDEX IF EXISTS idx_source_revisions_record;

-- ============================================================
-- Tables
-- ============================================================

DROP TRIGGER IF EXISTS prevent_history_event_delete;
DROP TRIGGER IF EXISTS prevent_history_event_update;

DROP TRIGGER IF EXISTS validate_source_record_head;
DROP TRIGGER IF EXISTS prevent_source_revision_delete;
DROP TRIGGER IF EXISTS prevent_source_revision_update;

DROP TABLE IF EXISTS history_events;
DROP TABLE IF EXISTS tracking_reevaluation_requests;
DROP TABLE IF EXISTS tracked_item_relations;
DROP TABLE IF EXISTS notification_external_deliveries;
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS attention_issues;
DROP TABLE IF EXISTS monitor_points;
DROP TABLE IF EXISTS tracked_item_step_states;
DROP TABLE IF EXISTS tracked_items;
DROP TABLE IF EXISTS workflows;
DROP TABLE IF EXISTS workflow_steps;
DROP TABLE IF EXISTS workflow_definitions;
DROP TABLE IF EXISTS proposal_operation_results;
DROP TABLE IF EXISTS proposal_operations;
DROP TABLE IF EXISTS change_proposal_sources;
DROP TABLE IF EXISTS change_proposals;
DROP TABLE IF EXISTS calendar_events;
DROP TABLE IF EXISTS todo_reminders;
DROP TABLE IF EXISTS planner_items;
DROP TABLE IF EXISTS work_items;
DROP TABLE IF EXISTS external_refs;
DROP TABLE IF EXISTS ai_run_sources;
DROP TABLE IF EXISTS ai_runs;
DROP TABLE IF EXISTS source_sync_state;
DROP TABLE IF EXISTS source_processing_state;
DROP TABLE IF EXISTS revision_sources;
DROP TABLE IF EXISTS source_revisions;
DROP TABLE IF EXISTS source_records;
DROP TABLE IF EXISTS revisions;
DROP TABLE IF EXISTS entities;
