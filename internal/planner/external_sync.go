package planner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
	"lum-harbor-notifier/internal/foundation"
	"lum-harbor-notifier/internal/identity"
	"lum-harbor-notifier/internal/temporal"
)

// CreateExternalPlannerItemInput contains everything a deterministic external
// sync caller (a future M13 reconciler) supplies to create a Planner Item
// the first time an external object is observed. It has no scheduled_at/
// scheduled_date: those are Lum Harbor-only concepts with no external
// counterpart (e.g. Apple Reminders has no "scheduled" distinct from "due").
//
// System and ExternalID are the identity this create binds as an
// AUTHORITATIVE external_refs row (Slice 1's foundation.Repository); System
// must already be a caller-constructed, instance-safe namespace (e.g.
// "APPLE_REMINDER:<instance-id>") — this package does not construct it.
//
// SourceRevisionID is an optional provenance identifier only: Planner never
// imports Source, decodes a payload, or stores a Source domain object.
type CreateExternalPlannerItemInput struct {
	System     string
	ExternalID string

	Type        ItemType
	Title       string
	Details     *string
	DueAt       *time.Time
	DueDate     *temporal.LocalDate
	Status      Status
	CompletedAt *time.Time

	SourceRevisionID *string
}

// CreateExternalPlannerItem atomically creates one EXTERNAL Planner Item: a
// SYNC Revision, its Entity, the PlannerItem row itself, an AUTHORITATIVE
// external_refs binding (reusing Slice 1's foundation.Repository — no
// external_refs SQL is duplicated here), and one PLANNER_ITEM_CREATED History
// event with actor SOURCE. Any failure rolls back the entire transaction, so
// a duplicate (System, ExternalID) identity leaves no orphan Entity, Revision,
// PlannerItem, or History event.
func (s *Service) CreateExternalPlannerItem(ctx context.Context, input CreateExternalPlannerItemInput) (*PlannerItem, error) {
	if strings.TrimSpace(input.System) == "" || strings.TrimSpace(input.ExternalID) == "" {
		return nil, fmt.Errorf("external planner item requires a non-empty system and external id")
	}
	if !input.Type.Valid() {
		return nil, fmt.Errorf("invalid planner item type %q", input.Type)
	}
	trimmedTitle, err := validateExternalNativeState(input.Title, input.DueAt, input.DueDate, input.Status, input.CompletedAt)
	if err != nil {
		return nil, err
	}

	// Generate every ID before opening the transaction, matching
	// CreateLocalTodo: UUID generation needs no database access.
	plannerID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate planner item ID: %w", err)
	}
	revisionID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate revision ID: %w", err)
	}
	externalRefID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate external ref ID: %w", err)
	}

	now := time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create external planner item transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	foundationRepo := foundation.NewRepository(tx)

	summary := "Create external planner item"
	if _, err := foundationRepo.CreateRevision(ctx, revisionID, foundation.RevisionOriginSync, &summary, now); err != nil {
		return nil, err
	}
	if err := foundationRepo.CreateEntity(ctx, plannerID, "PLANNER_ITEM", now); err != nil {
		return nil, err
	}

	item := &PlannerItem{
		ID:             plannerID,
		Authority:      AuthorityExternal,
		Type:           input.Type,
		Title:          trimmedTitle,
		Details:        input.Details,
		Status:         input.Status,
		DueAt:          input.DueAt,
		DueDate:        input.DueDate,
		ContextStatus:  ContextUnknown,
		LastRevisionID: revisionID,
		CreatedAt:      now,
		UpdatedAt:      now,
		CompletedAt:    input.CompletedAt,
	}

	plannerRepo := NewRepository(tx)
	if err := plannerRepo.Create(ctx, item); err != nil {
		return nil, err
	}

	// Binding external identity last among the writes (before History) means
	// a duplicate (System, ExternalID) conflict — the one failure mode this
	// step can realistically produce — rolls back the Entity/Revision/
	// PlannerItem just created above along with everything else.
	if _, err := foundationRepo.CreateExternalRef(
		ctx, externalRefID, plannerID, input.System, input.ExternalID, foundation.RelationshipAuthoritative, &now,
	); err != nil {
		return nil, err
	}

	if input.SourceRevisionID != nil && strings.TrimSpace(*input.SourceRevisionID) != "" {
		if err := foundationRepo.LinkRevisionSource(ctx, revisionID, *input.SourceRevisionID, "PRIMARY_SOURCE"); err != nil {
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

	if err := recordPlannerExternalCreated(ctx, tx, item, input.SourceRevisionID); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create external planner item transaction: %w", err)
	}

	return item, nil
}

// ExternalPlannerItemNativeState is the complete external-authoritative
// native state a sync refresh asserts. It always fully replaces the current
// native state (never a partial patch), because the authoritative external
// system is the source of truth for all of these fields at once.
type ExternalPlannerItemNativeState struct {
	Title       string
	Details     *string
	DueAt       *time.Time
	DueDate     *temporal.LocalDate
	Status      Status
	CompletedAt *time.Time
}

// SyncExternalPlannerItemInput identifies an already-existing EXTERNAL
// Planner Item (by its Entity ID, which a caller resolves via Slice 1's
// foundation.Repository.GetExternalRefByIdentity before calling this — this
// package does not perform that lookup) and the native state an external
// sync just observed for it.
type SyncExternalPlannerItemInput struct {
	ID               string
	Native           ExternalPlannerItemNativeState
	SourceRevisionID *string
}

// SyncExternalPlannerItemResult reports whether a real mutation happened, so
// a caller can tell a true no-op apart from a real refresh without
// inspecting Revision/History internals.
type SyncExternalPlannerItemResult struct {
	Item    *PlannerItem
	Changed bool
}

// SyncExternalPlannerItem refreshes an existing EXTERNAL Planner Item's
// native fields (title, details, due, status, completed_at) to match what an
// external source currently reports. It never touches scheduled_at/
// scheduled_date, never accepts a LOCAL item, and never completes/reopens
// through CompleteLocalTodo (that command is a user/local action and
// correctly rejects EXTERNAL authority; sync is a native-state refresh, not
// a command).
//
// If the requested state is identical to the current authoritative state,
// this is a true no-op: no Revision, no History event, Changed=false. A real
// change produces exactly one SYNC Revision and one
// PLANNER_ITEM_EXTERNAL_UPDATED History event, no matter how many fields
// changed together.
func (s *Service) SyncExternalPlannerItem(ctx context.Context, input SyncExternalPlannerItemInput) (SyncExternalPlannerItemResult, error) {
	trimmedTitle, err := validateExternalNativeState(
		input.Native.Title, input.Native.DueAt, input.Native.DueDate, input.Native.Status, input.Native.CompletedAt,
	)
	if err != nil {
		return SyncExternalPlannerItemResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SyncExternalPlannerItemResult{}, fmt.Errorf("begin sync external planner item transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	plannerRepo := NewRepository(tx)
	current, err := plannerRepo.GetByID(ctx, input.ID)
	if err != nil {
		return SyncExternalPlannerItemResult{}, err
	}
	if current.Authority != AuthorityExternal {
		return SyncExternalPlannerItemResult{}, ErrNotExternalAuthority
	}

	if externalNativeStateUnchanged(current, trimmedTitle, input.Native.Details, input.Native.DueAt, input.Native.DueDate, input.Native.Status, input.Native.CompletedAt) {
		// No transaction was ever mutated; the deferred Rollback is a no-op
		// cleanup of the read-only transaction, matching CompleteLocalTodo's
		// existing idempotent-branch pattern (return before Commit).
		return SyncExternalPlannerItemResult{Item: current, Changed: false}, nil
	}

	revisionID, err := identity.New()
	if err != nil {
		return SyncExternalPlannerItemResult{}, fmt.Errorf("generate sync revision ID: %w", err)
	}
	now := time.Now().UTC()

	foundationRepo := foundation.NewRepository(tx)
	summary := "Sync external planner item"
	if _, err := foundationRepo.CreateRevision(ctx, revisionID, foundation.RevisionOriginSync, &summary, now); err != nil {
		return SyncExternalPlannerItemResult{}, err
	}

	updated, err := plannerRepo.UpdateExternalNativeState(ctx, UpdateExternalNativeStateParams{
		EntityID: input.ID, Title: trimmedTitle, Details: input.Native.Details,
		DueAt: input.Native.DueAt, DueDate: input.Native.DueDate,
		Status: input.Native.Status, CompletedAt: input.Native.CompletedAt,
		UpdatedAt: now, LastRevisionID: revisionID,
	})
	if err != nil {
		return SyncExternalPlannerItemResult{}, err
	}

	if input.SourceRevisionID != nil && strings.TrimSpace(*input.SourceRevisionID) != "" {
		if err := foundationRepo.LinkRevisionSource(ctx, revisionID, *input.SourceRevisionID, "PRIMARY_SOURCE"); err != nil {
			return SyncExternalPlannerItemResult{}, err
		}
		atStr := codec.FormatTime(now)
		_ = dbgen.New(tx).CompleteSourceProcessingWithRevision(ctx, dbgen.CompleteSourceProcessingWithRevisionParams{
			SourceRevisionID: *input.SourceRevisionID,
			RevisionID:       &revisionID,
			CompletedAt:      &atStr,
			UpdatedAt:        atStr,
		})
	}

	if err := recordPlannerExternalUpdated(ctx, tx, current, updated, input.SourceRevisionID); err != nil {
		return SyncExternalPlannerItemResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return SyncExternalPlannerItemResult{}, fmt.Errorf("commit sync external planner item transaction: %w", err)
	}

	return SyncExternalPlannerItemResult{Item: updated, Changed: true}, nil
}

// externalNativeStateUnchanged compares canonical current native state
// against a validated, trimmed incoming state field by field. All must match
// for this to be a true no-op.
func externalNativeStateUnchanged(
	current *PlannerItem,
	title string, details *string, dueAt *time.Time, dueDate *temporal.LocalDate, status Status, completedAt *time.Time,
) bool {
	return current.Title == title &&
		stringPtrEqual(current.Details, details) &&
		timePtrEqual(current.DueAt, dueAt) &&
		localDatePtrEqual(current.DueDate, dueDate) &&
		current.Status == status &&
		timePtrEqual(current.CompletedAt, completedAt)
}

func stringPtrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func timePtrEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}

func localDatePtrEqual(a, b *temporal.LocalDate) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ReconcileOutcome classifies what ReconcileExternalPlannerItem actually did.
// It is deliberately closed and has no FAILED value: a genuine failure is
// always reported through this method's error return. FAILED is a
// per-record orchestration concept owned by an external caller (M13 Slice
// 2B's internal/plannersync.Reconciler scans many candidates per call), not
// a Planner mutation outcome.
type ReconcileOutcome string

const (
	ReconcileOutcomeCreated   ReconcileOutcome = "CREATED"
	ReconcileOutcomeUpdated   ReconcileOutcome = "UPDATED"
	ReconcileOutcomeUnchanged ReconcileOutcome = "UNCHANGED"
)

// ReconcileExternalPlannerItemInput contains only the facts Planner needs
// to own external identity resolution and the create/update decision for one
// external observation. It deliberately excludes SourceRecord, Source
// current-head, any Plugin/Reminder-specific payload shape, Plugin Instance,
// or Scheduler concepts — a caller (internal/plannersync) must already have
// translated its evidence into these Planner-native terms before calling
// this method.
type ReconcileExternalPlannerItemInput struct {
	System     string
	ExternalID string

	Type   ItemType
	Native ExternalPlannerItemNativeState

	SourceRevisionID *string
}

// ReconcileExternalPlannerItemResult reports the exact outcome plus the
// resulting item, so a caller can maintain its own aggregate counters
// without inspecting Revision/History internals.
type ReconcileExternalPlannerItemResult struct {
	Item    *PlannerItem
	Outcome ReconcileOutcome
}

// ReconcileExternalPlannerItem is Planner's single owned seam for turning one
// external observation into Planner truth: it resolves (System, ExternalID)
// identity via Slice 1's foundation.Repository, then either syncs the
// existing item or creates a new one — reusing the existing
// SyncExternalPlannerItem/CreateExternalPlannerItem transactions verbatim,
// never duplicating their validation, Revision, History, or external_ref
// logic.
//
// If a lookup miss is immediately followed by a losing create race (another
// caller creates the same (System, ExternalID) identity first —
// foundation.ErrExternalRefIdentityConflict), this method recovers by
// re-resolving identity and syncing the now-existing item, exactly as M13
// Slice 2B originally proved inside internal/plannersync.Reconciler. It never
// creates a second Entity for the same external object, and never uses a
// lock, retry queue, or durable reconciliation state.
func (s *Service) ReconcileExternalPlannerItem(ctx context.Context, input ReconcileExternalPlannerItemInput) (ReconcileExternalPlannerItemResult, error) {
	if strings.TrimSpace(input.System) == "" || strings.TrimSpace(input.ExternalID) == "" {
		return ReconcileExternalPlannerItemResult{}, fmt.Errorf("reconcile external planner item requires a non-empty system and external id")
	}

	foundationRepo := foundation.NewRepository(s.db)
	ref, err := foundationRepo.GetExternalRefByIdentity(ctx, input.System, input.ExternalID)
	switch {
	case err == nil:
		return s.syncExternalPlannerItemForReconcile(ctx, ref.EntityID, input)
	case errors.Is(err, foundation.ErrExternalRefNotFound):
		return s.createOrRecoverExternalPlannerItem(ctx, input)
	default:
		return ReconcileExternalPlannerItemResult{}, fmt.Errorf("resolve external identity for %q: %w", input.ExternalID, err)
	}
}

func (s *Service) syncExternalPlannerItemForReconcile(ctx context.Context, entityID string, input ReconcileExternalPlannerItemInput) (ReconcileExternalPlannerItemResult, error) {
	result, err := s.SyncExternalPlannerItem(ctx, SyncExternalPlannerItemInput{
		ID: entityID, Native: input.Native, SourceRevisionID: input.SourceRevisionID,
	})
	if err != nil {
		return ReconcileExternalPlannerItemResult{}, err
	}
	outcome := ReconcileOutcomeUnchanged
	if result.Changed {
		outcome = ReconcileOutcomeUpdated
	}
	return ReconcileExternalPlannerItemResult{Item: result.Item, Outcome: outcome}, nil
}

// createOrRecoverExternalPlannerItem attempts to create a brand-new EXTERNAL
// PlannerItem for an identity just confirmed missing. A duplicate-identity
// conflict from that attempt is treated as ordinary competition rather than
// a permanent failure: another caller may have created the same external
// identity in the window between this method's lookup and its create call.
func (s *Service) createOrRecoverExternalPlannerItem(ctx context.Context, input ReconcileExternalPlannerItemInput) (ReconcileExternalPlannerItemResult, error) {
	if s.beforeExternalCreate != nil {
		s.beforeExternalCreate()
	}

	item, err := s.CreateExternalPlannerItem(ctx, CreateExternalPlannerItemInput{
		System: input.System, ExternalID: input.ExternalID,
		Type: input.Type, Title: input.Native.Title, Details: input.Native.Details,
		DueAt: input.Native.DueAt, DueDate: input.Native.DueDate,
		Status: input.Native.Status, CompletedAt: input.Native.CompletedAt,
		SourceRevisionID: input.SourceRevisionID,
	})
	if err == nil {
		return ReconcileExternalPlannerItemResult{Item: item, Outcome: ReconcileOutcomeCreated}, nil
	}
	if !errors.Is(err, foundation.ErrExternalRefIdentityConflict) {
		return ReconcileExternalPlannerItemResult{}, fmt.Errorf("create external planner item for %q: %w", input.ExternalID, err)
	}

	foundationRepo := foundation.NewRepository(s.db)
	ref, lookupErr := foundationRepo.GetExternalRefByIdentity(ctx, input.System, input.ExternalID)
	if lookupErr != nil {
		return ReconcileExternalPlannerItemResult{}, fmt.Errorf("recover identity after create race for %q: %w", input.ExternalID, lookupErr)
	}
	return s.syncExternalPlannerItemForReconcile(ctx, ref.EntityID, input)
}
