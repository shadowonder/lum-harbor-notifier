-- name: CreateWorkItem :exec
INSERT INTO work_items (
    entity_id,
    title,
    description,
    lifecycle_state,
    completed_at,
    last_revision_id,
    created_at,
    updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateWorkItemDetails :one
UPDATE work_items
SET
    title = sqlc.arg(title),
    description = sqlc.arg(description),
    last_revision_id = sqlc.arg(last_revision_id),
    updated_at = sqlc.arg(updated_at)
WHERE entity_id = sqlc.arg(entity_id)
  AND EXISTS (
      SELECT 1
      FROM entities e
      WHERE e.id = work_items.entity_id
        AND e.archived_at IS NULL
  )
RETURNING
    entity_id,
    title,
    description,
    lifecycle_state,
    completed_at,
    last_revision_id,
    created_at,
    updated_at;

-- name: UpdateWorkItemLifecycle :one
UPDATE work_items
SET
    lifecycle_state = sqlc.arg(lifecycle_state),
    completed_at = sqlc.narg(completed_at),
    last_revision_id = sqlc.arg(last_revision_id),
    updated_at = sqlc.arg(updated_at)
WHERE entity_id = sqlc.arg(entity_id)
  AND EXISTS (
      SELECT 1
      FROM entities e
      WHERE e.id = work_items.entity_id
        AND e.archived_at IS NULL
  )
RETURNING
    entity_id,
    title,
    description,
    lifecycle_state,
    completed_at,
    last_revision_id,
    created_at,
    updated_at;

-- name: GetWorkItemByID :one
SELECT
    w.entity_id,
    w.title,
    w.description,
    w.lifecycle_state,
    w.completed_at,
    w.last_revision_id,
    w.created_at,
    w.updated_at
FROM work_items w
INNER JOIN entities e ON e.id = w.entity_id
WHERE
    w.entity_id = ?
    AND e.archived_at IS NULL;

-- name: ListWorkItems :many
SELECT
    w.entity_id,
    w.title,
    w.description,
    w.lifecycle_state,
    w.completed_at,
    w.last_revision_id,
    w.created_at,
    w.updated_at
FROM work_items w
INNER JOIN entities e ON e.id = w.entity_id
WHERE e.archived_at IS NULL
ORDER BY
    w.created_at ASC,
    w.entity_id ASC;

-- name: SearchWorkItemsByTitle :many
--
-- Bounded, case-insensitive, literal (no wildcard) substring search on title
-- for the readcap entity.search capability (M15 Slice 1). instr() performs a
-- plain substring containment check, so unlike LIKE, no character in query
-- (including '%'/'_') is ever treated as a wildcard.
SELECT
    w.entity_id,
    w.title,
    w.description,
    w.lifecycle_state,
    w.completed_at,
    w.last_revision_id,
    w.created_at,
    w.updated_at
FROM work_items w
INNER JOIN entities e ON e.id = w.entity_id
WHERE e.archived_at IS NULL
  AND instr(lower(w.title), lower(sqlc.arg(query))) > 0
ORDER BY
    w.title COLLATE NOCASE ASC,
    w.entity_id ASC
LIMIT sqlc.arg(row_limit);
