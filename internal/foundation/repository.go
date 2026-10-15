package foundation

import (
	"context"
	"fmt"
	"strings"
	"time"

	dbgen "lum-harbor-notifier/internal/database/generated"
)

type RevisionOrigin string

const (
	RevisionOriginUser   RevisionOrigin = "USER"
	RevisionOriginAI     RevisionOrigin = "AI"
	RevisionOriginSync   RevisionOrigin = "SYNC"
	RevisionOriginImport RevisionOrigin = "IMPORT"
	RevisionOriginSystem RevisionOrigin = "SYSTEM"
)

type Repository struct {
	db      dbgen.DBTX
	queries *dbgen.Queries
}

func NewRepository(db dbgen.DBTX) *Repository {
	return &Repository{
		db:      db,
		queries: dbgen.New(db),
	}
}

// CreateRevision creates one logical mutation batch.
//
// SQLite generates revision_no. The caller only supplies the stable
// revision UUID and semantic metadata.
//
// The returned revision number is useful for ordering/debugging but is
// not the identity referenced by domain tables.
func (r *Repository) CreateRevision(
	ctx context.Context,
	id string,
	origin RevisionOrigin,
	summary *string,
	appliedAt time.Time,
) (int64, error) {
	revisionNo, err := r.queries.CreateRevision(
		ctx,
		dbgen.CreateRevisionParams{
			ID:         id,
			OriginType: string(origin),
			Summary:    summary,
			AppliedAt:  appliedAt.UTC().Format(time.RFC3339Nano),
		},
	)
	if err != nil {
		return 0, fmt.Errorf("create revision %s: %w", id, err)
	}

	return revisionNo, nil
}

// CreateEntity registers a durable Lum Harbor entity.
//
// This method only creates the global identity record.
// Domain-specific state, such as planner_items, is created by that
// domain's Repository within the same transaction.
func (r *Repository) CreateEntity(
	ctx context.Context,
	id string,
	entityType string,
	createdAt time.Time,
) error {

	err := r.queries.CreateEntity(
		ctx,
		dbgen.CreateEntityParams{
			ID:         id,
			EntityType: entityType,
			CreatedAt:  createdAt.UTC().Format(time.RFC3339Nano),
			ArchivedAt: nil,
		},
	)
	if err != nil {
		return fmt.Errorf("create entity %s: %w", id, err)
	}

	return nil
}

// LinkRevisionSource links a committed domain Revision to an upstream SourceRevision.
func (r *Repository) LinkRevisionSource(
	ctx context.Context,
	revisionID string,
	sourceRevisionID string,
	role string,
) error {
	var rolePtr *string
	if strings.TrimSpace(role) != "" {
		trimmed := strings.TrimSpace(role)
		rolePtr = &trimmed
	}
	if err := r.queries.CreateRevisionSource(ctx, dbgen.CreateRevisionSourceParams{
		RevisionID:       revisionID,
		SourceRevisionID: sourceRevisionID,
		Role:             rolePtr,
	}); err != nil {
		return fmt.Errorf("link revision %s to source revision %s: %w", revisionID, sourceRevisionID, err)
	}
	return nil
}
