-- name: CreateTrackingReevaluationRequest :execrows
INSERT INTO tracking_reevaluation_requests (
    id, planner_item_id, completion_revision_id, tracked_item_id,
    state, attempt_count, created_at, updated_at
)
SELECT
    sqlc.arg(id), p.entity_id, sqlc.arg(completion_revision_id), p.related_entity_id,
    'PENDING', 0, sqlc.arg(created_at), sqlc.arg(updated_at)
FROM planner_items AS p
JOIN tracked_items AS t ON t.entity_id = p.related_entity_id
JOIN entities AS e ON e.id = t.entity_id AND e.entity_type = 'TRACKED_ITEM'
WHERE p.entity_id = sqlc.arg(planner_item_id)
  AND p.item_type = 'TODO'
  AND p.status = 'COMPLETED'
  AND p.last_revision_id = sqlc.arg(completion_revision_id)
  AND p.related_entity_id IS NOT NULL
  AND (p.work_item_id IS NULL OR p.work_item_id = t.work_item_id)
  AND (p.workflow_id IS NULL OR p.workflow_id = t.workflow_id)
ON CONFLICT(completion_revision_id) DO NOTHING;

-- name: GetTrackingReevaluationRequest :one
SELECT * FROM tracking_reevaluation_requests WHERE id = ?;

-- name: GetTrackingReevaluationRequestByCompletionRevision :one
SELECT * FROM tracking_reevaluation_requests WHERE completion_revision_id = ?;

-- name: ListPendingTrackingReevaluationRequests :many
SELECT * FROM tracking_reevaluation_requests
WHERE state = 'PENDING'
ORDER BY created_at, id
LIMIT ?;

-- name: ClaimTrackingReevaluationRequest :execrows
UPDATE tracking_reevaluation_requests
SET state = 'PROCESSING',
    attempt_count = attempt_count + 1,
    started_at = sqlc.arg(started_at),
    completed_at = NULL,
    last_error = NULL,
    proposal_id = NULL,
    ai_run_id = NULL,
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND state = 'PENDING';

-- name: CompleteTrackingReevaluationRequest :execrows
UPDATE tracking_reevaluation_requests
SET state = 'COMPLETED',
    completed_at = sqlc.arg(completed_at),
    last_error = NULL,
    proposal_id = sqlc.arg(proposal_id),
    ai_run_id = sqlc.arg(ai_run_id),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND state = 'PROCESSING';

-- name: FailTrackingReevaluationRequest :execrows
UPDATE tracking_reevaluation_requests
SET state = 'FAILED',
    completed_at = sqlc.arg(completed_at),
    last_error = sqlc.arg(last_error),
    proposal_id = NULL,
    ai_run_id = sqlc.arg(ai_run_id),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND state = 'PROCESSING';

-- name: RecoverInterruptedTrackingReevaluations :execrows
UPDATE tracking_reevaluation_requests
SET state = 'PENDING',
    started_at = NULL,
    completed_at = NULL,
    last_error = 'interrupted while processing; recovered at startup',
    proposal_id = NULL,
    ai_run_id = NULL,
    updated_at = sqlc.arg(updated_at)
WHERE state = 'PROCESSING';

-- name: RetryFailedTrackingReevaluationRequest :execrows
UPDATE tracking_reevaluation_requests
SET state = 'PENDING',
    started_at = NULL,
    completed_at = NULL,
    proposal_id = NULL,
    ai_run_id = NULL,
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND state = 'FAILED';
