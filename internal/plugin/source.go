package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type SourceFetchRequest struct {
	Cursor  *string           `json:"cursor,omitempty"`
	Context map[string]string `json:"context,omitempty"`
}

type SourceFetchResult struct {
	Records    []SourceRecord `json:"records"`
	NextCursor *string        `json:"nextCursor,omitempty"`
}

type SourceRecord struct {
	ExternalID      string          `json:"externalId"`
	SourceKind      string          `json:"sourceKind"`
	ObservedAt      time.Time       `json:"observedAt"`
	SourceUpdatedAt *time.Time      `json:"sourceUpdatedAt,omitempty"`
	Payload         json.RawMessage `json:"payload"`
}

type SourceFetcher struct {
	invoker Invoker
}

func NewSourceFetcher(invoker Invoker) *SourceFetcher {
	return &SourceFetcher{invoker: invoker}
}

func (f *SourceFetcher) Fetch(ctx context.Context, instance Instance, request SourceFetchRequest) (SourceFetchResult, error) {
	if f == nil || f.invoker == nil {
		return SourceFetchResult{}, fmt.Errorf("source fetch invoker must not be nil")
	}
	if instance.CapabilityID != CapabilitySourceFetch {
		return SourceFetchResult{}, fmt.Errorf("%w: instance %q requires %q", ErrCapabilityUnsupported, instance.ID, CapabilitySourceFetch)
	}
	input, err := json.Marshal(request)
	if err != nil {
		return SourceFetchResult{}, fmt.Errorf("encode source.fetch request: %w", err)
	}
	output, err := f.invoker.Invoke(ctx, Invocation{
		ExtensionID:  instance.ExtensionID,
		CapabilityID: CapabilitySourceFetch,
		Instance:     &instance,
		Input:        input,
	})
	if err != nil {
		return SourceFetchResult{}, err
	}
	var result SourceFetchResult
	if err := json.Unmarshal(output, &result); err != nil {
		return SourceFetchResult{}, fmt.Errorf("decode source.fetch result for instance %q: %w", instance.ID, err)
	}
	if err := result.Validate(); err != nil {
		return SourceFetchResult{}, fmt.Errorf("validate source.fetch result for instance %q: %w", instance.ID, err)
	}
	return cloneSourceFetchResult(result), nil
}

func (r SourceFetchResult) Validate() error {
	for index, record := range r.Records {
		if strings.TrimSpace(record.ExternalID) == "" {
			return fmt.Errorf("record %d externalId must not be empty", index)
		}
		if strings.TrimSpace(record.SourceKind) == "" {
			return fmt.Errorf("record %d sourceKind must not be empty", index)
		}
		if record.ObservedAt.IsZero() {
			return fmt.Errorf("record %d observedAt must not be zero", index)
		}
		if record.SourceUpdatedAt != nil && record.SourceUpdatedAt.IsZero() {
			return fmt.Errorf("record %d sourceUpdatedAt must not be zero", index)
		}
		if len(record.Payload) == 0 || !json.Valid(record.Payload) {
			return fmt.Errorf("record %d payload must be valid JSON", index)
		}
	}
	return nil
}

func cloneSourceFetchResult(result SourceFetchResult) SourceFetchResult {
	cloned := result
	cloned.Records = append([]SourceRecord(nil), result.Records...)
	for index := range cloned.Records {
		cloned.Records[index].Payload = append(json.RawMessage(nil), result.Records[index].Payload...)
		if result.Records[index].SourceUpdatedAt != nil {
			value := *result.Records[index].SourceUpdatedAt
			cloned.Records[index].SourceUpdatedAt = &value
		}
	}
	if result.NextCursor != nil {
		value := *result.NextCursor
		cloned.NextCursor = &value
	}
	return cloned
}
