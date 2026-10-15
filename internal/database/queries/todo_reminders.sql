-- ============================================================
-- Todo Reminders Queries (M18)
-- ============================================================

-- name: CreateTodoReminder :exec
INSERT INTO todo_reminders (
    entity_id, planner_item_id, state, offsets_seconds_json, fired_offsets_json,
    remind_mode, last_revision_id, created_at, updated_at, satisfied_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetTodoReminderByPlannerItemID :one
SELECT * FROM todo_reminders
WHERE planner_item_id = ?;

-- name: GetTodoReminderByID :one
SELECT * FROM todo_reminders
WHERE entity_id = ?;

-- name: ListActiveTodoReminders :many
SELECT * FROM todo_reminders
WHERE state = 'ACTIVE'
ORDER BY created_at ASC;

-- name: UpdateTodoReminderFiredOffsets :exec
UPDATE todo_reminders
SET fired_offsets_json = sqlc.arg(fired_offsets_json),
    last_revision_id = sqlc.arg(last_revision_id),
    updated_at = sqlc.arg(updated_at)
WHERE entity_id = sqlc.arg(entity_id);

-- name: SetTodoReminderState :exec
UPDATE todo_reminders
SET state = sqlc.arg(state),
    satisfied_at = sqlc.narg(satisfied_at),
    last_revision_id = sqlc.arg(last_revision_id),
    updated_at = sqlc.arg(updated_at)
WHERE entity_id = sqlc.arg(entity_id);

-- name: UpdateTodoReminderConfiguration :exec
UPDATE todo_reminders
SET offsets_seconds_json = sqlc.arg(offsets_seconds_json),
    fired_offsets_json = sqlc.arg(fired_offsets_json),
    remind_mode = sqlc.arg(remind_mode),
    state = sqlc.arg(state),
    last_revision_id = sqlc.arg(last_revision_id),
    updated_at = sqlc.arg(updated_at)
WHERE entity_id = sqlc.arg(entity_id);

-- name: DeleteTodoReminder :exec
DELETE FROM todo_reminders
WHERE entity_id = ?;

