// Package readadapter is the seam between Plugin Runtime and
// internal/readcap (M15 Slice 2). It is the only package that understands
// both a resolved Plugin read.query Instance and a readcap.Descriptor: it
// maps each into the other, and translates one serialized readcap.Execute
// call into one plugin.ReadQuerier.Query call and back.
//
// internal/readcap never imports internal/plugin — it only sees the narrow
// readcap.PluginReadSource interface this package's Source satisfies. This
// package, not readcap, is the one place that may import both.
package readadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/readcap"
)

type entry struct {
	descriptor readcap.Descriptor
	instance   plugin.Instance
}

// Source is a readcap.PluginReadSource backed by real, resolved Plugin
// Instances that select the read.query capability. It never invokes a
// disabled, unresolved, or non-read Instance — New only ever sees Instances
// its caller already resolved (plugininstance.Resolve), so filtering to
// CapabilityID == read.query is the only additional narrowing this package
// performs.
type Source struct {
	querier *plugin.ReadQuerier
	entries []entry
}

// New builds a Source from every resolved Instance in instances that
// selects plugin.CapabilityReadQuery. registry supplies each Instance's
// extension manifest, so this package — not readcap — owns turning a
// Plugin's declared semantic metadata (CapabilityDeclaration.Description/
// InputSemantics/ResultSemantics) into a readcap.Descriptor.
//
// Descriptor IDs are "plugin:<extensionId>:<instanceId>" — deterministic
// across restart (Instance IDs are stable, server-generated, and persisted
// in config.json per the Plugin Runtime Contract), never derived from a
// display name, and never embedding config or secret values. The "plugin:"
// prefix can never collide with a CORE capability ID (all of which are
// bare dotted names with no colon).
func New(invoker plugin.Invoker, registry *plugin.Registry, instances []plugin.Instance) (*Source, error) {
	if invoker == nil {
		return nil, fmt.Errorf("readadapter: invoker must not be nil")
	}
	if registry == nil {
		return nil, fmt.Errorf("readadapter: registry must not be nil")
	}
	var entries []entry
	for _, instance := range instances {
		if instance.CapabilityID != plugin.CapabilityReadQuery {
			continue
		}
		extension, err := registry.Find(instance.ExtensionID)
		if err != nil {
			return nil, fmt.Errorf("readadapter: resolve extension %q for instance %q: %w", instance.ExtensionID, instance.ID, err)
		}
		declaration, found := findCapability(extension.Manifest, plugin.CapabilityReadQuery)
		if !found {
			return nil, fmt.Errorf("readadapter: extension %q does not declare %q", instance.ExtensionID, plugin.CapabilityReadQuery)
		}
		entries = append(entries, entry{
			descriptor: readcap.Descriptor{
				ID:              fmt.Sprintf("plugin:%s:%s", instance.ExtensionID, instance.ID),
				Description:     declaration.Description,
				InputSemantics:  declaration.InputSemantics,
				ResultSemantics: declaration.ResultSemantics,
				Origin:          readcap.OriginPlugin,
			},
			instance: instance,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].descriptor.ID < entries[j].descriptor.ID })
	return &Source{querier: plugin.NewReadQuerier(invoker), entries: entries}, nil
}

func findCapability(manifest plugin.Manifest, id plugin.CapabilityID) (plugin.CapabilityDeclaration, bool) {
	for _, declaration := range manifest.Capabilities {
		if declaration.ID == id {
			return declaration, true
		}
	}
	return plugin.CapabilityDeclaration{}, false
}

// ListDescriptors returns one Descriptor per active, resolved read.query
// Instance, in stable ID order. It satisfies readcap.PluginReadSource.
func (s *Source) ListDescriptors() []readcap.Descriptor {
	out := make([]readcap.Descriptor, len(s.entries))
	for index, e := range s.entries {
		out[index] = e.descriptor
	}
	return out
}

// Execute decodes input as a plugin.ReadQueryRequest, invokes the real
// Plugin Instance named by descriptorID, and maps its bounded, validated
// result into a readcap.PluginQueryOutput. It satisfies
// readcap.PluginReadSource; readcap.Service only ever calls this with a
// descriptorID one of this Source's own ListDescriptors just returned.
func (s *Source) Execute(ctx context.Context, descriptorID string, input json.RawMessage) (readcap.Result, error) {
	for _, e := range s.entries {
		if e.descriptor.ID != descriptorID {
			continue
		}
		var request plugin.ReadQueryRequest
		if len(input) == 0 {
			return readcap.Result{}, fmt.Errorf("%w: %s requires a non-empty Input", readcap.ErrInvalidInput, descriptorID)
		}
		if err := json.Unmarshal(input, &request); err != nil {
			return readcap.Result{}, fmt.Errorf("%w: %s: %v", readcap.ErrInvalidInput, descriptorID, err)
		}
		result, err := s.querier.Query(ctx, e.instance, request)
		if err != nil {
			return readcap.Result{}, err
		}
		facts := make([]readcap.Fact, len(result.Facts))
		for index, fact := range result.Facts {
			facts[index] = readcap.Fact{Name: fact.Name, Value: fact.Value}
		}
		return readcap.Result{
			CapabilityID: descriptorID,
			Output:       readcap.PluginQueryOutput{Summary: result.Summary, Facts: facts},
		}, nil
	}
	return readcap.Result{}, fmt.Errorf("%w: %q", readcap.ErrUnknownCapability, descriptorID)
}
