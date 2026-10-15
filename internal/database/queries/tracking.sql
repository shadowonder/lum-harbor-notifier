-- name: CreateWorkflowDefinition :exec
INSERT INTO workflow_definitions (id, name, version, active, created_at)
VALUES (?, ?, ?, ?, ?);

-- name: CreateWorkflowStep :exec
INSERT INTO workflow_steps (id, workflow_definition_id, step_key, title, sequence_no, terminal)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetWorkflowDefinitionByID :one
SELECT id, name, version, active, created_at
FROM workflow_definitions
WHERE id = ?;

-- name: ListWorkflowStepsByDefinition :many
SELECT id, workflow_definition_id, step_key, title, sequence_no, terminal
FROM workflow_steps
WHERE workflow_definition_id = ?
ORDER BY sequence_no ASC;

-- name: CreateWorkflow :exec
INSERT INTO workflows (
    id, work_item_id, workflow_definition_id, current_step_id, started_at,
    last_revision_id, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: SetWorkflowCurrentStep :exec
UPDATE workflows
SET current_step_id = ?, last_revision_id = ?, updated_at = ?
WHERE id = ?;

-- name: GetWorkflowByID :one
SELECT id, work_item_id, workflow_definition_id, current_step_id, started_at, last_revision_id, created_at, updated_at
FROM workflows
WHERE id = ?;

-- name: GetWorkflowByWorkItem :one
SELECT w.id, w.work_item_id, w.workflow_definition_id, w.current_step_id, w.started_at, w.last_revision_id, w.created_at, w.updated_at
FROM workflows w
INNER JOIN work_items wi ON wi.entity_id = w.work_item_id
INNER JOIN entities e ON e.id = wi.entity_id
WHERE w.work_item_id = ? AND e.archived_at IS NULL;

-- name: ListWorkflowDefinitionsForWorkItem :many
SELECT d.id, d.name, d.version, d.active, d.created_at
FROM workflow_definitions d
INNER JOIN workflows w ON w.workflow_definition_id = d.id
INNER JOIN work_items wi ON wi.entity_id = w.work_item_id
INNER JOIN entities e ON e.id = wi.entity_id
WHERE w.work_item_id = sqlc.arg(work_item_id)
  AND e.archived_at IS NULL;

-- name: ListWorkflowStepsForWorkItem :many
SELECT s.id, s.workflow_definition_id, s.step_key, s.title, s.sequence_no, s.terminal
FROM workflow_steps s
INNER JOIN workflows w ON w.workflow_definition_id = s.workflow_definition_id
INNER JOIN work_items wi ON wi.entity_id = w.work_item_id
INNER JOIN entities e ON e.id = wi.entity_id
WHERE w.work_item_id = sqlc.arg(work_item_id)
  AND e.archived_at IS NULL
ORDER BY s.sequence_no ASC;

-- name: CreateTrackedItem :exec
INSERT INTO tracked_items (
    entity_id, work_item_id, workflow_id, parent_item_id, title, item_type, lifecycle_state,
    completed_at, last_revision_id, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetTrackedItemByID :one
SELECT
    t.entity_id, t.work_item_id, t.workflow_id, t.parent_item_id, t.title, t.item_type,
    t.lifecycle_state, t.completed_at,
    t.last_revision_id, t.created_at, t.updated_at
FROM tracked_items t
INNER JOIN entities e ON e.id = t.entity_id
WHERE t.entity_id = ? AND e.archived_at IS NULL;

-- name: ListTrackedItemsByWorkItem :many
SELECT
    t.entity_id, t.work_item_id, t.workflow_id, t.parent_item_id, t.title, t.item_type,
    t.lifecycle_state, t.completed_at,
    t.last_revision_id, t.created_at, t.updated_at
FROM tracked_items t
INNER JOIN entities e ON e.id = t.entity_id
WHERE t.work_item_id = ? AND e.archived_at IS NULL
ORDER BY t.created_at ASC, t.entity_id ASC;

-- name: ListWorkflowsForWorkItemOverview :many
SELECT
    w.id, w.work_item_id, w.workflow_definition_id, w.current_step_id, w.started_at, w.last_revision_id, w.created_at, w.updated_at
FROM workflows w
INNER JOIN work_items wi ON wi.entity_id = w.work_item_id
INNER JOIN entities we ON we.id = wi.entity_id
WHERE we.archived_at IS NULL
ORDER BY w.work_item_id ASC;

-- name: ListWorkflowDefinitionsForOverview :many
SELECT d.id, d.name, d.version, d.active, d.created_at
FROM workflow_definitions d
WHERE d.id IN (
    SELECT DISTINCT w.workflow_definition_id
    FROM workflows w
    INNER JOIN work_items wi ON wi.entity_id = w.work_item_id
    INNER JOIN entities we ON we.id = wi.entity_id
    WHERE we.archived_at IS NULL
)
ORDER BY d.id ASC;

-- name: ListTrackedItemsForWorkItemOverview :many
SELECT
    t.entity_id, t.work_item_id, t.workflow_id, t.parent_item_id, t.title, t.item_type,
    t.lifecycle_state, t.completed_at,
    t.last_revision_id, t.created_at, t.updated_at
FROM tracked_items t
INNER JOIN entities e ON e.id = t.entity_id
INNER JOIN work_items w ON w.entity_id = t.work_item_id
INNER JOIN entities we ON we.id = w.entity_id
WHERE e.archived_at IS NULL AND we.archived_at IS NULL
ORDER BY t.work_item_id ASC, t.created_at ASC, t.entity_id ASC;

-- name: ListWorkflowStepsForWorkItemOverview :many
SELECT s.id, s.workflow_definition_id, s.step_key, s.title, s.sequence_no, s.terminal
FROM workflow_steps s
WHERE s.workflow_definition_id IN (
    SELECT DISTINCT w.workflow_definition_id
    FROM workflows w
    INNER JOIN work_items wi ON wi.entity_id = w.work_item_id
    INNER JOIN entities we ON we.id = wi.entity_id
    WHERE we.archived_at IS NULL
)
ORDER BY s.workflow_definition_id ASC, s.sequence_no ASC;

-- name: TouchTrackedItem :exec
UPDATE tracked_items
SET last_revision_id = sqlc.arg(last_revision_id),
    updated_at = sqlc.arg(updated_at)
WHERE entity_id = sqlc.arg(entity_id);

-- name: UpdateTrackedItemDetails :exec
UPDATE tracked_items
SET title = sqlc.arg(title),
    item_type = sqlc.narg(item_type),
    parent_item_id = sqlc.narg(parent_item_id),
    last_revision_id = sqlc.arg(last_revision_id),
    updated_at = sqlc.arg(updated_at)
WHERE entity_id = sqlc.arg(entity_id);

-- name: ArchiveTrackedItemEntity :exec
UPDATE entities
SET archived_at = sqlc.arg(archived_at)
WHERE id = sqlc.arg(entity_id) AND archived_at IS NULL;

-- name: CreateTrackedItemRelation :exec
INSERT INTO tracked_item_relations (
    workflow_id, from_tracked_item_id, to_tracked_item_id, relation_type,
    last_revision_id, created_at
) VALUES (?, ?, ?, 'DEPENDS_ON', ?, ?);

-- name: DeleteTrackedItemRelation :execrows
DELETE FROM tracked_item_relations
WHERE from_tracked_item_id = sqlc.arg(from_tracked_item_id)
  AND to_tracked_item_id = sqlc.arg(to_tracked_item_id)
  AND relation_type = 'DEPENDS_ON';

-- name: GetTrackedItemRelation :one
SELECT workflow_id, from_tracked_item_id, to_tracked_item_id, relation_type,
       last_revision_id, created_at
FROM tracked_item_relations
WHERE from_tracked_item_id = ? AND to_tracked_item_id = ?
  AND relation_type = 'DEPENDS_ON';

-- name: ListTrackedItemRelationsByWorkflow :many
SELECT r.workflow_id, r.from_tracked_item_id, r.to_tracked_item_id,
       r.relation_type, r.last_revision_id, r.created_at
FROM tracked_item_relations r
INNER JOIN entities ef ON ef.id = r.from_tracked_item_id
INNER JOIN entities et ON et.id = r.to_tracked_item_id
WHERE r.workflow_id = ? AND ef.archived_at IS NULL AND et.archived_at IS NULL
ORDER BY r.created_at ASC, r.from_tracked_item_id ASC, r.to_tracked_item_id ASC;

-- name: CreateTrackedItemStepState :exec
INSERT INTO tracked_item_step_states (
    tracked_item_id, workflow_step_id, state, entered_at, completed_at, last_revision_id
)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetTrackedItemStepState :one
SELECT tracked_item_id, workflow_step_id, state, entered_at, completed_at, last_revision_id
FROM tracked_item_step_states
WHERE tracked_item_id = ? AND workflow_step_id = ?;

-- name: UpdateTrackedItemStepState :exec
UPDATE tracked_item_step_states
SET state = sqlc.arg(state),
    entered_at = sqlc.narg(entered_at),
    completed_at = sqlc.narg(completed_at),
    last_revision_id = sqlc.arg(last_revision_id)
WHERE tracked_item_id = sqlc.arg(tracked_item_id)
  AND workflow_step_id = sqlc.arg(workflow_step_id);

-- name: GetWorkflowStepByID :one
SELECT id, workflow_definition_id, step_key, title, sequence_no, terminal
FROM workflow_steps
WHERE id = ?;

-- name: ListStepStatesByWorkItem :many
SELECT
    ss.tracked_item_id, ss.workflow_step_id, ss.state, ss.entered_at,
    ss.completed_at, ss.last_revision_id
FROM tracked_item_step_states ss
INNER JOIN tracked_items t ON t.entity_id = ss.tracked_item_id
INNER JOIN entities e ON e.id = t.entity_id
INNER JOIN workflow_steps s ON s.id = ss.workflow_step_id
WHERE t.work_item_id = ? AND e.archived_at IS NULL
ORDER BY ss.tracked_item_id ASC, s.sequence_no ASC;

-- name: ListStepStatesForWorkItemOverview :many
SELECT
    ss.tracked_item_id, ss.workflow_step_id, ss.state, ss.entered_at,
    ss.completed_at, ss.last_revision_id
FROM tracked_item_step_states ss
INNER JOIN tracked_items t ON t.entity_id = ss.tracked_item_id
INNER JOIN entities e ON e.id = t.entity_id
INNER JOIN work_items w ON w.entity_id = t.work_item_id
INNER JOIN entities we ON we.id = w.entity_id
INNER JOIN workflow_steps s ON s.id = ss.workflow_step_id
WHERE e.archived_at IS NULL AND we.archived_at IS NULL
ORDER BY ss.tracked_item_id ASC, s.sequence_no ASC;

-- name: ListActiveWorkflowDefinitions :many
SELECT id, name, version, active, created_at
FROM workflow_definitions
WHERE active = 1
ORDER BY name ASC, version ASC, id ASC;

-- name: ListStepsForActiveWorkflowDefinitions :many
SELECT s.id, s.workflow_definition_id, s.step_key, s.title, s.sequence_no, s.terminal
FROM workflow_steps s
INNER JOIN workflow_definitions d ON d.id = s.workflow_definition_id
WHERE d.active = 1
ORDER BY s.workflow_definition_id ASC, s.sequence_no ASC;

-- name: CreateMonitorPoint :exec
INSERT INTO monitor_points (
    entity_id, work_item_id, subject_tracked_item_id, monitor_kind, title, due_at, timezone,
    expected_kind, expected_work_item_lifecycle, expected_step_id, state, satisfied_at,
    last_revision_id, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetMonitorPointByID :one
SELECT
    m.entity_id, m.work_item_id, m.subject_tracked_item_id, m.monitor_kind, m.title, m.due_at, m.timezone,
    m.expected_kind, m.expected_work_item_lifecycle, m.expected_step_id, m.state, m.satisfied_at,
    m.last_revision_id, m.created_at, m.updated_at
FROM monitor_points m
INNER JOIN entities e ON e.id = m.entity_id
WHERE m.entity_id = ? AND e.archived_at IS NULL;

-- name: ListMonitorPointsByWorkItem :many
SELECT
    m.entity_id, m.work_item_id, m.subject_tracked_item_id, m.monitor_kind, m.title, m.due_at, m.timezone,
    m.expected_kind, m.expected_work_item_lifecycle, m.expected_step_id, m.state, m.satisfied_at,
    m.last_revision_id, m.created_at, m.updated_at
FROM monitor_points m
INNER JOIN entities e ON e.id = m.entity_id
WHERE m.work_item_id = ? AND e.archived_at IS NULL
ORDER BY m.due_at ASC, m.created_at ASC, m.entity_id ASC;

-- name: ListMonitorPointsForOverview :many
SELECT
    m.entity_id, m.work_item_id, m.subject_tracked_item_id, m.monitor_kind, m.title, m.due_at, m.timezone,
    m.expected_kind, m.expected_work_item_lifecycle, m.expected_step_id, m.state, m.satisfied_at,
    m.last_revision_id, m.created_at, m.updated_at
FROM monitor_points m
INNER JOIN entities e ON e.id = m.entity_id
INNER JOIN work_items w ON w.entity_id = m.work_item_id
INNER JOIN entities we ON we.id = w.entity_id
WHERE m.subject_tracked_item_id IS NULL
  AND e.archived_at IS NULL
  AND we.archived_at IS NULL
UNION ALL
SELECT
    m.entity_id, m.work_item_id, m.subject_tracked_item_id, m.monitor_kind, m.title, m.due_at, m.timezone,
    m.expected_kind, m.expected_work_item_lifecycle, m.expected_step_id, m.state, m.satisfied_at,
    m.last_revision_id, m.created_at, m.updated_at
FROM monitor_points m
INNER JOIN entities e ON e.id = m.entity_id
INNER JOIN tracked_items t ON t.entity_id = m.subject_tracked_item_id
INNER JOIN entities te ON te.id = t.entity_id
INNER JOIN work_items w ON w.entity_id = m.work_item_id
INNER JOIN entities we ON we.id = w.entity_id
WHERE m.subject_tracked_item_id IS NOT NULL
  AND e.archived_at IS NULL
  AND te.archived_at IS NULL
  AND we.archived_at IS NULL
ORDER BY 2 ASC, 6 ASC, 1 ASC;

-- name: CountActiveFinalTargets :one
SELECT COUNT(*)
FROM monitor_points
WHERE work_item_id = sqlc.arg(work_item_id)
  AND COALESCE(subject_tracked_item_id, '') = sqlc.arg(subject_key)
  AND monitor_kind = 'FINAL_TARGET'
  AND state = 'ACTIVE';

-- name: UpdateMonitorPointState :exec
UPDATE monitor_points
SET state = sqlc.arg(state),
    satisfied_at = sqlc.narg(satisfied_at),
    last_revision_id = sqlc.arg(last_revision_id),
    updated_at = sqlc.arg(updated_at)
WHERE entity_id = sqlc.arg(entity_id);

-- name: ListDueActiveMonitorPoints :many
SELECT
    m.entity_id, m.work_item_id, m.subject_tracked_item_id, m.monitor_kind, m.title, m.due_at, m.timezone,
    m.expected_kind, m.expected_work_item_lifecycle, m.expected_step_id, m.state, m.satisfied_at,
    m.last_revision_id, m.created_at, m.updated_at
FROM monitor_points m
INNER JOIN entities e ON e.id = m.entity_id
INNER JOIN work_items w ON w.entity_id = m.work_item_id
INNER JOIN entities we ON we.id = w.entity_id
WHERE m.state = 'ACTIVE'
  AND m.due_at <= ?
  AND e.archived_at IS NULL
  AND we.archived_at IS NULL
ORDER BY m.due_at ASC, m.entity_id ASC;
