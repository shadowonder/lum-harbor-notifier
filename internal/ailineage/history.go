package ailineage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"lum-harbor-notifier/internal/history"
)

type terminalHistorySnapshot struct {
	Version              int     `json:"version"`
	Action               string  `json:"action"`
	Status               string  `json:"status"`
	ProviderInvoked      *bool   `json:"provider_invoked"`
	ActualProviderID     *string `json:"actual_provider_id,omitempty"`
	ActualProviderSource *string `json:"actual_provider_source,omitempty"`
	ResponseOriginType   *string `json:"response_origin_type,omitempty"`
	ResponseOriginID     *string `json:"response_origin_id,omitempty"`
	UpstreamRunID        *string `json:"upstream_run_id,omitempty"`
}

func recordTerminalHistory(ctx context.Context, tx *sql.Tx, run Run, sourceRevisionID string) error {
	if run.CompletedAt == nil || (run.Status != StatusSucceeded && run.Status != StatusFailed) {
		return fmt.Errorf("invalid terminal AI run history input")
	}
	snapshot := terminalHistorySnapshot{
		Version: 1, Action: string(run.Action), Status: string(run.Status), UpstreamRunID: run.UpstreamRunID,
	}
	actor := history.ActorSystem
	if run.Status == StatusSucceeded {
		actor = history.ActorAI
	}
	if run.Provenance != nil {
		invoked := run.Provenance.ProviderInvoked
		snapshot.ProviderInvoked = &invoked
		originType, originID := string(run.Provenance.Origin.Kind), run.Provenance.Origin.ID
		snapshot.ResponseOriginType, snapshot.ResponseOriginID = &originType, &originID
		if run.Provenance.ActualProvider != nil {
			providerID, providerSource := run.Provenance.ActualProvider.ID, string(run.Provenance.ActualProvider.Source)
			snapshot.ActualProviderID, snapshot.ActualProviderSource = &providerID, &providerSource
		}
		if run.Status == StatusFailed {
			actor = history.ActorAI
		}
	}
	after, err := history.Snapshot(snapshot)
	if err != nil {
		return err
	}
	eventType, summary := history.EventAIRunSucceeded, fmt.Sprintf("AI run completed for %s", run.Action)
	if run.Status == StatusFailed {
		eventType, summary = history.EventAIRunFailed, fmt.Sprintf("AI run failed for %s", run.Action)
	}
	historyRepository := history.NewRepository(tx)
	var causeID *string
	if run.UpstreamRunID != nil {
		cause, err := historyRepository.FindTerminalByAIRunID(ctx, *run.UpstreamRunID)
		if err != nil {
			return err
		}
		correlation := history.ProcessingCorrelation(run.ProcessingID)
		if cause != nil && cause.CorrelationID == correlation && cause.SourceRevisionID != nil && *cause.SourceRevisionID == sourceRevisionID {
			causeID = &cause.ID
		}
	}
	var sourceIDPtr *string
	if strings.TrimSpace(sourceRevisionID) != "" {
		s := strings.TrimSpace(sourceRevisionID)
		sourceIDPtr = &s
	}
	aiRunID := run.ID
	_, err = historyRepository.Append(ctx, history.Draft{
		Type: eventType, Scope: history.ScopeAI, Actor: actor, Summary: summary,
		AIRunID: &aiRunID, SourceRevisionID: sourceIDPtr,
		CorrelationID: history.ProcessingCorrelation(run.ProcessingID), CausationEventID: causeID,
		AfterJSON: after, OccurredAt: *run.CompletedAt,
	})
	return err
}
