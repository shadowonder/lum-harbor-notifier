package history

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	dbgen "lum-harbor-notifier/internal/database/generated"
)

const (
	maxQueryEvents = 200
	maxTraceList   = 100
)

type Query struct {
	Since  time.Time
	Scope  *Scope
	Search string
}

type EventPage struct {
	AsOf      time.Time
	Events    []Event
	Truncated bool
}

type TraceSummary struct {
	CorrelationID         string
	EventCount            int
	FirstOccurredAt       time.Time
	LastOccurredAt        time.Time
	RepresentativeSummary string
	RepresentativeScope   Scope
	WorkItemID            *string
	Scopes                []Scope
}

type TracePage struct {
	Traces    []TraceSummary
	Truncated bool
}

type Trace struct {
	CorrelationID string
	Events        []Event
	Truncated     bool
}

type Service struct {
	repository *Repository
	now        func() time.Time
}

func NewService(repository *Repository) *Service {
	return &Service{repository: repository, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Query(ctx context.Context, query Query) (EventPage, error) {
	if s == nil || s.repository == nil || query.Since.IsZero() {
		return EventPage{}, fmt.Errorf("invalid History query")
	}
	scope := ""
	if query.Scope != nil {
		if !query.Scope.Valid() {
			return EventPage{}, fmt.Errorf("%w: invalid scope", ErrInvalidEvent)
		}
		scope = string(*query.Scope)
	}
	search := strings.ToLower(strings.TrimSpace(query.Search))
	if len(search) > 200 {
		return EventPage{}, fmt.Errorf("%w: search is too long", ErrInvalidEvent)
	}
	rows, err := s.repository.queries.QueryHistoryEvents(ctx, dbgen.QueryHistoryEventsParams{
		Since: query.Since.UTC().Format(time.RFC3339Nano), Scope: scope, Search: search,
		ResultLimit: maxQueryEvents + 1,
	})
	if err != nil {
		return EventPage{}, fmt.Errorf("query History events: %w", err)
	}
	events, err := eventsFromDB(rows)
	if err != nil {
		return EventPage{}, err
	}
	truncated := len(events) > maxQueryEvents
	if truncated {
		events = events[:maxQueryEvents]
	}
	return EventPage{AsOf: s.now(), Events: events, Truncated: truncated}, nil
}

// QueryWorkItem returns the History events whose authoritative work_item_id
// linkage equals workItemID, newest first, bounded like the global Query. It
// applies no date range so terminal WorkItems keep their durable explanation.
func (s *Service) QueryWorkItem(ctx context.Context, workItemID string) (EventPage, error) {
	workItemID = strings.TrimSpace(workItemID)
	if s == nil || s.repository == nil || workItemID == "" {
		return EventPage{}, fmt.Errorf("%w: work item ID is required", ErrInvalidEvent)
	}
	rows, err := s.repository.queries.ListHistoryEventsByWorkItem(ctx, dbgen.ListHistoryEventsByWorkItemParams{WorkItemID: &workItemID, Limit: maxQueryEvents + 1})
	if err != nil {
		return EventPage{}, fmt.Errorf("query WorkItem History events: %w", err)
	}
	events, err := eventsFromDB(rows)
	if err != nil {
		return EventPage{}, err
	}
	truncated := len(events) > maxQueryEvents
	if truncated {
		events = events[:maxQueryEvents]
	}
	return EventPage{AsOf: s.now(), Events: events, Truncated: truncated}, nil
}

// QuerySubject returns the History events whose authoritative subject_entity_id
// linkage equals subjectID, newest first.
func (s *Service) QuerySubject(ctx context.Context, subjectID string) (EventPage, error) {
	subjectID = strings.TrimSpace(subjectID)
	if s == nil || s.repository == nil || subjectID == "" {
		return EventPage{}, fmt.Errorf("%w: subject ID is required", ErrInvalidEvent)
	}
	events, err := s.repository.ListBySubject(ctx, subjectID, maxQueryEvents+1)
	if err != nil {
		return EventPage{}, fmt.Errorf("query subject History events: %w", err)
	}
	truncated := len(events) > maxQueryEvents
	if truncated {
		events = events[:maxQueryEvents]
	}
	return EventPage{AsOf: s.now(), Events: events, Truncated: truncated}, nil
}

// QueryPlannerItem returns every canonical history event related to a Planner Item,
// combining both direct subject events (created, completed, updated) and
// correlated lifecycle events (reminder set/removed, attention opened/resolved, notifications).
func (s *Service) QueryPlannerItem(ctx context.Context, plannerItemID string) (EventPage, error) {
	plannerItemID = strings.TrimSpace(plannerItemID)
	if s == nil || s.repository == nil || plannerItemID == "" {
		return EventPage{}, fmt.Errorf("%w: planner item ID is required", ErrInvalidEvent)
	}
	byCorrelation, err := s.repository.ListByCorrelation(ctx, PlannerCorrelation(plannerItemID), maxQueryEvents+1)
	if err != nil {
		return EventPage{}, fmt.Errorf("query planner item History events by correlation: %w", err)
	}
	bySubject, err := s.repository.ListBySubject(ctx, plannerItemID, maxQueryEvents+1)
	if err != nil {
		return EventPage{}, fmt.Errorf("query planner item History events by subject: %w", err)
	}

	seen := make(map[string]bool)
	allEvents := make([]Event, 0, len(byCorrelation)+len(bySubject))
	for _, ev := range byCorrelation {
		if !seen[ev.ID] {
			seen[ev.ID] = true
			allEvents = append(allEvents, ev)
		}
	}
	for _, ev := range bySubject {
		if !seen[ev.ID] {
			seen[ev.ID] = true
			allEvents = append(allEvents, ev)
		}
	}

	sort.Slice(allEvents, func(i, j int) bool {
		return allEvents[i].OccurredAt.After(allEvents[j].OccurredAt)
	})

	truncated := len(allEvents) > maxQueryEvents
	if truncated {
		allEvents = allEvents[:maxQueryEvents]
	}
	return EventPage{AsOf: s.now(), Events: allEvents, Truncated: truncated}, nil
}

func (s *Service) ListTraces(ctx context.Context) (TracePage, error) {
	rows, err := s.repository.queries.ListHistoryTraceSummaries(ctx, maxTraceList+1)
	if err != nil {
		return TracePage{}, fmt.Errorf("list History traces: %w", err)
	}
	truncated := len(rows) > maxTraceList
	if truncated {
		rows = rows[:maxTraceList]
	}
	result := make([]TraceSummary, 0, len(rows))
	for _, row := range rows {
		first, err := parseTraceInstant(row.FirstOccurredAt)
		if err != nil {
			return TracePage{}, err
		}
		last, err := parseTraceInstant(row.LastOccurredAt)
		if err != nil {
			return TracePage{}, err
		}
		scopes := make([]Scope, 0)
		for _, raw := range strings.Split(row.Scopes, ",") {
			scope := Scope(raw)
			if scope.Valid() {
				scopes = append(scopes, scope)
			}
		}
		sort.Slice(scopes, func(i, j int) bool { return scopes[i] < scopes[j] })
		result = append(result, TraceSummary{CorrelationID: row.CorrelationID, EventCount: int(row.EventCount), FirstOccurredAt: first, LastOccurredAt: last, RepresentativeSummary: row.RepresentativeSummary, RepresentativeScope: Scope(row.RepresentativeScope), WorkItemID: row.WorkItemID, Scopes: scopes})
	}
	return TracePage{Traces: result, Truncated: truncated}, nil
}

func parseTraceInstant(value any) (time.Time, error) {
	raw, ok := value.(string)
	if !ok {
		return time.Time{}, fmt.Errorf("History trace timestamp has unexpected type %T", value)
	}
	return time.Parse(time.RFC3339Nano, raw)
}

func (s *Service) GetTrace(ctx context.Context, correlationID string) (Trace, error) {
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		return Trace{}, fmt.Errorf("%w: correlation ID is required", ErrInvalidEvent)
	}
	count, err := s.repository.queries.CountHistoryEventsByCorrelation(ctx, correlationID)
	if err != nil {
		return Trace{}, fmt.Errorf("count History trace: %w", err)
	}
	if count < 2 {
		return Trace{}, fmt.Errorf("%w: correlation is not an available trace", ErrNotFound)
	}
	events, err := s.repository.ListByCorrelation(ctx, correlationID, maxReadLimit)
	if err != nil {
		return Trace{}, err
	}
	return Trace{CorrelationID: correlationID, Events: events, Truncated: int64(len(events)) < count}, nil
}

// GetCausalChain traverses both upstream causation (why did this event happen?)
// and downstream consequence (what did this event cause?), returning the complete
// causal chain in chronological order along with the identified root cause.
//
// A Revision is one atomic domain change, so History facts recording the same
// revision_id belong to the same step. Causation is an immediate foreign key,
// which means a domain event written before the PROPOSAL_APPLIED of its own
// transaction cannot name it as cause; the same-Revision link carries the
// chain across that step in both directions.
func (s *Service) GetCausalChain(ctx context.Context, targetEventID string) (CausalChain, error) {
	targetEventID = strings.TrimSpace(targetEventID)
	if s == nil || s.repository == nil || targetEventID == "" {
		return CausalChain{}, fmt.Errorf("%w: target event ID is required", ErrInvalidEvent)
	}

	target, err := s.repository.GetByID(ctx, targetEventID)
	if err != nil {
		return CausalChain{}, fmt.Errorf("load target history event %s: %w", targetEventID, err)
	}

	visited := make(map[string]bool)
	visited[target.ID] = true
	chainEvents := []Event{*target}

	addRevisionSiblings := func(event Event) []Event {
		if event.RevisionID == nil {
			return nil
		}
		siblings, err := s.repository.ListByRevisionID(ctx, *event.RevisionID)
		if err != nil {
			return nil
		}
		var added []Event
		for _, sibling := range siblings {
			if !visited[sibling.ID] {
				visited[sibling.ID] = true
				chainEvents = append(chainEvents, sibling)
				added = append(added, sibling)
			}
		}
		return added
	}

	// 1. Traverse upstream causation (why did this happen?)
	curr := target
	for len(visited) < maxReadLimit {
		if curr.CausationEventID != nil && strings.TrimSpace(*curr.CausationEventID) != "" {
			parentID := strings.TrimSpace(*curr.CausationEventID)
			if visited[parentID] {
				break
			}
			parent, err := s.repository.GetByID(ctx, parentID)
			if err != nil {
				break
			}
			visited[parent.ID] = true
			chainEvents = append(chainEvents, *parent)
			curr = parent
			continue
		}
		// No recorded cause: continue from a same-Revision fact that has one.
		var bridge *Event
		for _, sibling := range addRevisionSiblings(*curr) {
			if sibling.CausationEventID != nil && strings.TrimSpace(*sibling.CausationEventID) != "" && !visited[strings.TrimSpace(*sibling.CausationEventID)] {
				bridged := sibling
				bridge = &bridged
				break
			}
		}
		if bridge == nil {
			break
		}
		curr = bridge
	}

	// If the earliest event or target has a SourceRevisionID, connect the SOURCE_REVISION_OBSERVED event
	if curr.SourceRevisionID != nil && strings.TrimSpace(*curr.SourceRevisionID) != "" {
		srcID := strings.TrimSpace(*curr.SourceRevisionID)
		srcEvents, err := s.repository.ListBySourceRevisionID(ctx, srcID)
		if err == nil {
			for _, ev := range srcEvents {
				if !visited[ev.ID] {
					visited[ev.ID] = true
					chainEvents = append(chainEvents, ev)
				}
			}
		}
	}

	// 2. Traverse downstream consequences (what did this cause?), including
	// the other facts of each consequence's own Revision.
	queue := []Event{*target}
	for _, sibling := range addRevisionSiblings(*target) {
		queue = append(queue, sibling)
	}
	for len(queue) > 0 && len(visited) < maxReadLimit {
		next := queue[0]
		queue = queue[1:]
		children, err := s.repository.ListByCausationID(ctx, next.ID)
		if err != nil {
			continue
		}
		for _, child := range children {
			if !visited[child.ID] {
				visited[child.ID] = true
				chainEvents = append(chainEvents, child)
				queue = append(queue, child)
				queue = append(queue, addRevisionSiblings(child)...)
			}
		}
	}

	// Sort chronologically
	sort.Slice(chainEvents, func(i, j int) bool {
		if chainEvents[i].OccurredAt.Equal(chainEvents[j].OccurredAt) {
			return chainEvents[i].ID < chainEvents[j].ID
		}
		return chainEvents[i].OccurredAt.Before(chainEvents[j].OccurredAt)
	})

	var rootEvent *Event
	if len(chainEvents) > 0 {
		rootEvent = &chainEvents[0]
	}

	return CausalChain{
		TargetEvent: *target,
		RootEvent:   rootEvent,
		Events:      chainEvents,
	}, nil
}

// GetCausalChainBySubject finds the latest or primary event for an entity and resolves its causal chain.
func (s *Service) GetCausalChainBySubject(ctx context.Context, subjectEntityID string) (CausalChain, error) {
	subjectEntityID = strings.TrimSpace(subjectEntityID)
	if s == nil || s.repository == nil || subjectEntityID == "" {
		return CausalChain{}, fmt.Errorf("%w: subject entity ID is required", ErrInvalidEvent)
	}

	events, err := s.repository.ListBySubject(ctx, subjectEntityID, 1)
	if err != nil {
		return CausalChain{}, fmt.Errorf("find event for subject %s: %w", subjectEntityID, err)
	}
	if len(events) == 0 {
		return CausalChain{}, fmt.Errorf("%w: no history event found for subject %s", ErrNotFound, subjectEntityID)
	}
	return s.GetCausalChain(ctx, events[0].ID)
}
