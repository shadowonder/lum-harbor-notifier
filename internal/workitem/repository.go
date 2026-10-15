package workitem

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
)

// ErrNotFound indicates that no current, unarchived WorkItem has the ID.
var ErrNotFound = errors.New("work item not found")

// Repository persists WorkItem domain state through its supplied executor.
// It may be bound to *sql.DB for reads or *sql.Tx inside a service mutation.
type Repository struct {
	queries *dbgen.Queries
}

func NewRepository(db dbgen.DBTX) *Repository {
	return &Repository{queries: dbgen.New(db)}
}

// Create persists a WorkItem without owning a transaction or Revision.
func (r *Repository) Create(ctx context.Context, item *WorkItem) error {
	if err := r.queries.CreateWorkItem(ctx, dbgen.CreateWorkItemParams{
		EntityID:       item.ID,
		Title:          item.Title,
		Description:    item.Description,
		LifecycleState: string(item.Lifecycle),
		CompletedAt:    codec.FormatOptionalTime(item.CompletedAt),
		LastRevisionID: item.LastRevisionID,
		CreatedAt:      codec.FormatTime(item.CreatedAt),
		UpdatedAt:      codec.FormatTime(item.UpdatedAt),
	}); err != nil {
		return fmt.Errorf("create work item %s: %w", item.ID, err)
	}
	return nil
}

// UpdateDetails persists only the editable WorkItem fields plus mutation
// bookkeeping supplied by the Service-owned transaction.
func (r *Repository) UpdateDetails(
	ctx context.Context,
	id string,
	title string,
	description *string,
	revisionID string,
	updatedAt time.Time,
) (*WorkItem, error) {
	row, err := r.queries.UpdateWorkItemDetails(ctx, dbgen.UpdateWorkItemDetailsParams{
		Title:          title,
		Description:    description,
		LastRevisionID: revisionID,
		UpdatedAt:      codec.FormatTime(updatedAt),
		EntityID:       id,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("update work item %s details: %w", id, err)
	}
	item, err := workItemFromDB(row)
	if err != nil {
		return nil, fmt.Errorf("convert updated work item %s: %w", id, err)
	}
	return item, nil
}

// UpdateLifecycle persists only lifecycle state and its mutation bookkeeping.
// Transition validation and transaction ownership remain with Service.
func (r *Repository) UpdateLifecycle(
	ctx context.Context,
	id string,
	lifecycle LifecycleState,
	completedAt *time.Time,
	revisionID string,
	updatedAt time.Time,
) (*WorkItem, error) {
	row, err := r.queries.UpdateWorkItemLifecycle(ctx, dbgen.UpdateWorkItemLifecycleParams{
		LifecycleState: string(lifecycle),
		CompletedAt:    codec.FormatOptionalTime(completedAt),
		LastRevisionID: revisionID,
		UpdatedAt:      codec.FormatTime(updatedAt),
		EntityID:       id,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("update work item %s lifecycle: %w", id, err)
	}
	item, err := workItemFromDB(row)
	if err != nil {
		return nil, fmt.Errorf("convert lifecycle-updated work item %s: %w", id, err)
	}
	return item, nil
}

// GetByID returns one current, unarchived WorkItem.
func (r *Repository) GetByID(ctx context.Context, id string) (*WorkItem, error) {
	row, err := r.queries.GetWorkItemByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("load work item %s: %w", id, err)
	}
	item, err := workItemFromDB(row)
	if err != nil {
		return nil, fmt.Errorf("convert work item %s: %w", id, err)
	}
	return item, nil
}

// List returns every current, unarchived WorkItem in deterministic creation
// order, with entity ID as a stable tie-breaker.
func (r *Repository) List(ctx context.Context) ([]WorkItem, error) {
	rows, err := r.queries.ListWorkItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("list work items: %w", err)
	}
	items := make([]WorkItem, 0, len(rows))
	for _, row := range rows {
		item, err := workItemFromDB(row)
		if err != nil {
			return nil, fmt.Errorf("convert work item %s: %w", row.EntityID, err)
		}
		items = append(items, *item)
	}
	return items, nil
}

// SearchByTitle returns up to limit current, unarchived WorkItems whose
// title contains query as a literal, case-insensitive substring, ordered by
// title then entity ID for a deterministic result (M15 Slice 1 readcap
// entity.search capability).
func (r *Repository) SearchByTitle(ctx context.Context, query string, limit int32) ([]WorkItem, error) {
	rows, err := r.queries.SearchWorkItemsByTitle(ctx, dbgen.SearchWorkItemsByTitleParams{
		Query: query, RowLimit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search work items by title: %w", err)
	}
	items := make([]WorkItem, 0, len(rows))
	for _, row := range rows {
		item, err := workItemFromDB(row)
		if err != nil {
			return nil, fmt.Errorf("convert work item %s: %w", row.EntityID, err)
		}
		items = append(items, *item)
	}
	return items, nil
}

func workItemFromDB(row dbgen.WorkItem) (*WorkItem, error) {
	lifecycle := LifecycleState(row.LifecycleState)
	if !lifecycle.Valid() {
		return nil, fmt.Errorf("invalid work item lifecycle %q", row.LifecycleState)
	}
	createdAt, err := codec.ParseTime(row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	updatedAt, err := codec.ParseTime(row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	completedAt, err := codec.ParseOptionalTime(row.CompletedAt)
	if err != nil {
		return nil, fmt.Errorf("parse completed_at: %w", err)
	}
	return &WorkItem{
		ID:             row.EntityID,
		Title:          row.Title,
		Description:    row.Description,
		Lifecycle:      lifecycle,
		CompletedAt:    completedAt,
		LastRevisionID: row.LastRevisionID,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}, nil
}
