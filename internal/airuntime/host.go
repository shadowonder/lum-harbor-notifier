package airuntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"lum-harbor-notifier/internal/proposalcap"
	"lum-harbor-notifier/internal/readcap"
)

type ReadSource interface {
	ListDescriptors() []readcap.Descriptor
	Execute(context.Context, readcap.Request) (readcap.Result, error)
}

// CapabilityHost adapts the already-authoritative readcap and proposalcap
// boundaries into one AI-facing catalog. It does not duplicate either
// subsystem's validation or execution logic.
type CapabilityHost struct {
	reads    ReadSource
	proposes proposalcap.Source
}

func NewCapabilityHost(reads ReadSource, proposes proposalcap.Source) (*CapabilityHost, error) {
	if reads == nil || proposes == nil {
		return nil, errors.New("AI capability host requires read and proposal sources")
	}
	h := &CapabilityHost{reads: reads, proposes: proposes}
	seen := map[string]struct{}{}
	for _, descriptor := range h.ListCapabilities() {
		if _, exists := seen[descriptor.ID]; exists {
			return nil, fmt.Errorf("duplicate AI capability %q", descriptor.ID)
		}
		seen[descriptor.ID] = struct{}{}
	}
	return h, nil
}

func (h *CapabilityHost) ListCapabilities() []Capability {
	readDescriptors := h.reads.ListDescriptors()
	result := make([]Capability, 0, len(readDescriptors)+len(h.proposes.ListDescriptors()))
	for _, descriptor := range readDescriptors {
		result = append(result, Capability{
			ID: descriptor.ID, Kind: CapabilityRead, Description: descriptor.Description,
			UseWhen: descriptor.Description, InputSemantics: descriptor.InputSemantics,
			ResultSemantics: descriptor.ResultSemantics, Origin: string(descriptor.Origin),
		})
	}
	for _, descriptor := range h.proposes.ListDescriptors() {
		result = append(result, Capability{
			ID: descriptor.ID, Kind: CapabilityPropose, Description: descriptor.Summary,
			UseWhen: descriptor.UseWhen, InputSemantics: renderProposalArguments(descriptor),
			ResultSemantics: "Creates one durable pending Proposal for human review; it never directly mutates domain truth.",
			Origin:          "CORE",
		})
	}
	return result
}

func renderProposalArguments(descriptor proposalcap.Descriptor) string {
	if len(descriptor.Arguments) == 0 {
		return "JSON object {}"
	}
	names := make([]string, 0, len(descriptor.Arguments))
	for name := range descriptor.Arguments {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		argument := descriptor.Arguments[name]
		parts = append(parts, fmt.Sprintf("%s (%s, required=%t): %s", name, argument.Type, argument.Required, argument.Description))
	}
	return "JSON object {" + strings.Join(parts, "; ") + "}"
}

func (h *CapabilityHost) ExecuteReads(ctx context.Context, calls []CapabilityCall) ([]CapabilityResult, error) {
	capabilities := h.ListCapabilities()
	byID := make(map[string]Capability, len(capabilities))
	for _, capability := range capabilities {
		byID[capability.ID] = capability
	}
	results := make([]CapabilityResult, len(calls))
	var wg sync.WaitGroup
	for index, call := range calls {
		index, call := index, call
		results[index] = CapabilityResult{Index: index, Call: call}
		capability, found := byID[call.ID]
		if !found {
			results[index].Error = &RuntimeError{Code: ErrorCapabilityNotFound, Message: fmt.Sprintf("capability %q is not available", call.ID)}
			continue
		}
		if capability.Kind != CapabilityRead {
			results[index].Error = &RuntimeError{Code: ErrorCapabilityNotAllowed, Message: fmt.Sprintf("capability %q is not a READ capability", call.ID)}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			readResult, err := h.reads.Execute(ctx, readcap.Request{CapabilityID: call.ID, Input: call.Arguments})
			if err != nil {
				if errors.Is(err, readcap.ErrEntityNotFound) {
					results[index].Output = json.RawMessage(`{"found":false,"status":"NOT_FOUND"}`)
					results[index].Value = map[string]any{"found": false, "status": "NOT_FOUND"}
					return
				}
				code := ErrorCapabilityExecution
				if errors.Is(err, readcap.ErrInvalidInput) {
					code = ErrorCapabilityInput
				} else if errors.Is(err, readcap.ErrUnknownCapability) {
					code = ErrorCapabilityNotFound
				}
				results[index].Error = &RuntimeError{Code: code, Message: err.Error()}
				return
			}
			encoded, err := json.Marshal(readResult.Output)
			if err != nil {
				results[index].Error = &RuntimeError{Code: ErrorCapabilityExecution, Message: "capability result could not be encoded"}
				return
			}
			results[index].Output = encoded
			results[index].Value = readResult.Output
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
