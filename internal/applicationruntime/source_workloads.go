package applicationruntime

import (
	"fmt"
	"time"

	"lum-harbor-notifier/internal/background"
	"lum-harbor-notifier/internal/background/pluginjob"
	"lum-harbor-notifier/internal/plugin"
)

// RegisterSourceWorkloads derives and statically registers one durable source
// workload for every selected source.fetch Instance. Manifest SCHEDULE
// bindings provide the production interval; the caller owns the execution
// timeout and lifecycle dependencies.
func RegisterSourceWorkloads(
	scheduler *background.Scheduler,
	registry *plugin.Registry,
	instances []plugin.Instance,
	fetcher *plugin.SourceFetcher,
	requestSource pluginjob.SourceFetchRequestSource,
	sink pluginjob.SourceFetchSink,
	timeout time.Duration,
) ([]background.JobDefinition, error) {
	sourceInstances := make([]plugin.Instance, 0, len(instances))
	for _, instance := range instances {
		if instance.CapabilityID == plugin.CapabilitySourceFetch {
			sourceInstances = append(sourceInstances, instance)
		}
	}
	if len(sourceInstances) == 0 {
		return nil, nil
	}
	if scheduler == nil {
		return nil, fmt.Errorf("register production source workloads: scheduler must not be nil")
	}
	if registry == nil {
		return nil, fmt.Errorf("register production source workloads: plugin registry must not be nil")
	}
	if requestSource == nil {
		return nil, fmt.Errorf("register production source workloads: durable request source must not be nil")
	}

	registrations := make([]pluginjob.ScheduledSourceFetch, 0, len(sourceInstances))
	definitions := make([]background.JobDefinition, 0, len(sourceInstances))
	seenJobIDs := make(map[string]struct{}, len(sourceInstances))
	for _, instance := range sourceInstances {
		extension, err := registry.Find(instance.ExtensionID)
		if err != nil {
			return nil, fmt.Errorf("compose source workload for instance %q: %w", instance.ID, err)
		}
		registration, err := pluginjob.BuildScheduledSourceFetch(
			extension,
			instance,
			pluginjob.ScheduleOptions{Timeout: timeout, Enabled: true},
			fetcher,
			requestSource,
			sink,
		)
		if err != nil {
			return nil, fmt.Errorf("compose source workload for instance %q: %w", instance.ID, err)
		}
		if _, exists := seenJobIDs[registration.Definition.ID]; exists {
			return nil, fmt.Errorf("compose source workload for instance %q: duplicate Job ID %q", instance.ID, registration.Definition.ID)
		}
		seenJobIDs[registration.Definition.ID] = struct{}{}
		registrations = append(registrations, registration)
		definitions = append(definitions, registration.Definition)
	}

	for _, registration := range registrations {
		if err := scheduler.Register(registration.Definition, registration.Job); err != nil {
			return nil, fmt.Errorf("register production source workload %q: %w", registration.Definition.ID, err)
		}
	}
	return definitions, nil
}
