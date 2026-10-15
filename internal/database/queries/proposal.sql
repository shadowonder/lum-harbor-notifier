-- name: CreateProposal :exec
INSERT INTO change_proposals (
    id,
    target_entity_id,
    operation_type,
    title,
    summary,
    reason,
    status,
    operation_payload_json,
    applied_revision_id,
    ai_run_id,
    created_at,
    updated_at,
    resolved_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetProposalByID :one
SELECT
    id,
    target_entity_id,
    operation_type,
    title,
    summary,
    reason,
    status,
    operation_payload_json,
    applied_revision_id,
    ai_run_id,
    created_at,
    updated_at,
    resolved_at
FROM change_proposals
WHERE id = ?;

-- name: ListPendingProposals :many
SELECT
    id,
    target_entity_id,
    operation_type,
    title,
    summary,
    reason,
    status,
    operation_payload_json,
    applied_revision_id,
    ai_run_id,
    created_at,
    updated_at,
    resolved_at
FROM change_proposals
WHERE status = 'PENDING'
ORDER BY
    created_at ASC,
    id ASC;

-- name: TransitionProposalStatus :one
UPDATE change_proposals
SET
    status = sqlc.arg(to_status),
    resolved_at = sqlc.narg(resolved_at),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
  AND status = sqlc.arg(from_status)
RETURNING
    id,
    target_entity_id,
    operation_type,
    title,
    summary,
    reason,
    status,
    operation_payload_json,
    applied_revision_id,
    ai_run_id,
    created_at,
    updated_at,
    resolved_at;

-- name: MarkProposalAppliedIfPending :one
UPDATE change_proposals
SET
    status = 'APPLIED',
    applied_revision_id = sqlc.arg(applied_revision_id),
    resolved_at = sqlc.arg(resolved_at),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
  AND status = 'PENDING'
RETURNING
    id,
    target_entity_id,
    operation_type,
    title,
    summary,
    reason,
    status,
    operation_payload_json,
    applied_revision_id,
    ai_run_id,
    created_at,
    updated_at,
    resolved_at;

-- name: MarkProposalStaleIfPending :one
UPDATE change_proposals
SET
    status = 'STALE',
    applied_revision_id = NULL,
    resolved_at = sqlc.arg(resolved_at),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id)
  AND status = 'PENDING'
RETURNING
    id,
    target_entity_id,
    operation_type,
    title,
    summary,
    reason,
    status,
    operation_payload_json,
    applied_revision_id,
    ai_run_id,
    created_at,
    updated_at,
    resolved_at;

-- name: CreateProposalSource :exec
INSERT INTO change_proposal_sources (
    proposal_id,
    source_revision_id,
    role
) VALUES (?, ?, ?);

-- name: ListProposalSources :many
SELECT proposal_id, source_revision_id, role
FROM change_proposal_sources
WHERE proposal_id = ?
ORDER BY source_revision_id ASC;

-- name: CreateRevisionSource :exec
INSERT INTO revision_sources (
    revision_id,
    source_revision_id,
    role
) VALUES (?, ?, ?);

-- name: ListRevisionSources :many
SELECT revision_id, source_revision_id, role
FROM revision_sources
WHERE revision_id = ?
ORDER BY source_revision_id ASC;

-- name: FindPendingProposalBySourceRevision :one
SELECT
    p.id,
    p.target_entity_id,
    p.operation_type,
    p.title,
    p.summary,
    p.reason,
    p.status,
    p.operation_payload_json,
    p.applied_revision_id,
    p.ai_run_id,
    p.created_at,
    p.updated_at,
    p.resolved_at
FROM change_proposals p
JOIN change_proposal_sources s ON p.id = s.proposal_id
WHERE s.source_revision_id = ?
  AND p.status = 'PENDING'
LIMIT 1;


-- name: CreateProposalOperation :exec
INSERT INTO proposal_operations (
    id,
    proposal_id,
    sequence_no,
    operation_type,
    target_entity_id,
    operation_payload_json
) VALUES (?, ?, ?, ?, ?, ?);

-- name: ListProposalOperations :many
SELECT
    id,
    proposal_id,
    sequence_no,
    operation_type,
    target_entity_id,
    operation_payload_json
FROM proposal_operations
WHERE proposal_id = ?
ORDER BY sequence_no ASC;

-- name: CreateProposalOperationResult :exec
INSERT INTO proposal_operation_results (
    proposal_operation_id,
    sequence_no,
    entity_id,
    revision_id,
    result_role
) VALUES (?, ?, ?, ?, ?);

-- name: ListProposalOperationResults :many
SELECT
    r.proposal_operation_id,
    r.sequence_no,
    r.entity_id,
    r.revision_id,
    r.result_role
FROM proposal_operation_results r
JOIN proposal_operations o ON o.id = r.proposal_operation_id
WHERE o.proposal_id = ?
ORDER BY
    o.sequence_no ASC,
    r.sequence_no ASC;
