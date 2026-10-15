package source

import (
	"context"
	"fmt"
	"time"

	"lum-harbor-notifier/internal/history"
)

type revisionObservedSnapshot struct {
	Version         int     `json:"version"`
	ExtensionID     string  `json:"extension_id"`
	InstanceID      string  `json:"instance_id"`
	SourceKind      string  `json:"source_kind"`
	ExternalID      string  `json:"external_id"`
	SourceRecordID  string  `json:"source_record_id"`
	ObservedAt      string  `json:"observed_at"`
	SourceUpdatedAt *string `json:"source_updated_at"`
	ContentHash     string  `json:"content_hash"`
}

func recordRevisionObserved(ctx context.Context, repository *history.Repository, record SourceRecord, revision SourceRevision) error {
	after, err := history.Snapshot(revisionObservedSnapshot{
		Version: 1, ExtensionID: record.ExtensionID, InstanceID: record.InstanceID,
		SourceKind: record.SourceKind, ExternalID: record.ExternalID, SourceRecordID: record.ID,
		ObservedAt:      revision.ObservedAt.UTC().Format(time.RFC3339Nano),
		SourceUpdatedAt: sourceHistoryTime(revision.SourceUpdatedAt), ContentHash: revision.ContentHash,
	})
	if err != nil {
		return err
	}
	sourceRevisionID := revision.ID
	_, err = repository.Append(ctx, history.Draft{
		Type: history.EventSourceRevisionObserved, Scope: history.ScopeSource, Actor: history.ActorSource,
		Summary:          fmt.Sprintf("Observed new %s source revision", record.SourceKind),
		SourceRevisionID: &sourceRevisionID, CorrelationID: history.SourceRevisionCorrelation(revision.ID),
		AfterJSON: after, OccurredAt: revision.ObservedAt,
	})
	return err
}

func sourceHistoryTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339Nano)
	return &formatted
}
