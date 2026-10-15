package pluginjob

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"lum-harbor-notifier/internal/plugin"
)

type invokerFunc func(context.Context, plugin.Invocation) (json.RawMessage, error)

func (f invokerFunc) Invoke(ctx context.Context, invocation plugin.Invocation) (json.RawMessage, error) {
	return f(ctx, invocation)
}

type sinkFunc func(context.Context, SourceFetchBatch) error

func (f sinkFunc) Accept(ctx context.Context, batch SourceFetchBatch) error {
	return f(ctx, batch)
}

func TestSourceFetchJobPassesTypedResultAndIdentityToSink(t *testing.T) {
	now := time.Now().UTC()
	fetcher := plugin.NewSourceFetcher(invokerFunc(func(_ context.Context, invocation plugin.Invocation) (json.RawMessage, error) {
		if invocation.ExtensionID != "dummy" || invocation.Instance == nil || invocation.Instance.ID != "team-a" {
			t.Fatalf("unexpected invocation: %+v", invocation)
		}
		result := plugin.SourceFetchResult{Records: []plugin.SourceRecord{{
			ExternalID: "item-1", SourceKind: "dummy.todo", ObservedAt: now, Payload: json.RawMessage("{}"),
		}}}
		return json.Marshal(result)
	}))
	var accepted SourceFetchBatch
	job, err := NewSourceFetchJob(fetcher, sourceInstance("team-a", nil), nil, sinkFunc(func(_ context.Context, batch SourceFetchBatch) error {
		accepted = batch
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if accepted.ExtensionID != "dummy" || accepted.InstanceID != "team-a" || accepted.Result.Records[0].ExternalID != "item-1" {
		t.Fatalf("unexpected batch: %+v", accepted)
	}
}

func TestSourceFetchJobUsesExternalCursorSourceWithoutOwningDurability(t *testing.T) {
	var mu sync.Mutex
	var cursor *string
	var seen []*string
	requestSource := SourceFetchRequestSourceFunc(func(_ context.Context, _ plugin.Instance) (PreparedSourceFetch, error) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, cloneString(cursor))
		return PreparedSourceFetch{Request: plugin.SourceFetchRequest{Cursor: cloneString(cursor)}, FetchContext: "checkpoint-token"}, nil
	})
	fetcher := plugin.NewSourceFetcher(invokerFunc(func(_ context.Context, invocation plugin.Invocation) (json.RawMessage, error) {
		var request plugin.SourceFetchRequest
		if err := json.Unmarshal(invocation.Input, &request); err != nil {
			return nil, err
		}
		next := "cursor-1"
		if request.Cursor != nil {
			next = "cursor-2"
		}
		return json.Marshal(plugin.SourceFetchResult{NextCursor: &next})
	}))
	sink := sinkFunc(func(_ context.Context, batch SourceFetchBatch) error {
		if batch.FetchContext != "checkpoint-token" {
			t.Fatalf("fetch context was not passed unchanged: %#v", batch.FetchContext)
		}
		mu.Lock()
		cursor = cloneString(batch.Result.NextCursor)
		mu.Unlock()
		return nil
	})
	job, err := NewSourceFetchJob(fetcher, sourceInstance("team-a", nil), requestSource, sink)
	if err != nil {
		t.Fatal(err)
	}
	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if seen[0] != nil || seen[1] == nil || *seen[1] != "cursor-1" {
		t.Fatalf("unexpected cursor sequence: %+v", seen)
	}
}

func TestSourceFetchJobReturnsRequestFetchAndSinkErrors(t *testing.T) {
	expected := errors.New("failed")
	fetcher := plugin.NewSourceFetcher(invokerFunc(func(context.Context, plugin.Invocation) (json.RawMessage, error) {
		return nil, expected
	}))
	instance := sourceInstance("team-a", nil)
	job, _ := NewSourceFetchJob(fetcher, instance, nil, sinkFunc(func(context.Context, SourceFetchBatch) error { return nil }))
	if err := job.Run(context.Background()); !errors.Is(err, expected) {
		t.Fatalf("fetch error: %v", err)
	}
	requestJob, _ := NewSourceFetchJob(fetcher, instance, SourceFetchRequestSourceFunc(func(context.Context, plugin.Instance) (PreparedSourceFetch, error) {
		return PreparedSourceFetch{}, expected
	}), sinkFunc(func(context.Context, SourceFetchBatch) error { return nil }))
	if err := requestJob.Run(context.Background()); !errors.Is(err, expected) {
		t.Fatalf("request error: %v", err)
	}
	successFetcher := plugin.NewSourceFetcher(invokerFunc(func(context.Context, plugin.Invocation) (json.RawMessage, error) {
		return json.RawMessage(`{"records":[]}`), nil
	}))
	sinkJob, _ := NewSourceFetchJob(successFetcher, instance, nil, sinkFunc(func(context.Context, SourceFetchBatch) error {
		return expected
	}))
	if err := sinkJob.Run(context.Background()); !errors.Is(err, expected) {
		t.Fatalf("sink error: %v", err)
	}
}

func TestBuildScheduledSourceFetchUsesManifestDefaultOrCallerOverride(t *testing.T) {
	extension := sourceExtension()
	fetcher := plugin.NewSourceFetcher(invokerFunc(func(context.Context, plugin.Invocation) (json.RawMessage, error) {
		return json.RawMessage(`{"records":[]}`), nil
	}))
	sink := sinkFunc(func(context.Context, SourceFetchBatch) error { return nil })
	instanceA := sourceInstance("team-a", nil)
	defaulted, err := BuildScheduledSourceFetch(extension, instanceA, ScheduleOptions{
		BindingID: "poll", Timeout: time.Second, Enabled: true,
	}, fetcher, nil, sink)
	if err != nil {
		t.Fatal(err)
	}
	if defaulted.Definition.Interval != 300*time.Second || defaulted.Definition.ID != "source:dummy:team-a" {
		t.Fatalf("unexpected default definition: %+v", defaulted.Definition)
	}
	override := 25 * time.Millisecond
	instanceB := sourceInstance("team-b", nil)
	overridden, err := BuildScheduledSourceFetch(extension, instanceB, ScheduleOptions{
		BindingID: "poll", IntervalOverride: &override, Timeout: time.Second, Enabled: true,
	}, fetcher, nil, sink)
	if err != nil {
		t.Fatal(err)
	}
	if overridden.Definition.Interval != override || overridden.Definition.ID == defaulted.Definition.ID {
		t.Fatalf("unexpected override definition: %+v", overridden.Definition)
	}
}

func sourceExtension() plugin.Extension {
	return plugin.Extension{Manifest: plugin.Manifest{
		ID: "dummy", Name: "Dummy", Version: "1.0.0",
		ManifestVersion: plugin.SupportedManifestVersion, ProtocolVersion: plugin.SupportedProtocolVersion,
		Capabilities: []plugin.CapabilityDeclaration{{ID: plugin.CapabilitySourceFetch}},
		Bindings: []plugin.BindingDeclaration{{
			ID: "poll", Type: plugin.BindingSchedule, Capability: plugin.CapabilitySourceFetch, DefaultIntervalSeconds: 300,
		}},
		Configuration: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
			{Key: "team", Label: "Team", Type: plugin.ConfigString},
		}},
	}}
}

func sourceInstance(id string, config json.RawMessage) plugin.Instance {
	if config == nil {
		config = json.RawMessage("{}")
	}
	return plugin.Instance{ID: id, ExtensionID: "dummy", CapabilityID: plugin.CapabilitySourceFetch, Config: config}
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
