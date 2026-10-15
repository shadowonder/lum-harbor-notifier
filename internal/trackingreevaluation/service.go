package trackingreevaluation

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
	"lum-harbor-notifier/internal/identity"
)

const maxErrorBytes = 4096

type Service struct {
	db  *sql.DB
	now func() time.Time
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db, now: func() time.Time { return time.Now().UTC() }}
}

// RequestForCompletionTx is a Planner completion hook. Its insert shares the
// caller's transaction, and the completion Revision is the idempotency key.
func (s *Service) RequestForCompletionTx(ctx context.Context, tx *sql.Tx, plannerItemID, completionRevisionID string, now time.Time) error {
	if s == nil || tx == nil {
		return fmt.Errorf("tracking reevaluation transaction is not initialized")
	}
	id, err := identity.New()
	if err != nil {
		return fmt.Errorf("generate tracking reevaluation request ID: %w", err)
	}
	formatted := codec.FormatTime(now.UTC())
	_, err = dbgen.New(tx).CreateTrackingReevaluationRequest(ctx, dbgen.CreateTrackingReevaluationRequestParams{
		ID: id, CompletionRevisionID: strings.TrimSpace(completionRevisionID),
		CreatedAt: formatted, UpdatedAt: formatted, PlannerItemID: strings.TrimSpace(plannerItemID),
	})
	if err != nil {
		return fmt.Errorf("create tracking reevaluation request: %w", err)
	}
	return nil
}

func (s *Service) Get(ctx context.Context, id string) (*Request, error) {
	row, err := dbgen.New(s.db).GetTrackingReevaluationRequest(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	return requestFromRow(row)
}

func (s *Service) GetByCompletionRevision(ctx context.Context, revisionID string) (*Request, error) {
	row, err := dbgen.New(s.db).GetTrackingReevaluationRequestByCompletionRevision(ctx, strings.TrimSpace(revisionID))
	if err != nil {
		return nil, err
	}
	return requestFromRow(row)
}

func (s *Service) ListPending(ctx context.Context, limit int) ([]Request, error) {
	if limit <= 0 {
		return []Request{}, nil
	}
	rows, err := dbgen.New(s.db).ListPendingTrackingReevaluationRequests(ctx, int64(limit))
	if err != nil {
		return nil, fmt.Errorf("list pending tracking reevaluations: %w", err)
	}
	result := make([]Request, 0, len(rows))
	for _, row := range rows {
		request, err := requestFromRow(row)
		if err != nil {
			return nil, err
		}
		result = append(result, *request)
	}
	return result, nil
}

func (s *Service) Claim(ctx context.Context, id string) (bool, error) {
	now := codec.FormatTime(s.now().UTC())
	rows, err := dbgen.New(s.db).ClaimTrackingReevaluationRequest(ctx, dbgen.ClaimTrackingReevaluationRequestParams{
		ID: strings.TrimSpace(id), StartedAt: &now, UpdatedAt: now,
	})
	return rows > 0, err
}

func (s *Service) Complete(ctx context.Context, id string, aiRunID, proposalID *string) error {
	now := codec.FormatTime(s.now().UTC())
	rows, err := dbgen.New(s.db).CompleteTrackingReevaluationRequest(ctx, dbgen.CompleteTrackingReevaluationRequestParams{
		ID: strings.TrimSpace(id), CompletedAt: &now, UpdatedAt: now,
		AiRunID: cleanOptional(aiRunID), ProposalID: cleanOptional(proposalID),
	})
	if err != nil {
		return fmt.Errorf("complete tracking reevaluation %s: %w", id, err)
	}
	if rows != 1 {
		return fmt.Errorf("complete tracking reevaluation %s: request is not PROCESSING", id)
	}
	return nil
}

func (s *Service) Fail(ctx context.Context, id string, cause error, aiRunID *string) error {
	now := codec.FormatTime(s.now().UTC())
	message := "tracking reevaluation failed"
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		message = truncateUTF8(cause.Error(), maxErrorBytes)
	}
	rows, err := dbgen.New(s.db).FailTrackingReevaluationRequest(ctx, dbgen.FailTrackingReevaluationRequestParams{
		ID: strings.TrimSpace(id), CompletedAt: &now, UpdatedAt: now,
		LastError: &message, AiRunID: cleanOptional(aiRunID),
	})
	if err != nil {
		return fmt.Errorf("fail tracking reevaluation %s: %w", id, err)
	}
	if rows != 1 {
		return fmt.Errorf("fail tracking reevaluation %s: request is not PROCESSING", id)
	}
	return nil
}

func (s *Service) RecoverInterrupted(ctx context.Context) (int64, error) {
	rows, err := dbgen.New(s.db).RecoverInterruptedTrackingReevaluations(ctx, codec.FormatTime(s.now().UTC()))
	if err != nil {
		return 0, fmt.Errorf("recover interrupted tracking reevaluations: %w", err)
	}
	return rows, nil
}

func (s *Service) RetryFailed(ctx context.Context, id string) (bool, error) {
	rows, err := dbgen.New(s.db).RetryFailedTrackingReevaluationRequest(ctx, dbgen.RetryFailedTrackingReevaluationRequestParams{
		ID: strings.TrimSpace(id), UpdatedAt: codec.FormatTime(s.now().UTC()),
	})
	return rows > 0, err
}

// ListRelevantEvidence returns an ordered, bounded set of exact existing
// SourceRevisions: current heads for the TrackedItem's known external
// identities first, then revisions linked to mutations of the TrackedItem or
// completed Todo. It creates and mutates no Source state.
func (s *Service) ListRelevantEvidence(ctx context.Context, request Request, limit int) ([]EvidenceRevision, error) {
	if limit <= 0 {
		return []EvidenceRevision{}, nil
	}
	result := make([]EvidenceRevision, 0, limit)
	seen := map[string]bool{}
	appendRows := func(rows *sql.Rows, current bool) error {
		defer rows.Close()
		for rows.Next() && len(result) < limit {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			if !seen[id] {
				seen[id] = true
				result = append(result, EvidenceRevision{SourceRevisionID: id, CurrentHead: current})
			}
		}
		return rows.Err()
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT records.current_revision_id
		FROM revision_sources linked
		JOIN history_events h ON h.revision_id = linked.revision_id
		JOIN source_revisions prior ON prior.id = linked.source_revision_id
		JOIN source_records records ON records.id = prior.source_record_id
		WHERE h.subject_entity_id IN (?, ?)
		  AND records.current_revision_id IS NOT NULL
		ORDER BY records.updated_at DESC, records.id DESC
		LIMIT ?`, request.TrackedItemID, request.PlannerItemID, limit)
	if err != nil {
		return nil, fmt.Errorf("list current reevaluation evidence: %w", err)
	}
	if err := appendRows(rows, true); err != nil {
		return nil, err
	}
	if len(result) == limit {
		return result, nil
	}

	rows, err = s.db.QueryContext(ctx, `
		SELECT DISTINCT rs.source_revision_id
		FROM revision_sources rs
		JOIN history_events h ON h.revision_id = rs.revision_id
		JOIN source_revisions sr ON sr.id = rs.source_revision_id
		WHERE h.subject_entity_id IN (?, ?)
		ORDER BY sr.created_at DESC, sr.id DESC
		LIMIT ?`, request.TrackedItemID, request.PlannerItemID, limit)
	if err != nil {
		return nil, fmt.Errorf("list linked reevaluation evidence: %w", err)
	}
	if err := appendRows(rows, false); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) ListRelevantHistory(ctx context.Context, request Request, limit int) ([]HistoryFact, error) {
	if limit <= 0 {
		return []HistoryFact{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT event_type, summary, occurred_at
		FROM history_events
		WHERE subject_entity_id IN (?, ?)
		ORDER BY occurred_at DESC, id DESC
		LIMIT ?`, request.TrackedItemID, request.PlannerItemID, limit)
	if err != nil {
		return nil, fmt.Errorf("list reevaluation history: %w", err)
	}
	defer rows.Close()
	facts := make([]HistoryFact, 0, limit)
	for rows.Next() {
		var fact HistoryFact
		if err := rows.Scan(&fact.EventType, &fact.Summary, &fact.OccurredAt); err != nil {
			return nil, err
		}
		facts = append(facts, fact)
	}
	return facts, rows.Err()
}

func requestFromRow(row dbgen.TrackingReevaluationRequest) (*Request, error) {
	startedAt, err := codec.ParseOptionalTime(row.StartedAt)
	if err != nil {
		return nil, err
	}
	completedAt, err := codec.ParseOptionalTime(row.CompletedAt)
	if err != nil {
		return nil, err
	}
	createdAt, err := codec.ParseTime(row.CreatedAt)
	if err != nil {
		return nil, err
	}
	updatedAt, err := codec.ParseTime(row.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &Request{
		ID: row.ID, PlannerItemID: row.PlannerItemID, CompletionRevisionID: row.CompletionRevisionID,
		TrackedItemID: row.TrackedItemID, Status: Status(row.State), AttemptCount: row.AttemptCount,
		StartedAt: startedAt, CompletedAt: completedAt, LastError: row.LastError,
		ProposalID: row.ProposalID, AIRunID: row.AiRunID, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

func cleanOptional(value *string) *string {
	if value == nil {
		return nil
	}
	clean := strings.TrimSpace(*value)
	if clean == "" {
		return nil
	}
	return &clean
}

func truncateUTF8(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	value = value[:maximum]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
