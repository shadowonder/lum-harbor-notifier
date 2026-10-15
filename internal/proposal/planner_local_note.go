package proposal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/identity"
	"lum-harbor-notifier/internal/planner"
)

// OperationTypePlannerItemSetLocalNote proposes replacing the Lum
// Harbor-owned local note of one existing EXTERNAL Planner Item. It is
// deliberately not a general Planner update: title, external notes, due,
// completion, and external identity stay owned by the external system.
const OperationTypePlannerItemSetLocalNote = "planner.item.set_local_note"

// PlannerLocalNoteOperator is the narrow transaction-aware contract the local
// note operation needs. It is separate from PlannerOperator so Planner
// creation keeps its own minimal contract.
type PlannerLocalNoteOperator interface {
	GetByID(ctx context.Context, id string) (*planner.PlannerItem, error)
	SetExternalPlannerItemLocalNoteTx(ctx context.Context, tx *sql.Tx, input planner.SetExternalPlannerItemLocalNoteInput) (*planner.PlannerItem, error)
}

// PlannerItemSetLocalNotePayload is the versioned typed payload. The target
// Planner Item is the Proposal's target_entity_id; PlannerItemID repeats it so
// an aggregate child stays self-describing. ExpectedLocalNote is the stale
// guard: Apply succeeds only while the current note still equals it, so an
// external sync that changes only external fields never staled the change.
type PlannerItemSetLocalNotePayload struct {
	OperationKind     string  `json:"operation_kind"`
	Version           string  `json:"version"`
	PlannerItemID     string  `json:"planner_item_id"`
	PlannerItemTitle  string  `json:"planner_item_title"`
	WorkItemID        *string `json:"work_item_id,omitempty"`
	ExpectedLocalNote *string `json:"expected_local_note,omitempty"`
	LocalNote         *string `json:"local_note,omitempty"`
}

func (p *PlannerItemSetLocalNotePayload) Validate() error {
	if p.OperationKind != OperationTypePlannerItemSetLocalNote {
		return fmt.Errorf("%w: invalid operation kind %q, expected %q", ErrInvalidOperationPayload, p.OperationKind, OperationTypePlannerItemSetLocalNote)
	}
	if p.Version != OperationVersionV1 {
		return fmt.Errorf("%w: unsupported operation version %q", ErrInvalidOperationPayload, p.Version)
	}
	if strings.TrimSpace(p.PlannerItemID) == "" {
		return fmt.Errorf("%w: planner_item_id must not be blank", ErrInvalidOperationPayload)
	}
	if err := planner.ValidateLocalNote(planner.NormalizeLocalNote(p.LocalNote)); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidOperationPayload, err)
	}
	return nil
}

func (p *PlannerItemSetLocalNotePayload) JSON() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(p)
	return string(b), err
}

func ParsePlannerItemSetLocalNotePayload(data []byte) (*PlannerItemSetLocalNotePayload, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var payload PlannerItemSetLocalNotePayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: malformed planner local note payload: %v", ErrInvalidOperationPayload, err)
	}
	if err := payload.Validate(); err != nil {
		return nil, err
	}
	return &payload, nil
}

// ProposePlannerItemSetLocalNoteInput is caller-supplied intent for proposing
// a new local note (nil or blank clears it) on an existing EXTERNAL item.
type ProposePlannerItemSetLocalNoteInput struct {
	PlannerItemID     string
	LocalNote         *string
	ProposalTitle     *string
	Summary           *string
	Reason            *string
	AIRunID           *string
	SourceRevisionIDs []string
	Producer          Producer
}

func (in ProposePlannerItemSetLocalNoteInput) build(ctx context.Context, s *Service, mode buildMode) (*Proposal, Producer, error) {
	return s.buildPlannerItemSetLocalNote(ctx, in, mode)
}

func (in ProposePlannerItemSetLocalNoteInput) propose(ctx context.Context, s *Service) (*Proposal, error) {
	return s.proposeWith(ctx, in)
}

func (s *Service) plannerLocalNoteOperator() (PlannerLocalNoteOperator, bool) {
	operator, ok := s.plannerOperator.(PlannerLocalNoteOperator)
	return operator, ok
}

// buildPlannerItemSetLocalNote resolves the authoritative target and captures
// its current local note as the stale guard. It persists nothing.
func (s *Service) buildPlannerItemSetLocalNote(ctx context.Context, input ProposePlannerItemSetLocalNoteInput, _ buildMode) (*Proposal, Producer, error) {
	operator, ok := s.plannerLocalNoteOperator()
	if !ok {
		return nil, "", fmt.Errorf("%w: Planner local note capability unavailable", ErrInvalidInput)
	}
	itemID := strings.TrimSpace(input.PlannerItemID)
	if itemID == "" {
		return nil, "", fmt.Errorf("%w: planner item ID must not be empty", ErrInvalidInput)
	}
	producer := input.Producer
	if producer == "" {
		producer = ProducerUser
	}
	if !producer.valid() {
		return nil, "", fmt.Errorf("%w: unsupported Proposal producer %q", ErrInvalidInput, input.Producer)
	}
	note := planner.NormalizeLocalNote(input.LocalNote)
	if err := planner.ValidateLocalNote(note); err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}

	current, err := operator.GetByID(ctx, itemID)
	if err != nil {
		return nil, "", fmt.Errorf("%w: planner item %s: %v", ErrTargetUnavailable, itemID, err)
	}
	if current.Authority != planner.AuthorityExternal {
		return nil, "", fmt.Errorf("%w: local notes apply only to externally synced Planner items; %q is Lum Harbor-owned", ErrInvalidInput, current.Title)
	}
	expected := planner.NormalizeLocalNote(current.LocalNote)
	if optionalEqual(expected, note) {
		return nil, "", fmt.Errorf("%w: planner item %q already has this local note", ErrInvalidInput, current.Title)
	}

	payload := PlannerItemSetLocalNotePayload{
		OperationKind:     OperationTypePlannerItemSetLocalNote,
		Version:           OperationVersionV1,
		PlannerItemID:     current.ID,
		PlannerItemTitle:  current.Title,
		WorkItemID:        current.WorkItemID,
		ExpectedLocalNote: expected,
		LocalNote:         note,
	}
	if err := payload.Validate(); err != nil {
		return nil, "", err
	}

	id, err := identity.New()
	if err != nil {
		return nil, "", fmt.Errorf("generate proposal ID: %w", err)
	}
	proposalTitle := fmt.Sprintf("Update local note: %s", current.Title)
	if note == nil {
		proposalTitle = fmt.Sprintf("Clear local note: %s", current.Title)
	}
	if input.ProposalTitle != nil && strings.TrimSpace(*input.ProposalTitle) != "" {
		proposalTitle = strings.TrimSpace(*input.ProposalTitle)
	}
	now := time.Now().UTC()
	return &Proposal{
		ID:                      id,
		TargetEntityID:          ptrString(current.ID),
		OperationType:           OperationTypePlannerItemSetLocalNote,
		Title:                   proposalTitle,
		Summary:                 input.Summary,
		Reason:                  input.Reason,
		Status:                  StatusPending,
		PlannerLocalNotePayload: &payload,
		AIRunID:                 input.AIRunID,
		SourceRevisionIDs:       input.SourceRevisionIDs,
		CreatedAt:               now,
		UpdatedAt:               now,
	}, producer, nil
}

// executePlannerItemSetLocalNoteTx applies the note. A missing item or a
// local note changed since the Proposal was created makes it STALE; Planner
// owns the authority rule and the Revision/History write.
func (s *Service) executePlannerItemSetLocalNoteTx(ctx context.Context, tx *sql.Tx, op *Proposal) (*operationExecution, error) {
	operator, ok := s.plannerLocalNoteOperator()
	if !ok || op.PlannerLocalNotePayload == nil {
		return nil, fmt.Errorf("%w: planner local note operation unavailable", ErrInvalidOperationPayload)
	}
	payload := op.PlannerLocalNotePayload
	updated, err := operator.SetExternalPlannerItemLocalNoteTx(ctx, tx, planner.SetExternalPlannerItemLocalNoteInput{
		ID:                payload.PlannerItemID,
		LocalNote:         payload.LocalNote,
		ExpectedLocalNote: payload.ExpectedLocalNote,
	})
	if errors.Is(err, planner.ErrNotFound) || errors.Is(err, planner.ErrLocalNoteBaseChanged) {
		return staleExecution(nil), nil
	}
	if err != nil {
		return nil, fmt.Errorf("apply planner item local note: %w", err)
	}
	return &operationExecution{
		appliedRevisionID: updated.LastRevisionID,
		results:           []ProposalOperationResult{primaryResult(&updated.ID, updated.LastRevisionID)},
	}, nil
}

func optionalEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
