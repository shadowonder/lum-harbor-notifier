package history

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
	"lum-harbor-notifier/internal/identity"
)

const maxReadLimit = 200

// Repository uses the supplied executor. Mutation owners pass their *sql.Tx,
// making the business mutation and its History events one atomic commit.
type Repository struct{ queries *dbgen.Queries }

func NewRepository(db dbgen.DBTX) *Repository { return &Repository{queries: dbgen.New(db)} }

func (r *Repository) Append(ctx context.Context, draft Draft) (*Event, error) {
	if err := validateDraft(draft); err != nil {
		return nil, err
	}
	id, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate history event ID: %w", err)
	}
	recordedAt := time.Now().UTC()
	event := &Event{
		ID: id, Type: draft.Type, Scope: draft.Scope, Actor: draft.Actor, Summary: strings.TrimSpace(draft.Summary),
		SubjectEntityID: draft.SubjectEntityID, WorkItemID: draft.WorkItemID, RevisionID: draft.RevisionID,
		ProposalID: draft.ProposalID, AIRunID: draft.AIRunID, SourceRevisionID: draft.SourceRevisionID,
		CorrelationID: strings.TrimSpace(draft.CorrelationID), CausationEventID: draft.CausationEventID,
		BeforeJSON: draft.BeforeJSON, AfterJSON: draft.AfterJSON,
		OccurredAt: draft.OccurredAt.UTC(), RecordedAt: recordedAt,
	}
	if err := r.queries.CreateHistoryEvent(ctx, dbgen.CreateHistoryEventParams{
		ID: event.ID, EventType: string(event.Type), Scope: string(event.Scope), ActorType: string(event.Actor), Summary: event.Summary,
		SubjectEntityID: event.SubjectEntityID, WorkItemID: event.WorkItemID, RevisionID: event.RevisionID,
		ProposalID: event.ProposalID, AiRunID: event.AIRunID, SourceRevisionID: event.SourceRevisionID,
		CorrelationID: event.CorrelationID, CausationEventID: event.CausationEventID,
		BeforeJson: event.BeforeJSON, AfterJson: event.AfterJSON,
		OccurredAt: codec.FormatTime(event.OccurredAt), RecordedAt: codec.FormatTime(event.RecordedAt),
	}); err != nil {
		return nil, fmt.Errorf("append history event %s: %w", event.Type, err)
	}
	return event, nil
}

func (r *Repository) GetByID(ctx context.Context, id string) (*Event, error) {
	row, err := r.queries.GetHistoryEventByID(ctx, strings.TrimSpace(id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("get history event: %w", err)
	}
	return eventFromDB(row)
}

func (r *Repository) ListRecent(ctx context.Context, limit int) ([]Event, error) {
	rows, err := r.queries.ListRecentHistoryEvents(ctx, boundedLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list recent history: %w", err)
	}
	return eventsFromDB(rows)
}

func (r *Repository) ListByCorrelation(ctx context.Context, correlationID string, limit int) ([]Event, error) {
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		return nil, fmt.Errorf("%w: correlation ID is required", ErrInvalidEvent)
	}
	rows, err := r.queries.ListHistoryEventsByCorrelation(ctx, dbgen.ListHistoryEventsByCorrelationParams{CorrelationID: correlationID, Limit: boundedLimit(limit)})
	if err != nil {
		return nil, fmt.Errorf("list history correlation: %w", err)
	}
	return eventsFromDB(rows)
}

func (r *Repository) ListByWorkItem(ctx context.Context, workItemID string, limit int) ([]Event, error) {
	workItemID = strings.TrimSpace(workItemID)
	if workItemID == "" {
		return nil, fmt.Errorf("%w: work item ID is required", ErrInvalidEvent)
	}
	rows, err := r.queries.ListHistoryEventsByWorkItem(ctx, dbgen.ListHistoryEventsByWorkItemParams{WorkItemID: &workItemID, Limit: boundedLimit(limit)})
	if err != nil {
		return nil, fmt.Errorf("list work item history: %w", err)
	}
	return eventsFromDB(rows)
}

func (r *Repository) ListBySubject(ctx context.Context, subjectID string, limit int) ([]Event, error) {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return nil, fmt.Errorf("%w: subject entity ID is required", ErrInvalidEvent)
	}
	rows, err := r.queries.ListHistoryEventsBySubject(ctx, dbgen.ListHistoryEventsBySubjectParams{
		SubjectEntityID: &subjectID,
		Limit:           boundedLimit(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list subject history: %w", err)
	}
	return eventsFromDB(rows)
}

func (r *Repository) FindLatestBySubjectAndType(ctx context.Context, subjectID string, eventType EventType) (*Event, error) {
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" || !eventType.Valid() {
		return nil, fmt.Errorf("%w: valid subject and event type are required", ErrInvalidEvent)
	}
	row, err := r.queries.FindLatestHistoryEventBySubjectAndType(ctx, dbgen.FindLatestHistoryEventBySubjectAndTypeParams{SubjectEntityID: &subjectID, EventType: string(eventType)})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find causal history event: %w", err)
	}
	return eventFromDB(row)
}

// FindTerminalByAIRunID resolves the one prospective terminal event for an
// exact durable AI Run. It exists solely for explicit upstream-run causation.
func (r *Repository) FindTerminalByAIRunID(ctx context.Context, runID string) (*Event, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("%w: AI run ID is required", ErrInvalidEvent)
	}
	row, err := r.queries.FindTerminalHistoryEventByAIRunID(ctx, &runID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find AI run terminal history event: %w", err)
	}
	return eventFromDB(row)
}

func (r *Repository) ListByCausationID(ctx context.Context, causationEventID string) ([]Event, error) {
	causationEventID = strings.TrimSpace(causationEventID)
	if causationEventID == "" {
		return nil, nil
	}
	rows, err := r.queries.ListHistoryEventsByCausationID(ctx, &causationEventID)
	if err != nil {
		return nil, fmt.Errorf("list history events by causation: %w", err)
	}
	return eventsFromDB(rows)
}

func (r *Repository) ListBySourceRevisionID(ctx context.Context, sourceRevisionID string) ([]Event, error) {
	sourceRevisionID = strings.TrimSpace(sourceRevisionID)
	if sourceRevisionID == "" {
		return nil, nil
	}
	rows, err := r.queries.ListHistoryEventsBySourceRevisionID(ctx, &sourceRevisionID)
	if err != nil {
		return nil, fmt.Errorf("list history events by source revision: %w", err)
	}
	return eventsFromDB(rows)
}

func (r *Repository) ListByRevisionID(ctx context.Context, revisionID string) ([]Event, error) {
	revisionID = strings.TrimSpace(revisionID)
	if revisionID == "" {
		return nil, nil
	}
	rows, err := r.queries.ListHistoryEventsByRevisionID(ctx, &revisionID)
	if err != nil {
		return nil, fmt.Errorf("list history events by revision: %w", err)
	}
	return eventsFromDB(rows)
}

func boundedLimit(limit int) int64 {
	if limit <= 0 {
		return 50
	}
	if limit > maxReadLimit {
		return maxReadLimit
	}
	return int64(limit)
}

func eventsFromDB(rows []dbgen.HistoryEvent) ([]Event, error) {
	events := make([]Event, 0, len(rows))
	for _, row := range rows {
		event, err := eventFromDB(row)
		if err != nil {
			return nil, err
		}
		events = append(events, *event)
	}
	return events, nil
}

func eventFromDB(row dbgen.HistoryEvent) (*Event, error) {
	occurredAt, err := codec.ParseTime(row.OccurredAt)
	if err != nil {
		return nil, fmt.Errorf("parse history event %s occurred_at: %w", row.ID, err)
	}
	recordedAt, err := codec.ParseTime(row.RecordedAt)
	if err != nil {
		return nil, fmt.Errorf("parse history event %s recorded_at: %w", row.ID, err)
	}
	event := &Event{
		ID: row.ID, Type: EventType(row.EventType), Scope: Scope(row.Scope), Actor: ActorType(row.ActorType), Summary: row.Summary,
		SubjectEntityID: row.SubjectEntityID, WorkItemID: row.WorkItemID, RevisionID: row.RevisionID,
		ProposalID: row.ProposalID, AIRunID: row.AiRunID, SourceRevisionID: row.SourceRevisionID,
		CorrelationID: row.CorrelationID, CausationEventID: row.CausationEventID,
		BeforeJSON: row.BeforeJson, AfterJSON: row.AfterJson, OccurredAt: occurredAt, RecordedAt: recordedAt,
	}
	if !event.Type.Valid() || !event.Scope.Valid() || !event.Actor.Valid() {
		return nil, fmt.Errorf("history event %s has invalid vocabulary", row.ID)
	}
	return event, nil
}
