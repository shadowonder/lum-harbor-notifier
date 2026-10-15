package airuntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"lum-harbor-notifier/internal/ai"
)

var (
	ErrInvalidRequest = errors.New("AI runtime invalid request")
	ErrLimitExceeded  = errors.New("AI runtime limit exceeded")
	ErrProtocol       = errors.New("AI runtime protocol error")
)

type Model interface {
	Execute(context.Context, ai.Request) (ai.Response, error)
}

// Native is the built-in high-level Runtime. The injected Model is only the
// lower-level inference/transport seam; Native owns capability reasoning,
// execution rounds, accumulated context, and final-result validation.
type Native struct {
	model Model
	host  Host
}

func NewNative(model Model, host Host) (*Native, error) {
	if model == nil || host == nil {
		return nil, errors.New("native AI runtime requires model and capability host")
	}
	return &Native{model: model, host: host}, nil
}

type decisionEnvelope struct {
	Version  int
	Decision string
	Calls    []CapabilityCall
}

type decisionWireEnvelope struct {
	Version  int                `json:"version"`
	Decision string             `json:"decision"`
	Calls    []decisionWireCall `json:"calls"`
}

type decisionWireCall struct {
	CapabilityID  string `json:"capability_id"`
	ArgumentsJSON string `json:"arguments_json"`
}

var decisionSchema = json.RawMessage(`{"type":"object","additionalProperties":false,"required":["version","decision","calls"],"properties":{"version":{"type":"integer","const":1},"decision":{"type":"string","enum":["FINAL","CALL"]},"calls":{"type":"array","maxItems":4,"items":{"type":"object","additionalProperties":false,"required":["capability_id","arguments_json"],"properties":{"capability_id":{"type":"string"},"arguments_json":{"type":"string"}}}}}}`)

func (n *Native) Execute(ctx context.Context, request Request) (Result, error) {
	limits, err := normalizeRequest(&request)
	if err != nil {
		return Result{}, err
	}
	capabilities := n.host.ListCapabilities()
	var evidence []CapabilityResult
	totalCalls := 0
	var lastProvenance ai.ResponseProvenance
	var totalUsage ai.Usage

	for round := 0; round < limits.MaxRounds; round++ {
		decisionRequest := ai.Request{
			RequestID:    fmt.Sprintf("%s:capabilities:%d", request.RequestID, round+1),
			Action:       ai.ActionRuntimeCapabilityDecision,
			Messages:     buildDecisionMessages(request, capabilities, evidence, limits.MaxCalls-totalCalls),
			OutputSchema: append(json.RawMessage(nil), decisionSchema...),
			Metadata:     cloneMap(request.Metadata),
		}
		response, err := n.model.Execute(ctx, decisionRequest)
		if err != nil {
			return Result{}, fmt.Errorf("%w: capability decision: %v", ErrProtocol, err)
		}
		lastProvenance = response.Provenance
		addUsage(&totalUsage, response.Usage)
		decision, err := decodeDecision(response.Output)
		if err != nil {
			return Result{}, err
		}
		if decision.Decision == "FINAL" {
			break
		}
		if len(decision.Calls) == 0 || len(decision.Calls) > limits.MaxParallelCalls {
			return Result{}, fmt.Errorf("%w: runtime requested %d parallel calls", ErrLimitExceeded, len(decision.Calls))
		}
		if totalCalls+len(decision.Calls) > limits.MaxCalls {
			return Result{}, fmt.Errorf("%w: total capability calls exceed %d", ErrLimitExceeded, limits.MaxCalls)
		}
		results, err := n.host.ExecuteReads(ctx, decision.Calls)
		if err != nil {
			return Result{}, classifyContextError(err)
		}
		totalCalls += len(decision.Calls)
		evidence = append(evidence, results...)
		for _, result := range results {
			if result.Error != nil {
				return Result{}, fmt.Errorf("%w: %s: %s", ErrProtocol, result.Error.Code, result.Error.Message)
			}
		}
		if encoded, _ := json.Marshal(evidence); len(encoded) > limits.MaxContextBytes {
			return Result{}, fmt.Errorf("%w: capability context exceeds %d bytes", ErrLimitExceeded, limits.MaxContextBytes)
		}
		if totalCalls >= limits.MaxCalls {
			break
		}
		if round == limits.MaxRounds-1 {
			return Result{}, fmt.Errorf("%w: capability rounds exceed %d", ErrLimitExceeded, limits.MaxRounds)
		}
	}

	finalRequest := ai.Request{
		RequestID:    request.RequestID,
		Action:       request.Purpose,
		Messages:     buildFinalMessages(request, capabilities, evidence),
		OutputSchema: append(json.RawMessage(nil), request.OutputSchema...),
		Metadata:     cloneMap(request.Metadata),
	}
	response, err := n.model.Execute(ctx, finalRequest)
	if err != nil {
		return Result{}, fmt.Errorf("%w: final model execution: %v", ErrProtocol, err)
	}
	if len(response.Output) > limits.MaxOutputBytes {
		return Result{}, fmt.Errorf("%w: output exceeds %d bytes", ErrLimitExceeded, limits.MaxOutputBytes)
	}
	lastProvenance = response.Provenance
	addUsage(&totalUsage, response.Usage)
	return Result{
		RequestID: request.RequestID, Purpose: request.Purpose,
		Output: append(json.RawMessage(nil), response.Output...), Evidence: cloneEvidence(evidence),
		Provenance: lastProvenance, Usage: &totalUsage, Metadata: cloneMap(response.Metadata),
	}, nil
}

func normalizeRequest(request *Request) (Limits, error) {
	if request == nil || strings.TrimSpace(request.RequestID) == "" || strings.TrimSpace(string(request.Purpose)) == "" {
		return Limits{}, fmt.Errorf("%w: request ID and purpose are required", ErrInvalidRequest)
	}
	if strings.TrimSpace(request.Input) == "" || strings.TrimSpace(request.Instructions) == "" {
		return Limits{}, fmt.Errorf("%w: input and instructions are required", ErrInvalidRequest)
	}
	if !json.Valid(request.OutputSchema) {
		return Limits{}, fmt.Errorf("%w: output schema must be valid JSON", ErrInvalidRequest)
	}
	limits := request.Limits
	defaults := DefaultLimits()
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
		return Limits{}, fmt.Errorf("%w: invalid execution limits", ErrInvalidRequest)
	}
	request.Limits = limits
	return limits, nil
}

func buildDecisionMessages(request Request, capabilities []Capability, evidence []CapabilityResult, remaining int) []ai.Message {
	messages := []ai.Message{{Role: ai.RoleSystem, Content: `You are the capability-selection stage of Lum Harbor's AI Runtime.
Decide whether authoritative READ capabilities are needed before producing the caller's final result.
Return CALL with one or more independent READ calls when facts are needed. Calls in one round execute concurrently.
Return FINAL when the supplied context and accumulated results are sufficient.
For each CALL item, return capability_id and arguments_json. arguments_json must be a JSON-encoded object string matching that capability's documented input.
Never request a PROPOSE capability here; Proposal intent belongs only in the caller's final output.
Never invent capability IDs or arguments. Capability errors are evidence; do not retry the identical failed call.
Intent outranks identifier shape: explicit creation names are new-object data, existing titles use search, and explicitly external identifiers use external resolution.
Return only the required JSON envelope.` + "\n\nCaller final-result policy:\n" + request.Instructions + "\n\n" + renderCapabilities(capabilities, CapabilityRead) + fmt.Sprintf("\nRemaining total call budget: %d", remaining)}}
	messages = append(messages, cloneMessages(request.Context)...)
	if len(evidence) > 0 {
		encoded, _ := json.Marshal(evidence)
		messages = append(messages, ai.Message{Role: ai.RoleSystem, Content: "Accumulated capability results in request order:\n" + string(encoded)})
	}
	messages = append(messages, ai.Message{Role: ai.RoleUser, Content: request.Input})
	return messages
}

func buildFinalMessages(request Request, capabilities []Capability, evidence []CapabilityResult) []ai.Message {
	messages := []ai.Message{{Role: ai.RoleSystem, Content: request.Instructions + "\n\n" + renderCapabilities(capabilities, "")}}
	messages = append(messages, cloneMessages(request.Context)...)
	if len(evidence) == 0 {
		messages = append(messages, ai.Message{Role: ai.RoleSystem, Content: "No capabilities were executed. Do not claim that Core or an external system was queried."})
	} else {
		encoded, _ := json.Marshal(evidence)
		messages = append(messages, ai.Message{Role: ai.RoleSystem, Content: "Read capability execution result(s) in request order:\n" + string(encoded)})
	}
	messages = append(messages, ai.Message{Role: ai.RoleUser, Content: request.Input})
	return messages
}

func renderCapabilities(capabilities []Capability, kind CapabilityKind) string {
	filtered := make([]Capability, 0, len(capabilities))
	for _, capability := range capabilities {
		if kind == "" || capability.Kind == kind {
			filtered = append(filtered, capability)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool { return filtered[i].ID < filtered[j].ID })
	var builder strings.Builder
	builder.WriteString("Available capabilities:\n")
	for _, capability := range filtered {
		fmt.Fprintf(&builder, "- %s [%s, %s]: %s\n  Use when: %s\n  Input: %s\n  Result: %s\n", capability.ID, capability.Kind, capability.Origin, capability.Description, capability.UseWhen, capability.InputSemantics, capability.ResultSemantics)
	}
	if len(filtered) == 0 {
		builder.WriteString("(none)\n")
	}
	return builder.String()
}

func decodeDecision(raw json.RawMessage) (decisionEnvelope, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var wire decisionWireEnvelope
	if err := decoder.Decode(&wire); err != nil {
		return decisionEnvelope{}, fmt.Errorf("%w: decode capability decision: %v", ErrProtocol, err)
	}
	decision := decisionEnvelope{Version: wire.Version, Decision: wire.Decision}
	if decision.Version != 1 {
		return decisionEnvelope{}, fmt.Errorf("%w: unsupported decision version", ErrProtocol)
	}
	switch decision.Decision {
	case "FINAL":
		if len(wire.Calls) != 0 {
			return decisionEnvelope{}, fmt.Errorf("%w: FINAL must not include calls", ErrProtocol)
		}
		return decision, nil
	case "CALL":
	default:
		return decisionEnvelope{}, fmt.Errorf("%w: unsupported decision %q", ErrProtocol, decision.Decision)
	}
	decision.Calls = make([]CapabilityCall, len(wire.Calls))
	for index, call := range wire.Calls {
		capabilityID := strings.TrimSpace(call.CapabilityID)
		arguments := bytes.TrimSpace([]byte(call.ArgumentsJSON))
		var object map[string]json.RawMessage
		if capabilityID == "" || len(arguments) == 0 || arguments[0] != '{' || json.Unmarshal(arguments, &object) != nil {
			return decisionEnvelope{}, fmt.Errorf("%w: invalid capability call %d", ErrProtocol, index)
		}
		decision.Calls[index] = CapabilityCall{ID: capabilityID, Arguments: append(json.RawMessage(nil), arguments...)}
	}
	return decision, nil
}

func cloneMessages(values []ai.Message) []ai.Message { return append([]ai.Message(nil), values...) }
func cloneMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	out := make(map[string]string, len(values))
	for k, v := range values {
		out[k] = v
	}
	return out
}
func cloneEvidence(values []CapabilityResult) []CapabilityResult {
	out := make([]CapabilityResult, len(values))
	copy(out, values)
	for i := range out {
		out[i].Call.Arguments = append(json.RawMessage(nil), values[i].Call.Arguments...)
		out[i].Output = append(json.RawMessage(nil), values[i].Output...)
		if values[i].Error != nil {
			e := *values[i].Error
			out[i].Error = &e
		}
	}
	return out
}
func addUsage(total *ai.Usage, usage *ai.Usage) {
	if usage != nil {
		total.InputTokens += usage.InputTokens
		total.OutputTokens += usage.OutputTokens
		total.TotalTokens += usage.TotalTokens
	}
}
func classifyContextError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: %w", ErrorTimeout, err)
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s: %w", ErrorCanceled, err)
	}
	return err
}
