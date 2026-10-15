package foundation

import (
	"context"
	"errors"
	"testing"
	"time"

	"lum-harbor-notifier/internal/identity"
	"lum-harbor-notifier/internal/testsupport"
)

func newTestRepo(t *testing.T) (*Repository, func(entityType string) string) {
	t.Helper()
	db := testsupport.OpenTestDatabase(t)
	repo := NewRepository(db.DB)
	createEntity := func(entityType string) string {
		t.Helper()
		id, err := identity.New()
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.CreateEntity(context.Background(), id, entityType, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		return id
	}
	return repo, createEntity
}

func TestCreateExternalRefAndLookupByIdentity(t *testing.T) {
	repo, createEntity := newTestRepo(t)
	entityID := createEntity("PLANNER_ITEM")
	refID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	seenAt := time.Date(2027, 1, 15, 9, 0, 0, 0, time.UTC)

	created, err := repo.CreateExternalRef(context.Background(), refID, entityID, "APPLE_REMINDER:instance-1", "reminder-abc", RelationshipAuthoritative, &seenAt)
	if err != nil {
		t.Fatal(err)
	}
	if created.EntityID != entityID || created.RelationshipType != RelationshipAuthoritative {
		t.Fatalf("unexpected created ref: %+v", created)
	}

	got, err := repo.GetExternalRefByIdentity(context.Background(), "APPLE_REMINDER:instance-1", "reminder-abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != refID || got.EntityID != entityID || got.System != "APPLE_REMINDER:instance-1" ||
		got.ExternalID != "reminder-abc" || got.RelationshipType != RelationshipAuthoritative {
		t.Fatalf("unexpected looked-up ref: %+v", got)
	}
	if got.LastSeenAt == nil || !got.LastSeenAt.Equal(seenAt) {
		t.Fatalf("unexpected last_seen_at: %+v", got.LastSeenAt)
	}
}

func TestGetExternalRefByIdentityNotFound(t *testing.T) {
	repo, _ := newTestRepo(t)
	_, err := repo.GetExternalRefByIdentity(context.Background(), "APPLE_REMINDER:instance-1", "does-not-exist")
	if !errors.Is(err, ErrExternalRefNotFound) {
		t.Fatalf("expected ErrExternalRefNotFound, got %v", err)
	}
}

func TestCreateExternalRefRejectsInvalidRelationship(t *testing.T) {
	repo, createEntity := newTestRepo(t)
	entityID := createEntity("PLANNER_ITEM")
	refID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateExternalRef(context.Background(), refID, entityID, "APPLE_REMINDER:instance-1", "reminder-abc", RelationshipType("MADE_UP"), nil); err == nil {
		t.Fatal("expected an error for an invalid relationship type")
	}
}

func TestCreateExternalRefRejectsDuplicateIdentity(t *testing.T) {
	repo, createEntity := newTestRepo(t)
	firstEntity := createEntity("PLANNER_ITEM")
	secondEntity := createEntity("PLANNER_ITEM")

	firstRefID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateExternalRef(context.Background(), firstRefID, firstEntity, "APPLE_REMINDER:instance-1", "reminder-abc", RelationshipAuthoritative, nil); err != nil {
		t.Fatal(err)
	}

	secondRefID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.CreateExternalRef(context.Background(), secondRefID, secondEntity, "APPLE_REMINDER:instance-1", "reminder-abc", RelationshipAuthoritative, nil)
	if !errors.Is(err, ErrExternalRefIdentityConflict) {
		t.Fatalf("expected ErrExternalRefIdentityConflict, got %v", err)
	}

	// The conflicting attempt must not have mutated the existing mapping.
	got, err := repo.GetExternalRefByIdentity(context.Background(), "APPLE_REMINDER:instance-1", "reminder-abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != firstRefID || got.EntityID != firstEntity {
		t.Fatalf("existing external ref was mutated by a failed conflicting create: %+v", got)
	}
}

func TestCreateExternalRefAllowsSameExternalIDInDifferentSystem(t *testing.T) {
	repo, createEntity := newTestRepo(t)
	firstEntity := createEntity("PLANNER_ITEM")
	secondEntity := createEntity("PLANNER_ITEM")

	firstRefID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateExternalRef(context.Background(), firstRefID, firstEntity, "APPLE_REMINDER:instance-1", "shared-native-id", RelationshipAuthoritative, nil); err != nil {
		t.Fatal(err)
	}

	// A different Reminders Instance (a different deterministic system
	// namespace) reusing the same native external ID must not collide with
	// the first Instance's ref. This is the instance-safe namespace
	// guarantee: uniqueness is (system, external_id), and system already
	// encodes the Instance.
	secondRefID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateExternalRef(context.Background(), secondRefID, secondEntity, "APPLE_REMINDER:instance-2", "shared-native-id", RelationshipAuthoritative, nil); err != nil {
		t.Fatalf("expected no conflict across different system namespaces, got %v", err)
	}

	firstGot, err := repo.GetExternalRefByIdentity(context.Background(), "APPLE_REMINDER:instance-1", "shared-native-id")
	if err != nil {
		t.Fatal(err)
	}
	secondGot, err := repo.GetExternalRefByIdentity(context.Background(), "APPLE_REMINDER:instance-2", "shared-native-id")
	if err != nil {
		t.Fatal(err)
	}
	if firstGot.EntityID != firstEntity || secondGot.EntityID != secondEntity {
		t.Fatalf("cross-instance refs resolved to the wrong entities: first=%+v second=%+v", firstGot, secondGot)
	}
}

func TestCreateExternalRefRejectsSecondAuthoritativeForSameEntity(t *testing.T) {
	repo, createEntity := newTestRepo(t)
	entityID := createEntity("PLANNER_ITEM")

	firstRefID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateExternalRef(context.Background(), firstRefID, entityID, "APPLE_REMINDER:instance-1", "reminder-one", RelationshipAuthoritative, nil); err != nil {
		t.Fatal(err)
	}

	secondRefID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.CreateExternalRef(context.Background(), secondRefID, entityID, "APPLE_REMINDER:instance-1", "reminder-two", RelationshipAuthoritative, nil)
	if !errors.Is(err, ErrExternalRefAuthoritativeConflict) {
		t.Fatalf("expected ErrExternalRefAuthoritativeConflict, got %v", err)
	}
}

func TestCreateExternalRefAllowsMirrorAlongsideAuthoritativeForSameEntity(t *testing.T) {
	repo, createEntity := newTestRepo(t)
	entityID := createEntity("PLANNER_ITEM")

	authoritativeRefID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateExternalRef(context.Background(), authoritativeRefID, entityID, "APPLE_REMINDER:instance-1", "reminder-one", RelationshipAuthoritative, nil); err != nil {
		t.Fatal(err)
	}

	mirrorRefID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateExternalRef(context.Background(), mirrorRefID, entityID, "SOME_OTHER_SYSTEM:instance-1", "mirror-id", RelationshipMirror, nil); err != nil {
		t.Fatalf("expected a MIRROR ref to be allowed alongside an AUTHORITATIVE ref for the same entity, got %v", err)
	}
}

func TestCreateExternalRefRejectsUnknownEntity(t *testing.T) {
	repo, _ := newTestRepo(t)
	refID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateExternalRef(context.Background(), refID, "does-not-exist", "APPLE_REMINDER:instance-1", "reminder-abc", RelationshipAuthoritative, nil); err == nil {
		t.Fatal("expected a foreign key error for an unknown entity")
	}
}

// TestCreateExternalRefIsTransactionBound proves the same Repository pattern
// used by CreateEntity/CreateRevision: constructing the Repository over a
// live *sql.Tx lets a future caller (Slice 2's reconciler) create the Entity,
// its Revision, and its ExternalRef in one atomic transaction, and a
// rollback discards all three together.
func TestCreateExternalRefIsTransactionBound(t *testing.T) {
	db := testsupport.OpenTestDatabase(t)
	tx, err := db.DB.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(tx)

	entityID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateEntity(context.Background(), entityID, "PLANNER_ITEM", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	refID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateExternalRef(context.Background(), refID, entityID, "APPLE_REMINDER:instance-1", "reminder-abc", RelationshipAuthoritative, nil); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// After rollback, a fresh process-scoped repository must see neither row.
	freshRepo := NewRepository(db.DB)
	if _, err := freshRepo.GetExternalRefByIdentity(context.Background(), "APPLE_REMINDER:instance-1", "reminder-abc"); !errors.Is(err, ErrExternalRefNotFound) {
		t.Fatalf("expected the rolled-back external ref to be gone, got %v", err)
	}
}

func TestGetExternalRefByExternalIDUnique(t *testing.T) {
	repo, createEntity := newTestRepo(t)
	entityID := createEntity("PLANNER_ITEM")
	refID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.CreateExternalRef(context.Background(), refID, entityID, "APPLE_REMINDER:instance-1", "unique-ext-123", RelationshipAuthoritative, nil); err != nil {
		t.Fatal(err)
	}

	got, err := repo.GetExternalRefByExternalID(context.Background(), "unique-ext-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != refID || got.EntityID != entityID || got.ExternalID != "unique-ext-123" {
		t.Fatalf("unexpected ref: %+v", got)
	}
}

func TestGetExternalRefByExternalIDNotFound(t *testing.T) {
	repo, _ := newTestRepo(t)
	_, err := repo.GetExternalRefByExternalID(context.Background(), "missing-id")
	if !errors.Is(err, ErrExternalRefNotFound) {
		t.Fatalf("expected ErrExternalRefNotFound, got %v", err)
	}
}

func TestGetExternalRefByExternalIDAmbiguous(t *testing.T) {
	repo, createEntity := newTestRepo(t)
	entity1 := createEntity("PLANNER_ITEM")
	entity2 := createEntity("PLANNER_ITEM")
	refID1, _ := identity.New()
	refID2, _ := identity.New()

	if _, err := repo.CreateExternalRef(context.Background(), refID1, entity1, "SYSTEM_A", "shared-ext-id", RelationshipAuthoritative, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateExternalRef(context.Background(), refID2, entity2, "SYSTEM_B", "shared-ext-id", RelationshipAuthoritative, nil); err != nil {
		t.Fatal(err)
	}

	_, err := repo.GetExternalRefByExternalID(context.Background(), "shared-ext-id")
	if !errors.Is(err, ErrAmbiguousExternalRef) {
		t.Fatalf("expected ErrAmbiguousExternalRef, got %v", err)
	}
}

func TestGetExternalRefByExternalIDEmpty(t *testing.T) {
	repo, _ := newTestRepo(t)
	if _, err := repo.GetExternalRefByExternalID(context.Background(), "  "); err == nil {
		t.Fatal("expected error for empty external ID")
	}
}
