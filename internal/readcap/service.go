package readcap

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/foundation"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// ErrUnknownCapability is returned when Request.CapabilityID names no
// Descriptor in this package (neither CORE nor, if configured, active
// PLUGIN).
var ErrUnknownCapability = errors.New("readcap: unknown capability")

// ErrInvalidInput is returned when Request.Input is missing, is not valid
// JSON, does not decode into the requested capability's input shape, or
// fails that capability's own bounded validation — always before any domain
// read or Plugin invocation is attempted.
var ErrInvalidInput = errors.New("readcap: invalid input")

// ErrEntityNotFound is returned by workitem.read and entity.resolve_external
// when the requested entity or identity does not exist. It is never returned
// by entity.search, which reports absence as an empty candidate list.
var ErrEntityNotFound = errors.New("readcap: entity not found")

// PluginReadSource is the narrow seam Service uses to discover and execute
// PLUGIN-origin read capabilities (M15 Slice 2), satisfied by
// internal/plugin/readadapter.Source. Service never imports internal/plugin
// itself — it only ever sees this interface, so it can never reach Plugin
// process/RPC/manifest internals.
//
// ListDescriptors must return only active, resolved, read-capable
// descriptors — Service trusts this list completely when deciding whether a
// CapabilityID belongs to this source. Execute is only ever called with a
// descriptorID this source's own ListDescriptors just returned.
type PluginReadSource interface {
	ListDescriptors() []Descriptor
	Execute(ctx context.Context, descriptorID string, input json.RawMessage) (Result, error)
}

// Service lists and executes the permitted read capabilities: a fixed CORE
// set backed directly by existing WorkItem/Tracking/Planner/Foundation
// domain reads, plus (if configured) an active PLUGIN set backed by
// PluginReadSource. Service opens no transaction of its own and exposes no
// Repository, dbgen type, sql.Rows, or other persistence/process detail to
// its caller.
type Service struct {
	workItems    *workitem.Service
	tracking     *tracking.Service
	planner      *planner.Service
	identity     *foundation.Repository
	attention    *attention.Service
	history      *history.Service
	pluginSource PluginReadSource
}

// NewService constructs a Service. workItems, trackingService,
// plannerService, and db are required. pluginSource, attentionService,
// and historyService may be nil if those capabilities are not configured.
func NewService(
	workItems *workitem.Service,
	trackingService *tracking.Service,
	plannerService *planner.Service,
	db *sql.DB,
	pluginSource PluginReadSource,
	attentionService *attention.Service,
	historyService *history.Service,
) (*Service, error) {
	if workItems == nil || trackingService == nil || plannerService == nil || db == nil {
		return nil, fmt.Errorf("readcap: service requires non-nil WorkItem, Tracking, Planner services and a database handle")
	}
	return &Service{
		workItems:    workItems,
		tracking:     trackingService,
		planner:      plannerService,
		identity:     foundation.NewRepository(db),
		attention:    attentionService,
		history:      historyService,
		pluginSource: pluginSource,
	}, nil
}

// Request names one capability to execute and carries its serialized input.
// Input is the JSON encoding of the exact shape that capability's
// Descriptor.InputSemantics names. A caller (M16) needs only a Descriptor's
// ID and plain JSON — never a capability-specific Go input type, and never a
// decode switch of its own; Service owns that knowledge entirely.
type Request struct {
	CapabilityID string
	Input        json.RawMessage
}

// Result carries one capability's typed output. Output is always the exact
// Go type that capability's Descriptor.ResultSemantics names (a readcap-
// owned shape for every CORE and PLUGIN capability alike — never a
// Plugin/domain-internal type).
type Result struct {
	CapabilityID string
	Output       any
}

// ListDescriptors returns the permitted capability set in stable,
// deterministic order: the fixed CORE set first, then (if a PluginReadSource
// is configured) active PLUGIN descriptors ordered by their own stable ID.
// The returned slice is always a fresh copy; mutating it has no effect on
// this package's own registry or on any PluginReadSource.
func (s *Service) ListDescriptors() []Descriptor {
	out := make([]Descriptor, len(descriptors))
	copy(out, descriptors)
	if s.pluginSource != nil {
		out = append(out, s.pluginSource.ListDescriptors()...)
	}
	return out
}

// Execute decodes Request.Input against the requested capability and, if
// valid, performs exactly one bounded read. An unrecognized CapabilityID,
// malformed JSON, or a JSON value that fails the capability's own bounded
// field validation is rejected before any domain read or Plugin invocation
// is attempted.
func (s *Service) Execute(ctx context.Context, req Request) (Result, error) {
	switch req.CapabilityID {
	case CapabilityWorkItemRead:
		var input WorkItemReadInput
		if err := decodeInput(req.Input, &input, CapabilityWorkItemRead); err != nil {
			return Result{}, err
		}
		output, err := s.readWorkItem(ctx, input)
		if err != nil {
			return Result{}, err
		}
		return Result{CapabilityID: req.CapabilityID, Output: output}, nil

	case CapabilityResolveExternal:
		var input ResolveExternalInput
		if err := decodeInput(req.Input, &input, CapabilityResolveExternal); err != nil {
			return Result{}, err
		}
		output, err := s.resolveExternal(ctx, input)
		if err != nil {
			return Result{}, err
		}
		return Result{CapabilityID: req.CapabilityID, Output: output}, nil

	case CapabilitySearch:
		var input SearchInput
		if err := decodeInput(req.Input, &input, CapabilitySearch); err != nil {
			return Result{}, err
		}
		output, err := s.search(ctx, input)
		if err != nil {
			return Result{}, err
		}
		return Result{CapabilityID: req.CapabilityID, Output: output}, nil

	case CapabilityAttentionReadForWorkItem:
		var input AttentionReadForWorkItemInput
		if err := decodeInput(req.Input, &input, CapabilityAttentionReadForWorkItem); err != nil {
			return Result{}, err
		}
		output, err := s.attentionReadForWorkItem(ctx, input)
		if err != nil {
			return Result{}, err
		}
		return Result{CapabilityID: req.CapabilityID, Output: output}, nil

	case CapabilityHistoryReadForWorkItem:
		var input HistoryReadForWorkItemInput
		if err := decodeInput(req.Input, &input, CapabilityHistoryReadForWorkItem); err != nil {
			return Result{}, err
		}
		output, err := s.historyReadForWorkItem(ctx, input)
		if err != nil {
			return Result{}, err
		}
		return Result{CapabilityID: req.CapabilityID, Output: output}, nil

	case CapabilityWorkItemList:
		var input WorkItemListInput
		if len(req.Input) > 0 && string(req.Input) != "{}" && string(req.Input) != "null" {
			if err := decodeInput(req.Input, &input, CapabilityWorkItemList); err != nil {
				return Result{}, err
			}
		}
		output, err := s.listWorkItems(ctx, input)
		if err != nil {
			return Result{}, err
		}
		return Result{CapabilityID: req.CapabilityID, Output: output}, nil

	default:
		if s.pluginSource != nil {
			for _, descriptor := range s.pluginSource.ListDescriptors() {
				if descriptor.ID == req.CapabilityID {
					return s.pluginSource.Execute(ctx, req.CapabilityID, req.Input)
				}
			}
		}
		return Result{}, fmt.Errorf("%w: %q", ErrUnknownCapability, req.CapabilityID)
	}
}

// decodeInput unmarshals raw into target, rejecting empty input, malformed
// JSON, and trailing content after the JSON value — all before any domain
// read runs. Per-field bounded validation (non-empty required strings,
// length limits, etc.) still happens inside each capability's own
// implementation once decoding succeeds.
func decodeInput(raw json.RawMessage, target any, capabilityID string) error {
	if len(raw) == 0 {
		return fmt.Errorf("%w: %s requires a non-empty Input", ErrInvalidInput, capabilityID)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalidInput, capabilityID, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err == nil {
		return fmt.Errorf("%w: %s: trailing content after JSON value", ErrInvalidInput, capabilityID)
	}
	return nil
}
