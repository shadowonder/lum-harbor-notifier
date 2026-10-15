package workitem

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
	"lum-harbor-notifier/internal/foundation"
	"lum-harbor-notifier/internal/identity"
)

// ErrInvalidInput identifies caller input that cannot form a WorkItem.
var ErrInvalidInput = errors.New("invalid work item input")

// ErrInvalidLifecycleTransition identifies a disallowed WorkItem V1 state
// transition.
var ErrInvalidLifecycleTransition = errors.New("invalid work item lifecycle transition")

// CreateLocalWorkItemInput contains the user-authored fields accepted by the
// minimal create use case. Identity and lifecycle remain service-owned.
type CreateLocalWorkItemInput struct {
	Title            string
	Description      *string
	SourceRevisionID *string
}

// UpdateLocalWorkItemDetailsInput contains the complete editable WorkItem V1
// field set. Identity and lifecycle state are never caller-controlled.
type UpdateLocalWorkItemDetailsInput struct {
	ID          string
	Title       string
	Description *string
}

// SetLocalWorkItemLifecycleStateInput contains the identity and requested
// target state. Completion timestamp and Revision identity remain service-owned.
type SetLocalWorkItemLifecycleStateInput struct {
	ID     string
	Target LifecycleState
}

// Service owns WorkItem use cases and mutation transaction boundaries.
type Service struct {
	db         *sql.DB
	repository *Repository
}

func NewService(db *sql.DB, repository *Repository) *Service {
	return &Service{db: db, repository: repository}
}

// CreateLocalWorkItem atomically creates a USER Revision, global Entity, and
// ACTIVE WorkItem. Any persistence failure rolls the entire mutation back.
func (s *Service) CreateLocalWorkItem(ctx context.Context, input CreateLocalWorkItemInput) (*WorkItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create work item transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	item, err := s.CreateLocalWorkItemTx(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create work item transaction: %w", err)
	}
	return item, nil
}

// CreateLocalWorkItemTx executes the WorkItem creation within a caller-provided
// transaction, allowing Proposal Apply to create the WorkItem, Entity, Revision,
// and History atomically with Proposal finalization.
func (s *Service) CreateLocalWorkItemTx(ctx context.Context, tx *sql.Tx, input CreateLocalWorkItemInput) (*WorkItem, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, fmt.Errorf("%w: title must not be empty", ErrInvalidInput)
	}

	workItemID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate work item ID: %w", err)
	}
	revisionID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate work item revision ID: %w", err)
	}
	now := time.Now().UTC()

	foundationRepository := foundation.NewRepository(tx)
	summary := "Create local work item"
	if _, err := foundationRepository.CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return nil, err
	}
	if err := foundationRepository.CreateEntity(ctx, workItemID, "WORK_ITEM", now); err != nil {
		return nil, err
	}

	item := &WorkItem{
		ID:             workItemID,
		Title:          title,
		Description:    normalizeDescription(input.Description),
		Lifecycle:      LifecycleActive,
		LastRevisionID: revisionID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := NewRepository(tx).Create(ctx, item); err != nil {
		return nil, err
	}
	if input.SourceRevisionID != nil && strings.TrimSpace(*input.SourceRevisionID) != "" {
		if err := foundationRepository.LinkRevisionSource(ctx, revisionID, *input.SourceRevisionID, "PRIMARY_SOURCE"); err != nil {
			return nil, err
		}
		atStr := codec.FormatTime(now)
		_ = dbgen.New(tx).CompleteSourceProcessingWithRevision(ctx, dbgen.CompleteSourceProcessingWithRevisionParams{
			SourceRevisionID: *input.SourceRevisionID,
			RevisionID:       &revisionID,
			CompletedAt:      &atStr,
			UpdatedAt:        atStr,
		})
	}
	if err := recordCreatedHistory(ctx, tx, item, input.SourceRevisionID); err != nil {
		return nil, err
	}
	return item, nil
}

// UpdateLocalWorkItemDetails atomically records a USER Revision and updates
// only title/description plus WorkItem mutation bookkeeping.
func (s *Service) UpdateLocalWorkItemDetails(ctx context.Context, input UpdateLocalWorkItemDetailsInput) (*WorkItem, error) {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return nil, fmt.Errorf("%w: ID must not be empty", ErrInvalidInput)
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, fmt.Errorf("%w: title must not be empty", ErrInvalidInput)
	}
	description := normalizeDescription(input.Description)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin update work item transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	before, updated, err := s.updateDetailsInTx(ctx, tx, id, title, description)
	if err != nil {
		return nil, err
	}
	if err := RecordDetailsChangedHistoryTx(ctx, tx, before, updated, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit update work item transaction: %w", err)
	}
	return updated, nil
}

// UpdateLocalWorkItemDetailsTx executes the authoritative WorkItem detail update
// within a caller-provided transaction. It validates and normalizes input, ensures
// target existence and unarchived status, records a USER Revision, and updates details.
func (s *Service) UpdateLocalWorkItemDetailsTx(ctx context.Context, tx *sql.Tx, input UpdateLocalWorkItemDetailsInput) (*WorkItem, error) {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return nil, fmt.Errorf("%w: ID must not be empty", ErrInvalidInput)
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, fmt.Errorf("%w: title must not be empty", ErrInvalidInput)
	}
	description := normalizeDescription(input.Description)

	_, updated, err := s.updateDetailsInTx(ctx, tx, id, title, description)
	return updated, err
}

func (s *Service) updateDetailsInTx(ctx context.Context, tx *sql.Tx, id string, title string, description *string) (*WorkItem, *WorkItem, error) {
	repository := NewRepository(tx)
	before, err := repository.GetByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	revisionID, err := identity.New()
	if err != nil {
		return nil, nil, fmt.Errorf("generate work item update revision ID: %w", err)
	}
	now := time.Now().UTC()
	summary := "Update local work item"
	if _, err := foundation.NewRepository(tx).CreateRevision(
		ctx,
		revisionID,
		foundation.RevisionOriginUser,
		&summary,
		now,
	); err != nil {
		return nil, nil, err
	}

	updated, err := repository.UpdateDetails(ctx, id, title, description, revisionID, now)
	if err != nil {
		return nil, nil, err
	}
	return before, updated, nil
}

// SetLocalWorkItemLifecycleState applies the explicit WorkItem V1 transition
// contract. Same-state requests are authoritative no-ops with no Revision or
// timestamp change.
func (s *Service) SetLocalWorkItemLifecycleState(ctx context.Context, input SetLocalWorkItemLifecycleStateInput) (*WorkItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin work item lifecycle transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	updated, err := s.SetLocalWorkItemLifecycleStateTx(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit work item lifecycle transaction: %w", err)
	}
	return updated, nil
}

// SetLocalWorkItemLifecycleStateTx executes the same explicit WorkItem V1
// transition contract within a caller-provided transaction, so a Proposal
// Apply can perform the reload-validate-mutate sequence atomically with its
// own CAS/History bookkeeping. Same-state requests are authoritative no-ops.
func (s *Service) SetLocalWorkItemLifecycleStateTx(ctx context.Context, tx *sql.Tx, input SetLocalWorkItemLifecycleStateInput) (*WorkItem, error) {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return nil, fmt.Errorf("%w: ID must not be empty", ErrInvalidInput)
	}
	if !input.Target.Valid() {
		return nil, fmt.Errorf("%w: unsupported target lifecycle %q", ErrInvalidInput, input.Target)
	}

	repository := NewRepository(tx)
	current, err := repository.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if current.Lifecycle == input.Target {
		return current, nil
	}
	if !current.Lifecycle.CanTransitionTo(input.Target) {
		return nil, fmt.Errorf(
			"%w: %s to %s",
			ErrInvalidLifecycleTransition,
			current.Lifecycle,
			input.Target,
		)
	}

	revisionID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate work item lifecycle revision ID: %w", err)
	}
	now := time.Now().UTC()
	summary := fmt.Sprintf("Change local work item lifecycle to %s", input.Target)
	if _, err := foundation.NewRepository(tx).CreateRevision(
		ctx,
		revisionID,
		foundation.RevisionOriginUser,
		&summary,
		now,
	); err != nil {
		return nil, err
	}

	var completedAt *time.Time
	if input.Target == LifecycleCompleted {
		completedAt = &now
	}
	updated, err := repository.UpdateLifecycle(
		ctx,
		id,
		input.Target,
		completedAt,
		revisionID,
		now,
	)
	if err != nil {
		return nil, err
	}
	if err := recordLifecycleChangedHistory(ctx, tx, current, updated); err != nil {
		return nil, err
	}
	return updated, nil
}

func normalizeDescription(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func (s *Service) GetByID(ctx context.Context, id string) (*WorkItem, error) {
	return s.repository.GetByID(ctx, id)
}

// GetByIDTx returns one current, unarchived WorkItem within the supplied transaction.
// This is critical when running with MaxOpenConns=1 to avoid connection pool self-blocking.
func (s *Service) GetByIDTx(ctx context.Context, tx *sql.Tx, id string) (*WorkItem, error) {
	return NewRepository(tx).GetByID(ctx, id)
}

func (s *Service) List(ctx context.Context) ([]WorkItem, error) {
	return s.repository.List(ctx)
}

// SearchByTitle returns up to limit current, unarchived WorkItems whose
// title contains query as a literal, case-insensitive substring (M15 Slice 1
// readcap entity.search capability).
func (s *Service) SearchByTitle(ctx context.Context, query string, limit int32) ([]WorkItem, error) {
	return s.repository.SearchByTitle(ctx, query, limit)
}
