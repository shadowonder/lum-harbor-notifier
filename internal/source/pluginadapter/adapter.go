// Package pluginadapter connects typed source.fetch requests/results to the
// durable Source Layer without making Source core depend on Scheduler or RPC.
package pluginadapter

import (
	"context"
	"fmt"
	"time"

	"lum-harbor-notifier/internal/background/pluginjob"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/source"
)

type Adapter struct {
	service *source.Service
}

func New(service *source.Service) (*Adapter, error) {
	if service == nil {
		return nil, fmt.Errorf("source plugin adapter service must not be nil")
	}
	return &Adapter{service: service}, nil
}

func (a *Adapter) Prepare(ctx context.Context, instance plugin.Instance) (pluginjob.PreparedSourceFetch, error) {
	checkpoint, err := a.service.PrepareFetch(ctx, instance.ExtensionID, instance.ID, string(instance.CapabilityID))
	if err != nil {
		return pluginjob.PreparedSourceFetch{}, fmt.Errorf("prepare durable source checkpoint: %w", err)
	}
	return pluginjob.PreparedSourceFetch{
		Request:      plugin.SourceFetchRequest{Cursor: cloneString(checkpoint.Cursor)},
		FetchContext: checkpoint,
	}, nil
}

func (a *Adapter) Accept(ctx context.Context, batch pluginjob.SourceFetchBatch) error {
	checkpoint, ok := batch.FetchContext.(source.FetchCheckpoint)
	if !ok {
		return fmt.Errorf("source.fetch result for extension %q instance %q has invalid checkpoint context", batch.ExtensionID, batch.InstanceID)
	}
	input := source.FetchBatch{
		ExtensionID:  batch.ExtensionID,
		InstanceID:   batch.InstanceID,
		CapabilityID: string(plugin.CapabilitySourceFetch),
		Checkpoint:   checkpoint,
		NextCursor:   cloneString(batch.Result.NextCursor),
		Records:      make([]source.Observation, 0, len(batch.Result.Records)),
	}
	for _, record := range batch.Result.Records {
		input.Records = append(input.Records, source.Observation{
			ExternalID: record.ExternalID, SourceKind: record.SourceKind,
			ObservedAt: record.ObservedAt, SourceUpdatedAt: cloneTime(record.SourceUpdatedAt),
			Payload: append([]byte(nil), record.Payload...),
		})
	}
	if _, err := a.service.IngestFetchResult(ctx, input); err != nil {
		return fmt.Errorf("persist source.fetch result for extension %q instance %q: %w", batch.ExtensionID, batch.InstanceID, err)
	}
	return nil
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

var _ pluginjob.SourceFetchRequestSource = (*Adapter)(nil)
var _ pluginjob.SourceFetchSink = (*Adapter)(nil)
