package aiadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/airuntime"
	"lum-harbor-notifier/internal/plugin"
)

const (
	runtimePhaseStart  = "START"
	runtimePhaseResume = "RESUME"
	runtimeNeedCalls   = "NEED_CAPABILITIES"
	runtimeFinal       = "FINAL"
)

type runtimeWireRequest struct {
	Phase        string                       `json:"phase"`
	ExecutionID  string                       `json:"executionId"`
	Request      *airuntime.Request           `json:"request,omitempty"`
	Capabilities []airuntime.Capability       `json:"capabilities,omitempty"`
	State        json.RawMessage              `json:"state,omitempty"`
	Results      []airuntime.CapabilityResult `json:"results,omitempty"`
}

type runtimeWireResponse struct {
	Status   string                     `json:"status"`
	State    json.RawMessage            `json:"state,omitempty"`
	Calls    []airuntime.CapabilityCall `json:"calls,omitempty"`
	Output   json.RawMessage            `json:"output,omitempty"`
	Usage    *ai.Usage                  `json:"usage,omitempty"`
	Metadata map[string]string          `json:"metadata,omitempty"`
}

// Runtime is a full high-level Plugin AI Runtime. Core retains transport and
// capability authority through a bounded START/RESUME loop.
type Runtime struct {
	invoker     plugin.Invoker
	extensionID string
	instance    *plugin.Instance
	host        airuntime.Host
}

func NewRuntime(invoker plugin.Invoker, extensionID string, instance *plugin.Instance, host airuntime.Host) (*Runtime, error) {
	if invoker == nil || strings.TrimSpace(extensionID) == "" || instance == nil || host == nil {
		return nil, errors.New("plugin AI runtime requires invoker, extension, instance, and capability host")
	}
	return &Runtime{invoker: invoker, extensionID: extensionID, instance: cloneInstance(instance), host: host}, nil
}

func (r *Runtime) Execute(ctx context.Context, request airuntime.Request) (airuntime.Result, error) {
	limits := request.Limits
	defaults := airuntime.DefaultLimits()
	if limits.MaxRounds == 0 {
		limits.MaxRounds = defaults.MaxRounds
	}
	if limits.MaxCalls == 0 {
		limits.MaxCalls = defaults.MaxCalls
	}
	if limits.MaxParallelCalls == 0 {
		limits.MaxParallelCalls = defaults.MaxParallelCalls
	}
	if limits.MaxContextBytes == 0 {
		limits.MaxContextBytes = defaults.MaxContextBytes
	}
	if limits.MaxOutputBytes == 0 {
		limits.MaxOutputBytes = defaults.MaxOutputBytes
	}
	if limits.MaxRounds < 1 || limits.MaxRounds > 8 || limits.MaxCalls < 0 || limits.MaxCalls > 32 || limits.MaxParallelCalls < 1 || limits.MaxParallelCalls > 8 || limits.MaxContextBytes < 1024 || limits.MaxOutputBytes < 1024 {
		return airuntime.Result{}, fmt.Errorf("%s: invalid execution limits", airuntime.ErrorValidation)
	}
	request.Limits = limits
	wire := runtimeWireRequest{Phase: runtimePhaseStart, ExecutionID: request.RequestID, Request: &request, Capabilities: r.host.ListCapabilities()}
	var evidence []airuntime.CapabilityResult
	totalCalls := 0
	var usage ai.Usage
	for round := 0; round <= limits.MaxRounds; round++ {
		var response runtimeWireResponse
		if err := invoke(ctx, r.invoker, r.extensionID, plugin.CapabilityAIRuntime, r.instance, wire, &response); err != nil {
			return airuntime.Result{}, fmt.Errorf("%s: %w", airuntime.ErrorPluginProtocol, err)
		}
		if response.Usage != nil {
			usage.InputTokens += response.Usage.InputTokens
			usage.OutputTokens += response.Usage.OutputTokens
			usage.TotalTokens += response.Usage.TotalTokens
		}
		switch response.Status {
		case runtimeFinal:
			if len(response.Calls) != 0 || len(bytes.TrimSpace(response.Output)) == 0 || !json.Valid(response.Output) {
				return airuntime.Result{}, fmt.Errorf("%s: invalid FINAL response", airuntime.ErrorPluginProtocol)
			}
			if limits.MaxOutputBytes > 0 && len(response.Output) > limits.MaxOutputBytes {
				return airuntime.Result{}, fmt.Errorf("%s: output exceeds limit", airuntime.ErrorValidation)
			}
			return airuntime.Result{RequestID: request.RequestID, Purpose: request.Purpose, Output: append(json.RawMessage(nil), response.Output...), Evidence: evidence, Usage: &usage, Metadata: response.Metadata,
				Provenance: ai.ResponseProvenance{ProviderInvoked: true, ActualProvider: &ai.ProviderInfo{ID: r.extensionID, Name: r.extensionID, Source: ai.ProviderSourcePlugin}, Origin: ai.ResponseOrigin{Kind: ai.ResponseOriginProvider, ID: r.extensionID, Name: r.extensionID}}}, nil
		case runtimeNeedCalls:
			if round == limits.MaxRounds || len(response.Calls) == 0 || len(response.Calls) > limits.MaxParallelCalls || totalCalls+len(response.Calls) > limits.MaxCalls {
				return airuntime.Result{}, fmt.Errorf("%s: capability limits exceeded", airuntime.ErrorPluginProtocol)
			}
			if len(response.State) == 0 || !json.Valid(response.State) {
				return airuntime.Result{}, fmt.Errorf("%s: NEED_CAPABILITIES requires valid opaque state", airuntime.ErrorPluginProtocol)
			}
			results, err := r.host.ExecuteReads(ctx, response.Calls)
			if err != nil {
				return airuntime.Result{}, err
			}
			for _, result := range results {
				if result.Error != nil && result.Error.Code != airuntime.ErrorCapabilityExecution {
					return airuntime.Result{}, fmt.Errorf("%s: %s: %s", airuntime.ErrorPluginProtocol, result.Error.Code, result.Error.Message)
				}
			}
			evidence = append(evidence, results...)
			if encoded, _ := json.Marshal(evidence); len(encoded) > limits.MaxContextBytes {
				return airuntime.Result{}, fmt.Errorf("%s: capability context exceeds limit", airuntime.ErrorValidation)
			}
			totalCalls += len(response.Calls)
			wire = runtimeWireRequest{Phase: runtimePhaseResume, ExecutionID: request.RequestID, State: append(json.RawMessage(nil), response.State...), Results: results}
		default:
			return airuntime.Result{}, fmt.Errorf("%s: unsupported status %q", airuntime.ErrorPluginProtocol, response.Status)
		}
	}
	return airuntime.Result{}, fmt.Errorf("%s: runtime did not finish", airuntime.ErrorPluginProtocol)
}
