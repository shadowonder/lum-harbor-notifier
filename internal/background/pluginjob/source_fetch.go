// Package pluginjob adapts typed Plugin capabilities to background Jobs
// without teaching Scheduler about manifests/RPC or Plugin Host about timers.
package pluginjob

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"time"

	"lum-harbor-notifier/internal/background"
	"lum-harbor-notifier/internal/plugin"
)

type SourceFetchBatch struct {
	ExtensionID string
	InstanceID  string
	// FetchContext is opaque orchestration state produced before the remote
	// call. SourceFetchJob carries it unchanged and never interprets it.
	FetchContext any
	Result       plugin.SourceFetchResult
}

// SourceFetchSink consumes one validated fetch result together with the exact
// opaque context against which that fetch began.
type SourceFetchSink interface {
	Accept(ctx context.Context, batch SourceFetchBatch) error
}

type PreparedSourceFetch struct {
	Request      plugin.SourceFetchRequest
	FetchContext any
}

// SourceFetchRequestSource prepares per-run plugin input plus opaque Host-side
// context. Only Request is serialized to the plugin; FetchContext is passed
// unchanged to the Sink after the remote call.
type SourceFetchRequestSource interface {
	Prepare(ctx context.Context, instance plugin.Instance) (PreparedSourceFetch, error)
}

type SourceFetchRequestSourceFunc func(context.Context, plugin.Instance) (PreparedSourceFetch, error)

func (f SourceFetchRequestSourceFunc) Prepare(ctx context.Context, instance plugin.Instance) (PreparedSourceFetch, error) {
	return f(ctx, instance)
}

type SourceFetchJob struct {
	fetcher       *plugin.SourceFetcher
	instance      plugin.Instance
	requestSource SourceFetchRequestSource
	sink          SourceFetchSink
}

func NewSourceFetchJob(
	fetcher *plugin.SourceFetcher,
	instance plugin.Instance,
	requestSource SourceFetchRequestSource,
	sink SourceFetchSink,
) (*SourceFetchJob, error) {
	if fetcher == nil {
		return nil, fmt.Errorf("source fetch Job fetcher must not be nil")
	}
	if strings.TrimSpace(instance.ID) == "" || strings.TrimSpace(instance.ExtensionID) == "" {
		return nil, fmt.Errorf("source fetch Job instance and extension IDs must not be empty")
	}
	if instance.CapabilityID != plugin.CapabilitySourceFetch {
		return nil, fmt.Errorf("%w: instance %q requires %q", plugin.ErrCapabilityUnsupported, instance.ID, plugin.CapabilitySourceFetch)
	}
	if isNilInterface(sink) {
		return nil, fmt.Errorf("source fetch Job sink must not be nil")
	}
	if requestSource != nil && isNilInterface(requestSource) {
		return nil, fmt.Errorf("source fetch Job request source must not be typed nil")
	}
	cloned := instance
	cloned.Config = append([]byte(nil), instance.Config...)
	return &SourceFetchJob{
		fetcher: fetcher, instance: cloned, requestSource: requestSource, sink: sink,
	}, nil
}

func (j *SourceFetchJob) Run(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("source fetch Job context must not be nil")
	}
	prepared := PreparedSourceFetch{}
	if j.requestSource != nil {
		var err error
		prepared, err = j.requestSource.Prepare(ctx, cloneInstance(j.instance))
		if err != nil {
			return fmt.Errorf("source fetch request for extension %q instance %q: %w", j.instance.ExtensionID, j.instance.ID, err)
		}
	}
	result, err := j.fetcher.Fetch(ctx, j.instance, prepared.Request)
	if err != nil {
		return fmt.Errorf("source fetch extension %q instance %q: %w", j.instance.ExtensionID, j.instance.ID, err)
	}
	if err := j.sink.Accept(ctx, SourceFetchBatch{
		ExtensionID:  j.instance.ExtensionID,
		InstanceID:   j.instance.ID,
		FetchContext: prepared.FetchContext,
		Result:       result,
	}); err != nil {
		return fmt.Errorf("accept source fetch extension %q instance %q: %w", j.instance.ExtensionID, j.instance.ID, err)
	}
	return nil
}

type ScheduleOptions struct {
	BindingID        string
	IntervalOverride *time.Duration
	Timeout          time.Duration
	Enabled          bool
}

type ScheduledSourceFetch struct {
	Definition background.JobDefinition
	Job        *SourceFetchJob
}

// BuildScheduledSourceFetch converts one source.fetch SCHEDULE declaration
// into a Scheduler registration. Manifest interval is only a default; an
// explicit caller-owned override is authoritative for this registration.
func BuildScheduledSourceFetch(
	extension plugin.Extension,
	instance plugin.Instance,
	options ScheduleOptions,
	fetcher *plugin.SourceFetcher,
	requestSource SourceFetchRequestSource,
	sink SourceFetchSink,
) (ScheduledSourceFetch, error) {
	if instance.ExtensionID != extension.Manifest.ID {
		return ScheduledSourceFetch{}, fmt.Errorf("source fetch instance %q belongs to extension %q, not %q", instance.ID, instance.ExtensionID, extension.Manifest.ID)
	}
	if instance.CapabilityID != plugin.CapabilitySourceFetch {
		return ScheduledSourceFetch{}, fmt.Errorf("%w: instance %q requires %q", plugin.ErrCapabilityUnsupported, instance.ID, plugin.CapabilitySourceFetch)
	}
	if err := extension.Manifest.Configuration.ValidateDocument(instance.Config); err != nil {
		return ScheduledSourceFetch{}, fmt.Errorf("source fetch instance %q configuration invalid: %w", instance.ID, err)
	}
	binding, err := resolveScheduleBinding(extension, options.BindingID)
	if err != nil {
		return ScheduledSourceFetch{}, err
	}
	const maxDurationSeconds = int64((1<<63 - 1) / int64(time.Second))
	if int64(binding.DefaultIntervalSeconds) > maxDurationSeconds {
		return ScheduledSourceFetch{}, fmt.Errorf("source.fetch SCHEDULE binding %q interval exceeds time.Duration", binding.ID)
	}
	interval := time.Duration(binding.DefaultIntervalSeconds) * time.Second
	if options.IntervalOverride != nil {
		interval = *options.IntervalOverride
	}
	job, err := NewSourceFetchJob(fetcher, instance, requestSource, sink)
	if err != nil {
		return ScheduledSourceFetch{}, err
	}
	definition := background.JobDefinition{
		ID:            SourceFetchJobID(instance.ExtensionID, instance.ID),
		Interval:      interval,
		Timeout:       options.Timeout,
		Enabled:       options.Enabled,
		OverlapPolicy: background.OverlapPolicySkip,
	}
	if err := definition.Validate(); err != nil {
		return ScheduledSourceFetch{}, err
	}
	return ScheduledSourceFetch{Definition: definition, Job: job}, nil
}

func resolveScheduleBinding(extension plugin.Extension, bindingID string) (plugin.BindingDeclaration, error) {
	var matches []plugin.BindingDeclaration
	for _, binding := range extension.Manifest.Bindings {
		if binding.Type != plugin.BindingSchedule || binding.Capability != plugin.CapabilitySourceFetch {
			continue
		}
		if bindingID == "" || binding.ID == bindingID {
			matches = append(matches, binding)
		}
	}
	if len(matches) == 0 {
		return plugin.BindingDeclaration{}, fmt.Errorf("extension %q has no source.fetch SCHEDULE binding %q", extension.Manifest.ID, bindingID)
	}
	if len(matches) > 1 {
		return plugin.BindingDeclaration{}, fmt.Errorf("extension %q has multiple source.fetch SCHEDULE bindings; binding ID is required", extension.Manifest.ID)
	}
	return matches[0], nil
}

func SourceFetchJobID(extensionID, instanceID string) string {
	return "source:" + url.QueryEscape(extensionID) + ":" + url.QueryEscape(instanceID)
}

func cloneInstance(instance plugin.Instance) plugin.Instance {
	cloned := instance
	cloned.Config = append([]byte(nil), instance.Config...)
	return cloned
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
