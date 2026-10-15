package planner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/foundation"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/identity"
)

// MaxLocalNoteLength bounds a Lum Harbor local note. It is plain text context,
// not a document.
const MaxLocalNoteLength = 4000

// ErrLocalNoteBaseChanged indicates the item's current local note no longer
// matches the value a caller (a pending Proposal) captured as its base.
var ErrLocalNoteBaseChanged = errors.New("planner item local note no longer matches the expected base")

// SetExternalPlannerItemLocalNoteInput replaces the Lum Harbor-owned local
// note of one EXTERNAL Planner Item. LocalNote nil (or blank) clears it.
// ExpectedLocalNote is the note the caller based its change on; the write
// succeeds only while the current note still equals it.
type SetExternalPlannerItemLocalNoteInput struct {
	ID                string
	LocalNote         *string
	ExpectedLocalNote *string
}

// NormalizeLocalNote trims a local note and maps blank to nil, so "no note"
// has exactly one representation.
func NormalizeLocalNote(note *string) *string {
	if note == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*note)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// ValidateLocalNote checks a normalized local note's bounds.
func ValidateLocalNote(note *string) error {
	if note != nil && len([]rune(*note)) > MaxLocalNoteLength {
		return fmt.Errorf("planner item local note exceeds %d characters", MaxLocalNoteLength)
	}
	return nil
}

// SetExternalPlannerItemLocalNoteTx changes only the Lum Harbor-owned local
// note of an EXTERNAL Planner Item inside the caller's transaction (Proposal
// Apply). It never touches title, details, due, status, completion, or
// external identity: those stay owned by the external system and refreshed by
// SyncExternalPlannerItem, which in turn never touches local_note.
//
// A real change produces one USER Revision and one
// PLANNER_ITEM_LOCAL_NOTE_CHANGED History event. LOCAL items are rejected:
// their own details are already Lum Harbor-owned.
func (s *Service) SetExternalPlannerItemLocalNoteTx(ctx context.Context, tx *sql.Tx, input SetExternalPlannerItemLocalNoteInput) (*PlannerItem, error) {
	note := NormalizeLocalNote(input.LocalNote)
	if err := ValidateLocalNote(note); err != nil {
		return nil, err
	}

	plannerRepo := NewRepository(tx)
	current, err := plannerRepo.GetByID(ctx, strings.TrimSpace(input.ID))
	if err != nil {
		return nil, err
	}
	if current.Authority != AuthorityExternal {
		return nil, ErrNotExternalAuthority
	}
	if !stringPtrEqual(NormalizeLocalNote(current.LocalNote), NormalizeLocalNote(input.ExpectedLocalNote)) {
		return nil, ErrLocalNoteBaseChanged
	}
	if stringPtrEqual(NormalizeLocalNote(current.LocalNote), note) {
		return current, nil
	}

	revisionID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate local note revision ID: %w", err)
	}
	now := time.Now().UTC()
	summary := "Update planner item local note"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return nil, err
	}
	updated, err := plannerRepo.UpdateLocalNote(ctx, current.ID, note, now, revisionID)
	if err != nil {
		return nil, err
	}
	if err := recordPlannerLocalNoteChanged(ctx, tx, current, updated); err != nil {
		return nil, err
	}
	return updated, nil
}

type plannerLocalNoteSnapshot struct {
	Version   int     `json:"version"`
	LocalNote *string `json:"local_note,omitempty"`
}

func recordPlannerLocalNoteChanged(ctx context.Context, tx *sql.Tx, before, after *PlannerItem) error {
	beforeJSON, err := history.Snapshot(plannerLocalNoteSnapshot{Version: 1, LocalNote: before.LocalNote})
	if err != nil {
		return err
	}
	afterJSON, err := history.Snapshot(plannerLocalNoteSnapshot{Version: 1, LocalNote: after.LocalNote})
	if err != nil {
		return err
	}
	subjectID, revisionID := after.ID, after.LastRevisionID
	_, err = history.NewRepository(tx).Append(ctx, history.Draft{
		Type: history.EventPlannerItemLocalNoteChanged, Scope: history.ScopePlanner, Actor: history.ActorUser,
		Summary: "Planner item local note updated", SubjectEntityID: &subjectID, WorkItemID: after.WorkItemID, RevisionID: &revisionID,
		CorrelationID: history.RevisionCorrelation(revisionID), BeforeJSON: beforeJSON, AfterJSON: afterJSON, OccurredAt: after.UpdatedAt,
	})
	return err
}
