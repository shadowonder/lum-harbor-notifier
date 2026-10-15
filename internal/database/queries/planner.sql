-- name: CreatePlannerItem :exec
INSERT INTO planner_items (
    entity_id,
    authority_type,
    work_item_id,
    workflow_id,
    workflow_step_id,
    related_entity_id,
    monitor_point_id,
    item_type,
    title,
    details,
    status,
    scheduled_at,
    due_at,
    scheduled_date,
    due_date,
    context_status,
    missing_context_json,
    last_revision_id,
    created_at,
    updated_at,
    completed_at
)
VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
);


-- name: GetPlannerItemByID :one
SELECT
    p.entity_id,
    p.authority_type,
    p.work_item_id,
    p.workflow_id,
    p.workflow_step_id,
    p.related_entity_id,
    p.monitor_point_id,
    p.item_type,
    p.title,
    p.details,
    p.status,
    p.scheduled_at,
    p.due_at,
    p.scheduled_date,
    p.due_date,
    p.context_status,
    p.missing_context_json,
    p.last_revision_id,
    p.created_at,
    p.updated_at,
    p.completed_at,
    p.local_note
FROM planner_items p
INNER JOIN entities e ON e.id = p.entity_id
WHERE
    p.entity_id = ?
    AND e.archived_at IS NULL;


-- name: CompletePlannerItem :one
--
-- Marks an OPEN Planner Item as completed.
--
-- Business decisions such as whether the item is LOCAL or EXTERNAL
-- belong to PlannerService. This query only performs the persistence
-- transition from OPEN to COMPLETED.
UPDATE planner_items
SET
    status = 'COMPLETED',
    completed_at = sqlc.arg(completed_at),
    updated_at = sqlc.arg(updated_at),
    last_revision_id = sqlc.arg(last_revision_id)
WHERE entity_id = sqlc.arg(entity_id)
  AND status = 'OPEN'
RETURNING
    entity_id,
    authority_type,
    work_item_id,
    workflow_id,
    workflow_step_id,
    related_entity_id,
    monitor_point_id,
    item_type,
    title,
    details,
    status,
    scheduled_at,
    due_at,
    scheduled_date,
    due_date,
    context_status,
    missing_context_json,
    last_revision_id,
    created_at,
    updated_at,
    completed_at,
    local_note;

-- name: UpdateExternalPlannerItemNativeState :one
--
-- Refreshes the externally-authoritative native fields of an EXTERNAL
-- Planner Item (title, details, due shape, status, completed_at). The
-- authority_type = 'EXTERNAL' guard is defense in depth: PlannerService
-- already verifies authority before calling this, but a LOCAL row can never
-- match this statement even if that check were ever bypassed. No row matches
-- (sql.ErrNoRows) both for a nonexistent entity_id and for an existing LOCAL
-- one; the Service distinguishes those by reading first.
-- scheduled_at/scheduled_date are Lum Harbor-only concepts and are
-- deliberately never referenced here.
UPDATE planner_items
SET
    title = sqlc.arg(title),
    details = sqlc.arg(details),
    due_at = sqlc.arg(due_at),
    due_date = sqlc.arg(due_date),
    status = sqlc.arg(status),
    completed_at = sqlc.arg(completed_at),
    updated_at = sqlc.arg(updated_at),
    last_revision_id = sqlc.arg(last_revision_id)
WHERE entity_id = sqlc.arg(entity_id)
  AND authority_type = 'EXTERNAL'
RETURNING
    entity_id,
    authority_type,
    work_item_id,
    workflow_id,
    workflow_step_id,
    related_entity_id,
    monitor_point_id,
    item_type,
    title,
    details,
    status,
    scheduled_at,
    due_at,
    scheduled_date,
    due_date,
    context_status,
    missing_context_json,
    last_revision_id,
    created_at,
    updated_at,
    completed_at,
    local_note;

-- name: ListCurrentOpenPlannerItems :many
SELECT
    p.entity_id,
    p.authority_type,
    p.work_item_id,
    p.workflow_id,
    p.workflow_step_id,
    p.related_entity_id,
    p.monitor_point_id,
    p.item_type,
    p.title,
    p.details,
    p.status,
    p.scheduled_at,
    p.due_at,
    p.scheduled_date,
    p.due_date,
    p.context_status,
    p.missing_context_json,
    p.last_revision_id,
    p.created_at,
    p.updated_at,
    p.completed_at,
    p.local_note
FROM planner_items p
INNER JOIN entities e ON e.id = p.entity_id
WHERE
    e.archived_at IS NULL
    AND p.status = 'OPEN'
    AND (
        (p.scheduled_at IS NULL AND p.due_at IS NULL AND p.scheduled_date IS NULL AND p.due_date IS NULL)
        OR (p.scheduled_at IS NOT NULL AND p.scheduled_at < sqlc.arg(tomorrow_start))
        OR (p.due_at IS NOT NULL AND p.due_at < sqlc.arg(tomorrow_start))
        OR (p.scheduled_date IS NOT NULL AND p.scheduled_date <= sqlc.arg(today_date))
        OR (p.due_date IS NOT NULL AND p.due_date <= sqlc.arg(today_date))
    )
ORDER BY
    p.created_at ASC;

-- name: ListCompletedPlannerItemsByRange :many
SELECT
    p.entity_id,
    p.authority_type,
    p.work_item_id,
    p.workflow_id,
    p.workflow_step_id,
    p.related_entity_id,
    p.monitor_point_id,
    p.item_type,
    p.title,
    p.details,
    p.status,
    p.scheduled_at,
    p.due_at,
    p.scheduled_date,
    p.due_date,
    p.context_status,
    p.missing_context_json,
    p.last_revision_id,
    p.created_at,
    p.updated_at,
    p.completed_at,
    p.local_note
FROM planner_items p
INNER JOIN entities e ON e.id = p.entity_id
WHERE
    e.archived_at IS NULL
    AND p.status = 'COMPLETED'
    AND p.completed_at >= sqlc.arg(range_start)
    AND p.completed_at < sqlc.arg(range_end)
ORDER BY
    p.completed_at DESC;

-- name: ListPlannerItemsByRange :many
SELECT
    p.entity_id,
    p.authority_type,
    p.work_item_id,
    p.workflow_id,
    p.workflow_step_id,
    p.related_entity_id,
    p.monitor_point_id,
    p.item_type,
    p.title,
    p.details,
    p.status,
    p.scheduled_at,
    p.due_at,
    p.scheduled_date,
    p.due_date,
    p.context_status,
    p.missing_context_json,
    p.last_revision_id,
    p.created_at,
    p.updated_at,
    p.completed_at,
    p.local_note
FROM planner_items p
INNER JOIN entities e ON e.id = p.entity_id
WHERE
    e.archived_at IS NULL
AND ((
    p.scheduled_at IS NOT NULL
    AND p.scheduled_at >= sqlc.arg(range_start_at)
    AND p.scheduled_at < sqlc.arg(range_end_at)
  ) OR (
    p.due_at IS NOT NULL
    AND p.due_at >= sqlc.arg(range_start_at)
    AND p.due_at < sqlc.arg(range_end_at)
  ) OR (
    p.scheduled_date IS NOT NULL
    AND p.scheduled_date >= sqlc.arg(range_start_date)
    AND p.scheduled_date < sqlc.arg(range_end_date)
  ) OR (
    p.due_date IS NOT NULL
    AND p.due_date >= sqlc.arg(range_start_date)
    AND p.due_date < sqlc.arg(range_end_date)
))
ORDER BY
    p.scheduled_date ASC,
    p.due_date ASC,
    p.scheduled_at ASC,
    p.due_at ASC,
    p.created_at ASC;

-- name: SearchPlannerItemsByTitle :many
--
-- Bounded, case-insensitive, literal (no wildcard) substring search on title
-- for the readcap entity.search capability (M15 Slice 1). instr() performs a
-- plain substring containment check, so unlike LIKE, no character in query
-- (including '%'/'_') is ever treated as a wildcard.
SELECT
    p.entity_id,
    p.authority_type,
    p.work_item_id,
    p.workflow_id,
    p.workflow_step_id,
    p.related_entity_id,
    p.monitor_point_id,
    p.item_type,
    p.title,
    p.details,
    p.status,
    p.scheduled_at,
    p.due_at,
    p.scheduled_date,
    p.due_date,
    p.context_status,
    p.missing_context_json,
    p.last_revision_id,
    p.created_at,
    p.updated_at,
    p.completed_at,
    p.local_note
FROM planner_items p
INNER JOIN entities e ON e.id = p.entity_id
WHERE e.archived_at IS NULL
  AND instr(lower(p.title), lower(sqlc.arg(query))) > 0
ORDER BY
    p.title COLLATE NOCASE ASC,
    p.entity_id ASC
LIMIT sqlc.arg(row_limit);

-- name: ListOpenTrackingTodoDueTargets :many
--
-- Set-based input for the WorkItem overview's derived TrackedItem health.
-- Only current, active TrackedItems and open, non-archived local Todos with a
-- usable due target participate. Planner remains the authority for Todo state;
-- the overview owns the presentation thresholds.
SELECT
    p.entity_id AS planner_item_id,
    ti.entity_id AS tracked_item_id,
    ti.work_item_id,
    p.due_at,
    p.due_date
FROM planner_items p
INNER JOIN entities pe ON pe.id = p.entity_id
INNER JOIN tracked_items ti ON ti.entity_id = p.related_entity_id
INNER JOIN entities te ON te.id = ti.entity_id
WHERE pe.archived_at IS NULL
  AND te.archived_at IS NULL
  AND p.authority_type = 'LOCAL'
  AND p.status = 'OPEN'
  AND ti.lifecycle_state = 'ACTIVE'
  AND (p.due_at IS NOT NULL OR p.due_date IS NOT NULL)
ORDER BY ti.entity_id ASC, p.due_at ASC, p.due_date ASC, p.entity_id ASC;

-- name: ListOpenPlannerItemsByTrackedItemID :many
SELECT
    p.entity_id,
    p.authority_type,
    p.work_item_id,
    p.workflow_id,
    p.workflow_step_id,
    p.related_entity_id,
    p.monitor_point_id,
    p.item_type,
    p.title,
    p.details,
    p.status,
    p.scheduled_at,
    p.due_at,
    p.scheduled_date,
    p.due_date,
    p.context_status,
    p.missing_context_json,
    p.last_revision_id,
    p.created_at,
    p.updated_at,
    p.completed_at,
    p.local_note
FROM planner_items p
INNER JOIN entities e ON e.id = p.entity_id
WHERE e.archived_at IS NULL
  AND p.authority_type = 'LOCAL'
  AND p.status = 'OPEN'
  AND p.related_entity_id = sqlc.arg(tracked_item_id)
ORDER BY p.created_at DESC, p.entity_id ASC
LIMIT 20;

-- name: HasOpenTrackingTodoFromSourceRecord :one
SELECT EXISTS (
    SELECT 1
    FROM planner_items p
    INNER JOIN entities e ON e.id = p.entity_id
    INNER JOIN revision_sources rs ON rs.revision_id = p.last_revision_id
    INNER JOIN source_revisions sr ON sr.id = rs.source_revision_id
    WHERE e.archived_at IS NULL
      AND p.authority_type = 'LOCAL'
      AND p.status = 'OPEN'
      AND p.related_entity_id = sqlc.arg(tracked_item_id)
      AND sr.source_record_id = sqlc.arg(source_record_id)
);

-- name: UpdatePlannerItemLocalNote :one
--
-- Replaces only the Lum Harbor-owned local_note of one Planner Item. No
-- external-authoritative column (title/details/due/status/completed_at) is
-- referenced; authority and stale checks belong to PlannerService.
UPDATE planner_items
SET
    local_note = sqlc.arg(local_note),
    updated_at = sqlc.arg(updated_at),
    last_revision_id = sqlc.arg(last_revision_id)
WHERE entity_id = sqlc.arg(entity_id)
RETURNING
    entity_id,
    authority_type,
    work_item_id,
    workflow_id,
    workflow_step_id,
    related_entity_id,
    monitor_point_id,
    item_type,
    title,
    details,
    status,
    scheduled_at,
    due_at,
    scheduled_date,
    due_date,
    context_status,
    missing_context_json,
    last_revision_id,
    created_at,
    updated_at,
    completed_at,
    local_note;
