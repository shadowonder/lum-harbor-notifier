-- name: CreateRevision :one
INSERT INTO
    revisions (
        id,
        origin_type,
        summary,
        applied_at
    )
VALUES (?, ?, ?, ?) 
RETURNING revision_no;

-- name: CreateEntity :exec
INSERT INTO
    entities (
        id,
        entity_type,
        created_at,
        archived_at
    )
VALUES
    (?, ?, ?, ?);

-- name: CreateExternalRef :exec
--
-- Binds one external system identity to one Lum Harbor entity.
-- (system, external_id) is globally unique; entity_id may additionally
-- have at most one AUTHORITATIVE row (idx_external_refs_authoritative_entity).
-- Both are real conflict cases the caller must classify, not just retry.
INSERT INTO external_refs (
    id,
    entity_id,
    system,
    external_id,
    relationship_type,
    last_seen_at
)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetExternalRefByIdentity :one
--
-- Stable external identity lookup: one (system, external_id) resolves to at
-- most one row, by the same unique constraint CreateExternalRef relies on.
SELECT
    id,
    entity_id,
    system,
    external_id,
    relationship_type,
    last_seen_at
FROM external_refs
WHERE system = sqlc.arg(system)
  AND external_id = sqlc.arg(external_id);

-- name: GetAuthoritativeExternalRefByEntity :one
--
-- The single AUTHORITATIVE external identity bound to one entity, if any
-- (idx_external_refs_authoritative_entity guarantees at most one).
SELECT
    id,
    entity_id,
    system,
    external_id,
    relationship_type,
    last_seen_at
FROM external_refs
WHERE entity_id = sqlc.arg(entity_id)
  AND relationship_type = 'AUTHORITATIVE';

-- name: GetEntityType :one
--
-- Stable entity-kind lookup for a known, unarchived entity_id (M15 Slice 1
-- readcap's entity.resolve_external capability).
SELECT entity_type
FROM entities
WHERE id = sqlc.arg(id)
  AND archived_at IS NULL;