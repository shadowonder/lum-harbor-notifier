-- name: CreateSourceRecord :exec
INSERT INTO source_records (
    id,
    extension_id,
    instance_id,
    source_kind,
    external_id,
    current_revision_id,
    current_content_hash,
    source_updated_at,
    first_seen_at,
    last_seen_at,
    created_at,
    updated_at
)
VALUES (?, ?, ?, ?, ?, NULL, NULL, NULL, ?, ?, ?, ?);

-- name: GetSourceRecordByIdentity :one
SELECT
    id,
    extension_id,
    instance_id,
    source_kind,
    external_id,
    current_revision_id,
    current_content_hash,
    source_updated_at,
    first_seen_at,
    last_seen_at,
    created_at,
    updated_at
FROM source_records
WHERE extension_id = sqlc.arg(extension_id)
  AND instance_id = sqlc.arg(instance_id)
  AND source_kind = sqlc.arg(source_kind)
  AND external_id = sqlc.arg(external_id);

-- name: GetSourceRecordByID :one
SELECT
    id,
    extension_id,
    instance_id,
    source_kind,
    external_id,
    current_revision_id,
    current_content_hash,
    source_updated_at,
    first_seen_at,
    last_seen_at,
    created_at,
    updated_at
FROM source_records
WHERE id = ?;

-- name: UpdateSourceRecordObservation :exec
UPDATE source_records
SET
    source_updated_at = sqlc.arg(source_updated_at),
    last_seen_at = sqlc.arg(last_seen_at),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: UpdateSourceRecordHead :exec
UPDATE source_records
SET
    current_revision_id = sqlc.arg(current_revision_id),
    current_content_hash = sqlc.arg(current_content_hash),
    source_updated_at = sqlc.arg(source_updated_at),
    last_seen_at = sqlc.arg(last_seen_at),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: CreateSourceRevision :one
INSERT INTO source_revisions (
    id,
    source_record_id,
    content_hash,
    payload_json,
    source_updated_at,
    observed_at,
    created_at
)
VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING revision_no;

-- name: GetSourceRevisionByID :one
SELECT
    revision_no,
    id,
    source_record_id,
    content_hash,
    payload_json,
    source_updated_at,
    observed_at,
    created_at
FROM source_revisions
WHERE id = ?;

-- name: GetCurrentSourceRevision :one
SELECT
    sr.revision_no,
    sr.id,
    sr.source_record_id,
    sr.content_hash,
    sr.payload_json,
    sr.source_updated_at,
    sr.observed_at,
    sr.created_at
FROM source_records AS r
JOIN source_revisions AS sr ON sr.id = r.current_revision_id
WHERE r.id = ?;

-- name: ListSourceRevisionsForRecord :many
SELECT
    revision_no,
    id,
    source_record_id,
    content_hash,
    payload_json,
    source_updated_at,
    observed_at,
    created_at
FROM source_revisions
WHERE source_record_id = ?
ORDER BY revision_no ASC;

-- name: GetSourceSyncState :one
SELECT
    extension_id,
    instance_id,
    capability_id,
    cursor,
    checkpoint_version,
    last_success_at,
    created_at,
    updated_at
FROM source_sync_state
WHERE extension_id = sqlc.arg(extension_id)
  AND instance_id = sqlc.arg(instance_id)
  AND capability_id = sqlc.arg(capability_id);

-- name: CreateSourceSyncStateCAS :execrows
INSERT INTO source_sync_state (
    extension_id,
    instance_id,
    capability_id,
    cursor,
    checkpoint_version,
    last_success_at,
    created_at,
    updated_at
)
VALUES (?, ?, ?, ?, 1, ?, ?, ?)
ON CONFLICT(extension_id, instance_id, capability_id) DO NOTHING;

-- name: AdvanceSourceSyncStateCAS :execrows
UPDATE source_sync_state
SET
    cursor = sqlc.arg(cursor),
    checkpoint_version = checkpoint_version + 1,
    last_success_at = sqlc.arg(last_success_at),
    updated_at = sqlc.arg(updated_at)
WHERE extension_id = sqlc.arg(extension_id)
  AND instance_id = sqlc.arg(instance_id)
  AND capability_id = sqlc.arg(capability_id)
  AND checkpoint_version = sqlc.arg(expected_checkpoint_version);

-- name: ListCurrentSourceHeadsBySourceKind :many
--
-- Bounded set-based read of one current-head fact per SourceRecord of the
-- requested source_kind, joined to its current SourceRevision in one query
-- (no N+1: never list record IDs and then fetch each current revision
-- separately). Relies on the Source Layer invariant that a committed record
-- always has a non-null current_revision_id, so this is an inner join, not a
-- defensive left join.
SELECT
    r.id AS record_id,
    r.extension_id,
    r.instance_id,
    r.source_kind,
    r.external_id,
    r.last_seen_at,
    sr.id AS revision_id,
    sr.content_hash,
    sr.payload_json,
    sr.source_updated_at,
    sr.observed_at
FROM source_records AS r
JOIN source_revisions AS sr ON sr.id = r.current_revision_id
WHERE r.source_kind = sqlc.arg(source_kind)
ORDER BY r.id ASC;

-- name: CreateSourceProcessingState :exec
INSERT INTO source_processing_state (
    source_revision_id,
    state,
    mode,
    started_at,
    completed_at,
    last_error,
    proposal_id,
    revision_id,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetSourceProcessingState :one
SELECT
    source_revision_id,
    state,
    mode,
    started_at,
    completed_at,
    last_error,
    proposal_id,
    revision_id,
    created_at,
    updated_at
FROM source_processing_state
WHERE source_revision_id = ?;

-- name: ListPendingSemanticSources :many
SELECT
    sps.source_revision_id,
    sps.state,
    sps.mode,
    sps.started_at,
    sps.completed_at,
    sps.last_error,
    sps.proposal_id,
    sps.revision_id,
    sps.created_at,
    sps.updated_at,
    sr.payload_json,
    sr.content_hash,
    r.extension_id,
    r.instance_id,
    r.source_kind,
    r.external_id
FROM source_processing_state sps
JOIN source_revisions sr ON sr.id = sps.source_revision_id
JOIN source_records r ON r.id = sr.source_record_id
WHERE sps.state = 'PENDING' AND sps.mode = 'SEMANTIC_PROPOSAL'
ORDER BY sps.created_at ASC
LIMIT ?;

-- name: ClaimPendingSourceProcessing :execrows
UPDATE source_processing_state
SET
    state = 'PROCESSING',
    started_at = sqlc.arg(started_at),
    updated_at = sqlc.arg(updated_at)
WHERE source_revision_id = sqlc.arg(source_revision_id)
  AND state = 'PENDING';

-- name: CompleteSourceProcessingWithProposal :exec
UPDATE source_processing_state
SET
    state = 'COMPLETED',
    proposal_id = sqlc.arg(proposal_id),
    completed_at = sqlc.arg(completed_at),
    updated_at = sqlc.arg(updated_at)
WHERE source_revision_id = sqlc.arg(source_revision_id);

-- name: CompleteSourceProcessingWithRevision :exec
UPDATE source_processing_state
SET
    state = 'COMPLETED',
    revision_id = sqlc.arg(revision_id),
    completed_at = sqlc.arg(completed_at),
    updated_at = sqlc.arg(updated_at)
WHERE source_revision_id = sqlc.arg(source_revision_id);

-- name: FailSourceProcessing :exec
UPDATE source_processing_state
SET
    state = 'FAILED',
    last_error = sqlc.arg(last_error),
    completed_at = sqlc.arg(completed_at),
    updated_at = sqlc.arg(updated_at)
WHERE source_revision_id = sqlc.arg(source_revision_id);

-- name: FailOrphanedSourceProcessing :execrows
UPDATE source_processing_state
SET
    state = 'FAILED',
    last_error = sqlc.arg(last_error),
    completed_at = sqlc.arg(completed_at),
    updated_at = sqlc.arg(updated_at)
WHERE state = 'PROCESSING';

-- name: ReleaseClaimToPendingSourceProcessing :execrows
UPDATE source_processing_state
SET
    state = 'PENDING',
    started_at = NULL,
    updated_at = sqlc.arg(updated_at)
WHERE source_revision_id = sqlc.arg(source_revision_id)
  AND state = 'PROCESSING';
