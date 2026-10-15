-- name: CreateHistoryEvent :exec
INSERT INTO history_events (
    id, event_type, scope, actor_type, summary,
    subject_entity_id, work_item_id, revision_id, proposal_id, ai_run_id, source_revision_id,
    correlation_id, causation_event_id, before_json, after_json, occurred_at, recorded_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetHistoryEventByID :one
SELECT * FROM history_events WHERE id = ?;

-- name: ListRecentHistoryEvents :many
SELECT * FROM history_events
ORDER BY occurred_at DESC, id DESC
LIMIT ?;

-- name: ListHistoryEventsByCorrelation :many
SELECT * FROM history_events
WHERE correlation_id = ?
ORDER BY occurred_at ASC, id ASC
LIMIT ?;

-- name: ListHistoryEventsByWorkItem :many
SELECT * FROM history_events
WHERE work_item_id = ?
ORDER BY occurred_at DESC, id DESC
LIMIT ?;

-- name: ListHistoryEventsBySubject :many
SELECT * FROM history_events
WHERE subject_entity_id = ?
ORDER BY occurred_at DESC, id DESC
LIMIT ?;

-- name: FindLatestHistoryEventBySubjectAndType :one
SELECT * FROM history_events
WHERE subject_entity_id = ? AND event_type = ?
ORDER BY occurred_at DESC, id DESC
LIMIT 1;

-- name: FindTerminalHistoryEventByAIRunID :one
SELECT * FROM history_events
WHERE ai_run_id = ? AND event_type IN ('AI_RUN_SUCCEEDED', 'AI_RUN_FAILED')
ORDER BY occurred_at DESC, id DESC
LIMIT 1;

-- name: QueryHistoryEvents :many
SELECT * FROM history_events
WHERE occurred_at >= sqlc.arg(since)
  AND (sqlc.arg(scope) = '' OR scope = sqlc.arg(scope))
  AND (
    sqlc.arg(search) = ''
    OR instr(lower(summary), sqlc.arg(search)) > 0
    OR instr(lower(event_type), sqlc.arg(search)) > 0
    OR instr(lower(correlation_id), sqlc.arg(search)) > 0
    OR instr(lower(COALESCE(subject_entity_id, '')), sqlc.arg(search)) > 0
    OR instr(lower(COALESCE(work_item_id, '')), sqlc.arg(search)) > 0
    OR instr(lower(COALESCE(revision_id, '')), sqlc.arg(search)) > 0
    OR instr(lower(COALESCE(proposal_id, '')), sqlc.arg(search)) > 0
    OR instr(lower(COALESCE(ai_run_id, '')), sqlc.arg(search)) > 0
    OR instr(lower(COALESCE(source_revision_id, '')), sqlc.arg(search)) > 0
  )
ORDER BY occurred_at DESC, id DESC
LIMIT sqlc.arg(result_limit);

-- name: ListHistoryTraceSummaries :many
SELECT
  correlation_id,
  COUNT(*) AS event_count,
  MIN(occurred_at) AS first_occurred_at,
  MAX(occurred_at) AS last_occurred_at,
  (SELECT summary FROM history_events AS first_event WHERE first_event.correlation_id = history_events.correlation_id ORDER BY occurred_at ASC, id ASC LIMIT 1) AS representative_summary,
  (SELECT scope FROM history_events AS first_event WHERE first_event.correlation_id = history_events.correlation_id ORDER BY occurred_at ASC, id ASC LIMIT 1) AS representative_scope,
  (SELECT work_item_id FROM history_events AS first_event WHERE first_event.correlation_id = history_events.correlation_id AND work_item_id IS NOT NULL ORDER BY occurred_at ASC, id ASC LIMIT 1) AS work_item_id,
  GROUP_CONCAT(DISTINCT scope) AS scopes
FROM history_events
GROUP BY correlation_id
HAVING COUNT(*) >= 2
ORDER BY last_occurred_at DESC, correlation_id DESC
LIMIT ?;

-- name: CountHistoryEventsByCorrelation :one
SELECT COUNT(*) FROM history_events WHERE correlation_id = ?;

-- name: ListHistoryEventsByCausationID :many
SELECT * FROM history_events
WHERE causation_event_id = ?
ORDER BY occurred_at ASC, id ASC;

-- name: ListHistoryEventsBySourceRevisionID :many
SELECT * FROM history_events
WHERE source_revision_id = ?
ORDER BY occurred_at ASC, id ASC;

-- name: ListHistoryEventsByRevisionID :many
SELECT * FROM history_events
WHERE revision_id = ?
ORDER BY occurred_at ASC, id ASC;
