package foundation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	dbgen "lum-harbor-notifier/internal/database/generated"

	"lum-harbor-notifier/internal/database/codec"
)

// RelationshipType is the closed external_refs.relationship_type vocabulary.
type RelationshipType string

const (
	// RelationshipAuthoritative means this external system is the source of
	// truth for the Entity's existence and identity-bearing fields. At most
	// one AUTHORITATIVE ref may exist per entity_id
	// (idx_external_refs_authoritative_entity, a partial unique index).
	RelationshipAuthoritative RelationshipType = "AUTHORITATIVE"
	// RelationshipMirror means this external system merely mirrors an
	// already-authoritative Entity. Multiple MIRROR refs per entity are
	// allowed.
	RelationshipMirror RelationshipType = "MIRROR"
)

func (v RelationshipType) Valid() bool {
	return v == RelationshipAuthoritative || v == RelationshipMirror
}

// ExternalRef binds one external system's stable identity to one Lum Harbor
// Entity. It is a Foundation-level concept (identity, not domain content),
// so it lives beside Entity/Revision rather than in a domain package.
type ExternalRef struct {
	ID               string
	EntityID         string
	System           string
	ExternalID       string
	RelationshipType RelationshipType
	LastSeenAt       *time.Time
}

var (
	// ErrExternalRefNotFound is returned when no row matches the requested
	// (system, external_id) identity.
	ErrExternalRefNotFound = errors.New("external ref not found")
	// ErrExternalRefIdentityConflict is returned when (system, external_id)
	// already names a ref (to any entity, possibly a different one). The
	// caller should look the existing ref up rather than retry the create.
	ErrExternalRefIdentityConflict = errors.New("external ref identity already exists")
	// ErrExternalRefAuthoritativeConflict is returned when entity_id already
	// owns an AUTHORITATIVE external ref and a second AUTHORITATIVE ref was
	// requested for it. A MIRROR ref for the same entity is unaffected.
	ErrExternalRefAuthoritativeConflict = errors.New("entity already has an authoritative external ref")
	// ErrAmbiguousExternalRef is returned when an external_id lookup without a
	// system matches multiple refs across different systems.
	ErrAmbiguousExternalRef = errors.New("ambiguous external ref across multiple systems")
)

// CreateExternalRef persists one external identity binding. lastSeenAt may be
// nil (column is nullable); callers that already know the observation instant
// that justified creating this ref should pass it.
//
// It returns ErrExternalRefIdentityConflict if (system, externalID) already
// exists, or ErrExternalRefAuthoritativeConflict if relationship is
// AUTHORITATIVE and entityID already owns one. Both are classified from the
// real SQLite constraint-violation text, not guessed from field values, so a
// third violation type introduced later fails loudly instead of being
// silently misclassified.
func (r *Repository) CreateExternalRef(
	ctx context.Context,
	id, entityID, system, externalID string,
	relationship RelationshipType,
	lastSeenAt *time.Time,
) (ExternalRef, error) {
	if !relationship.Valid() {
		return ExternalRef{}, fmt.Errorf("invalid external ref relationship type %q", relationship)
	}
	if strings.TrimSpace(system) == "" || strings.TrimSpace(externalID) == "" {
		return ExternalRef{}, fmt.Errorf("external ref system and external id must not be empty")
	}
	err := r.queries.CreateExternalRef(ctx, dbgen.CreateExternalRefParams{
		ID: id, EntityID: entityID, System: system, ExternalID: externalID,
		RelationshipType: string(relationship), LastSeenAt: codec.FormatOptionalTime(lastSeenAt),
	})
	if err != nil {
		return ExternalRef{}, classifyExternalRefCreateError(err)
	}
	return ExternalRef{
		ID: id, EntityID: entityID, System: system, ExternalID: externalID,
		RelationshipType: relationship, LastSeenAt: lastSeenAt,
	}, nil
}

// GetExternalRefByIdentity looks a ref up by its unique (system, external_id)
// identity, the same identity CreateExternalRef enforces.
func (r *Repository) GetExternalRefByIdentity(ctx context.Context, system, externalID string) (*ExternalRef, error) {
	row, err := r.queries.GetExternalRefByIdentity(ctx, dbgen.GetExternalRefByIdentityParams{
		System: system, ExternalID: externalID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: system %q external id %q", ErrExternalRefNotFound, system, externalID)
		}
		return nil, fmt.Errorf("get external ref by identity: %w", err)
	}
	return externalRefFromDB(row)
}

// GetAuthoritativeExternalRefByEntity returns the single AUTHORITATIVE
// external identity bound to entityID, or ErrExternalRefNotFound.
func (r *Repository) GetAuthoritativeExternalRefByEntity(ctx context.Context, entityID string) (*ExternalRef, error) {
	row, err := r.queries.GetAuthoritativeExternalRefByEntity(ctx, entityID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: authoritative ref for entity %q", ErrExternalRefNotFound, entityID)
		}
		return nil, fmt.Errorf("get authoritative external ref by entity: %w", err)
	}
	return externalRefFromDB(row)
}

// GetExternalRefByExternalID looks up external refs with the given externalID
// across all systems. If exactly one ref is found, it is returned. If no refs
// are found, it returns ErrExternalRefNotFound. If multiple refs are found across
// different systems, it returns ErrAmbiguousExternalRef.
func (r *Repository) GetExternalRefByExternalID(ctx context.Context, externalID string) (*ExternalRef, error) {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return nil, fmt.Errorf("external id must not be empty")
	}
	if r.db == nil {
		return nil, fmt.Errorf("repository database handle is required for no-system lookup")
	}
	rows, err := r.db.QueryContext(ctx, `
SELECT id, entity_id, system, external_id, relationship_type, last_seen_at
FROM external_refs
WHERE external_id = ?`, externalID)
	if err != nil {
		return nil, fmt.Errorf("query external refs by external id: %w", err)
	}
	defer rows.Close()

	var refs []*ExternalRef
	for rows.Next() {
		var row dbgen.ExternalRef
		if err := rows.Scan(&row.ID, &row.EntityID, &row.System, &row.ExternalID, &row.RelationshipType, &row.LastSeenAt); err != nil {
			return nil, fmt.Errorf("scan external ref: %w", err)
		}
		ref, err := externalRefFromDB(row)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows err: %w", err)
	}

	if len(refs) == 0 {
		return nil, fmt.Errorf("%w: external id %q", ErrExternalRefNotFound, externalID)
	}
	if len(refs) > 1 {
		return nil, fmt.Errorf("%w: external id %q", ErrAmbiguousExternalRef, externalID)
	}
	return refs[0], nil
}

// ErrEntityNotFound is returned when the requested entity_id has no current,
// unarchived row in entities.
var ErrEntityNotFound = errors.New("entity not found")

// GetEntityType returns the stable entity_type of one current, unarchived
// Entity. It exists so a caller that already holds an entity_id (for example
// from GetExternalRefByIdentity) can learn what kind of Entity it names
// without duplicating knowledge of any domain package's own table.
func (r *Repository) GetEntityType(ctx context.Context, entityID string) (string, error) {
	entityType, err := r.queries.GetEntityType(ctx, entityID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("%w: %s", ErrEntityNotFound, entityID)
		}
		return "", fmt.Errorf("get entity type %s: %w", entityID, err)
	}
	return entityType, nil
}

func externalRefFromDB(row dbgen.ExternalRef) (*ExternalRef, error) {
	relationship := RelationshipType(row.RelationshipType)
	if !relationship.Valid() {
		return nil, fmt.Errorf("external ref %s has invalid relationship type %q", row.ID, row.RelationshipType)
	}
	lastSeenAt, err := codec.ParseOptionalTime(row.LastSeenAt)
	if err != nil {
		return nil, fmt.Errorf("parse external ref %s last_seen_at: %w", row.ID, err)
	}
	return &ExternalRef{
		ID: row.ID, EntityID: row.EntityID, System: row.System, ExternalID: row.ExternalID,
		RelationshipType: relationship, LastSeenAt: lastSeenAt,
	}, nil
}

// classifyExternalRefCreateError turns the two possible unique-constraint
// violations on external_refs into distinct sentinel errors. Both surface
// from modernc.org/sqlite as "UNIQUE constraint failed: <column list> (2067)";
// modernc.org/sqlite names the violated column(s), not the index, even for
// idx_external_refs_authoritative_entity's partial unique index — confirmed
// against the real driver in external_ref_test.go, not assumed. Any other
// error (including an unrecognized constraint name) is returned unwrapped so
// a future third constraint cannot be silently swallowed into the wrong case.
func classifyExternalRefCreateError(err error) error {
	message := err.Error()
	if !strings.Contains(message, "UNIQUE constraint failed") {
		return fmt.Errorf("create external ref: %w", err)
	}
	if strings.Contains(message, "external_refs.entity_id") {
		return fmt.Errorf("%w: %w", ErrExternalRefAuthoritativeConflict, err)
	}
	if strings.Contains(message, "external_refs.system") || strings.Contains(message, "external_refs.external_id") {
		return fmt.Errorf("%w: %w", ErrExternalRefIdentityConflict, err)
	}
	return fmt.Errorf("create external ref: unrecognized unique constraint violation: %w", err)
}
