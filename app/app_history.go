package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/history"
)

func (app *App) QueryHistory(request HistoryQueryRequestDTO) (HistoryEventPageDTO, error) {
	if app.historyService == nil {
		return HistoryEventPageDTO{}, fmt.Errorf("history service is not initialized")
	}
	if request.Days != 7 && request.Days != 30 && request.Days != 90 {
		return HistoryEventPageDTO{}, fmt.Errorf("history range must be 7, 30, or 90 days")
	}
	var scope *history.Scope
	if raw := strings.TrimSpace(request.Scope); raw != "" {
		value := history.Scope(raw)
		if !value.Valid() {
			return HistoryEventPageDTO{}, fmt.Errorf("unsupported history scope %q", raw)
		}
		scope = &value
	}
	now := time.Now().UTC()
	page, err := app.historyService.Query(appContext(app.ctx), history.Query{Since: now.AddDate(0, 0, -request.Days), Scope: scope, Search: request.Search})
	if err != nil {
		return HistoryEventPageDTO{}, fmt.Errorf("load history failed")
	}
	events, err := historyEventsToDTO(page.Events)
	if err != nil {
		return HistoryEventPageDTO{}, fmt.Errorf("load history failed")
	}
	return HistoryEventPageDTO{AsOf: page.AsOf.UTC().Format(time.RFC3339Nano), Events: events, Truncated: page.Truncated}, nil
}

// QueryWorkItemHistory returns the M12 History events linked to one WorkItem
// by authoritative work_item_id, newest first and bounded (Truncated is honest).
func (app *App) QueryWorkItemHistory(workItemID string) (HistoryEventPageDTO, error) {
	if app.historyService == nil {
		return HistoryEventPageDTO{}, fmt.Errorf("history service is not initialized")
	}
	if strings.TrimSpace(workItemID) == "" {
		return HistoryEventPageDTO{}, fmt.Errorf("work item ID is required")
	}
	page, err := app.historyService.QueryWorkItem(appContext(app.ctx), workItemID)
	if err != nil {
		return HistoryEventPageDTO{}, fmt.Errorf("load work item history failed")
	}
	events, err := historyEventsToDTO(page.Events)
	if err != nil {
		return HistoryEventPageDTO{}, fmt.Errorf("load work item history failed")
	}
	return HistoryEventPageDTO{AsOf: page.AsOf.UTC().Format(time.RFC3339Nano), Events: events, Truncated: page.Truncated}, nil
}

func (app *App) ListHistoryTraces() (HistoryTraceListDTO, error) {
	if app.historyService == nil {
		return HistoryTraceListDTO{}, fmt.Errorf("history service is not initialized")
	}
	page, err := app.historyService.ListTraces(appContext(app.ctx))
	if err != nil {
		return HistoryTraceListDTO{}, fmt.Errorf("load history traces failed")
	}
	result := make([]HistoryTraceSummaryDTO, 0, len(page.Traces))
	for _, trace := range page.Traces {
		scopes := make([]string, len(trace.Scopes))
		for i, scope := range trace.Scopes {
			scopes[i] = string(scope)
		}
		result = append(result, HistoryTraceSummaryDTO{CorrelationID: trace.CorrelationID, EventCount: trace.EventCount, FirstOccurredAt: trace.FirstOccurredAt.Format(time.RFC3339Nano), LastOccurredAt: trace.LastOccurredAt.Format(time.RFC3339Nano), RepresentativeSummary: trace.RepresentativeSummary, RepresentativeScope: string(trace.RepresentativeScope), WorkItemID: trace.WorkItemID, Scopes: scopes})
	}
	return HistoryTraceListDTO{Traces: result, Truncated: page.Truncated}, nil
}

func (app *App) GetHistoryTrace(correlationID string) (HistoryTraceDTO, error) {
	if app.historyService == nil {
		return HistoryTraceDTO{}, fmt.Errorf("history service is not initialized")
	}
	trace, err := app.historyService.GetTrace(appContext(app.ctx), correlationID)
	if err != nil {
		return HistoryTraceDTO{}, fmt.Errorf("load history trace failed")
	}
	events, err := historyEventsToDTO(trace.Events)
	if err != nil {
		return HistoryTraceDTO{}, fmt.Errorf("load history trace failed")
	}
	return HistoryTraceDTO{CorrelationID: trace.CorrelationID, Events: events, Truncated: trace.Truncated}, nil
}

func (app *App) GetCausalChain(targetEventID string) (HistoryCausalChainDTO, error) {
	if app.historyService == nil {
		return HistoryCausalChainDTO{}, fmt.Errorf("history service is not initialized")
	}
	chain, err := app.historyService.GetCausalChain(appContext(app.ctx), targetEventID)
	if err != nil {
		return HistoryCausalChainDTO{}, fmt.Errorf("get causal chain: %w", err)
	}
	events, err := historyEventsToDTO(chain.Events)
	if err != nil {
		return HistoryCausalChainDTO{}, fmt.Errorf("map causal chain events: %w", err)
	}
	var targetDTO *HistoryEventDTO
	targetEvs, err := historyEventsToDTO([]history.Event{chain.TargetEvent})
	if err == nil && len(targetEvs) > 0 {
		targetDTO = &targetEvs[0]
	}
	var rootDTO *HistoryEventDTO
	if chain.RootEvent != nil {
		rootEvs, err := historyEventsToDTO([]history.Event{*chain.RootEvent})
		if err == nil && len(rootEvs) > 0 {
			rootDTO = &rootEvs[0]
		}
	}
	return HistoryCausalChainDTO{
		TargetEventID: chain.TargetEvent.ID,
		TargetEvent:   targetDTO,
		RootEvent:     rootDTO,
		Events:        events,
		RootSource:    app.causalRootSource(chain),
	}, nil
}

func (app *App) GetCausalChainBySubject(subjectEntityID string) (HistoryCausalChainDTO, error) {
	if app.historyService == nil {
		return HistoryCausalChainDTO{}, fmt.Errorf("history service is not initialized")
	}
	chain, err := app.historyService.GetCausalChainBySubject(appContext(app.ctx), subjectEntityID)
	if err != nil {
		return HistoryCausalChainDTO{}, fmt.Errorf("get causal chain by subject: %w", err)
	}
	events, err := historyEventsToDTO(chain.Events)
	if err != nil {
		return HistoryCausalChainDTO{}, fmt.Errorf("map causal chain events: %w", err)
	}
	var targetDTO *HistoryEventDTO
	targetEvs, err := historyEventsToDTO([]history.Event{chain.TargetEvent})
	if err == nil && len(targetEvs) > 0 {
		targetDTO = &targetEvs[0]
	}
	var rootDTO *HistoryEventDTO
	if chain.RootEvent != nil {
		rootEvs, err := historyEventsToDTO([]history.Event{*chain.RootEvent})
		if err == nil && len(rootEvs) > 0 {
			rootDTO = &rootEvs[0]
		}
	}
	return HistoryCausalChainDTO{
		TargetEventID: chain.TargetEvent.ID,
		TargetEvent:   targetDTO,
		RootEvent:     rootDTO,
		Events:        events,
		RootSource:    app.causalRootSource(chain),
	}, nil
}

func historyEventsToDTO(events []history.Event) ([]HistoryEventDTO, error) {
	result := make([]HistoryEventDTO, 0, len(events))
	for _, event := range events {
		before, err := historySnapshotObject(event.BeforeJSON)
		if err != nil {
			return nil, err
		}
		after, err := historySnapshotObject(event.AfterJSON)
		if err != nil {
			return nil, err
		}
		result = append(result, HistoryEventDTO{ID: event.ID, EventType: string(event.Type), Scope: string(event.Scope), Actor: string(event.Actor), Summary: event.Summary, OccurredAt: event.OccurredAt.Format(time.RFC3339Nano), RecordedAt: event.RecordedAt.Format(time.RFC3339Nano), CorrelationID: event.CorrelationID, CausationEventID: event.CausationEventID, SubjectEntityID: event.SubjectEntityID, WorkItemID: event.WorkItemID, RevisionID: event.RevisionID, ProposalID: event.ProposalID, AIRunID: event.AIRunID, SourceRevisionID: event.SourceRevisionID, Before: before, After: after})
	}
	return result, nil
}

func historySnapshotObject(raw *string) (map[string]any, error) {
	if raw == nil {
		return nil, nil
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(*raw), &result); err != nil {
		return nil, err
	}
	return result, nil
}

// causalRootSource resolves the earliest Source revision in a causal chain so
// the History UI can show what originally entered Lum Harbor. It is a
// presentation aid: an unresolvable Source leaves it empty.
func (app *App) causalRootSource(chain history.CausalChain) *HistoryCausalSourceDTO {
	if app.sourceService == nil {
		return nil
	}
	ctx := appContext(app.ctx)
	for _, event := range chain.Events {
		if event.SourceRevisionID == nil || strings.TrimSpace(*event.SourceRevisionID) == "" {
			continue
		}
		revision, err := app.sourceService.GetSourceRevision(ctx, *event.SourceRevisionID)
		if err != nil || revision == nil {
			return nil
		}
		record, err := app.sourceService.GetSourceRecord(ctx, revision.SourceRecordID)
		if err != nil || record == nil {
			return nil
		}
		return &HistoryCausalSourceDTO{
			SourceRevisionID: revision.ID,
			ExtensionID:      record.ExtensionID,
			InstanceID:       record.InstanceID,
			SourceKind:       record.SourceKind,
			PayloadJSON:      string(revision.Payload),
		}
	}
	return nil
}
