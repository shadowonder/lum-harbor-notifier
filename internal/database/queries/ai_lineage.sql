-- name: CreateAIRun :exec
INSERT INTO ai_runs (
    id,
    processing_id,
    request_id,
    action,
    status,
    canonical_request_json,
    upstream_run_id,
    started_at
)
VALUES (?, ?, ?, ?, 'RUNNING', ?, ?, ?);

-- name: CreateAIRunSource :exec
INSERT INTO ai_run_sources (
    ai_run_id,
    source_revision_id,
    role
)
VALUES (?, ?, ?);

-- name: FinalizeAIRunSucceeded :execrows
UPDATE ai_runs
SET
    status = 'SUCCEEDED',
    output_json = sqlc.arg(output_json),
    error_text = NULL,
    provider_invoked = sqlc.arg(provider_invoked),
    actual_provider_id = sqlc.arg(actual_provider_id),
    actual_provider_name = sqlc.arg(actual_provider_name),
    actual_provider_source = sqlc.arg(actual_provider_source),
    response_origin_type = sqlc.arg(response_origin_type),
    response_origin_id = sqlc.arg(response_origin_id),
    response_origin_name = sqlc.arg(response_origin_name),
    completed_at = sqlc.arg(completed_at)
WHERE id = sqlc.arg(id)
  AND status = 'RUNNING';

-- name: FinalizeAIRunFailed :execrows
UPDATE ai_runs
SET
    status = 'FAILED',
    output_json = NULL,
    error_text = sqlc.arg(error_text),
    provider_invoked = sqlc.arg(provider_invoked),
    actual_provider_id = sqlc.arg(actual_provider_id),
    actual_provider_name = sqlc.arg(actual_provider_name),
    actual_provider_source = sqlc.arg(actual_provider_source),
    response_origin_type = sqlc.arg(response_origin_type),
    response_origin_id = sqlc.arg(response_origin_id),
    response_origin_name = sqlc.arg(response_origin_name),
    completed_at = sqlc.arg(completed_at)
WHERE id = sqlc.arg(id)
  AND status = 'RUNNING';

-- name: GetAIRun :one
SELECT
    id,
    processing_id,
    request_id,
    action,
    status,
    canonical_request_json,
    output_json,
    error_text,
    provider_invoked,
    actual_provider_id,
    actual_provider_name,
    actual_provider_source,
    response_origin_type,
    response_origin_id,
    response_origin_name,
    upstream_run_id,
    started_at,
    completed_at
FROM ai_runs
WHERE id = ?;

-- name: ListAIRunsForProcessing :many
SELECT
    id,
    processing_id,
    request_id,
    action,
    status,
    canonical_request_json,
    output_json,
    error_text,
    provider_invoked,
    actual_provider_id,
    actual_provider_name,
    actual_provider_source,
    response_origin_type,
    response_origin_id,
    response_origin_name,
    upstream_run_id,
    started_at,
    completed_at
FROM ai_runs
WHERE processing_id = ?
ORDER BY started_at, id;

-- name: ListAIRunsForSourceRevision :many
SELECT
    r.id,
    r.processing_id,
    r.request_id,
    r.action,
    r.status,
    r.canonical_request_json,
    r.output_json,
    r.error_text,
    r.provider_invoked,
    r.actual_provider_id,
    r.actual_provider_name,
    r.actual_provider_source,
    r.response_origin_type,
    r.response_origin_id,
    r.response_origin_name,
    r.upstream_run_id,
    r.started_at,
    r.completed_at
FROM ai_runs AS r
JOIN ai_run_sources AS s ON s.ai_run_id = r.id
WHERE s.source_revision_id = ?
ORDER BY r.started_at, r.id;

-- name: GetAIRunSource :one
SELECT ai_run_id, source_revision_id, role
FROM ai_run_sources
WHERE ai_run_id = ?
  AND source_revision_id = ?
  AND role = ?;

-- name: GetPrimarySourceRevisionForAIRun :one
SELECT source_revision_id
FROM ai_run_sources
WHERE ai_run_id = ? AND role = 'PRIMARY_SOURCE'
LIMIT 1;
