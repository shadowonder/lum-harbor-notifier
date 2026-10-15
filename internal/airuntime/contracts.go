// Package airuntime owns Lum Harbor's high-level AI reasoning loop.
//
// Callers provide a business purpose, initial context, and a final-result
// contract. Runtime implementations decide when to request capabilities.
// Core's Host remains the only authority that can execute those requests.
package airuntime

import (
	"context"
	"encoding/json"

	"lum-harbor-notifier/internal/ai"
)

type CapabilityKind string

const (
	CapabilityRead    CapabilityKind = "READ"
	CapabilityPropose CapabilityKind = "PROPOSE"
)

// Capability is the single AI-facing descriptor used for both Core and
// Plugin methods. It deliberately stays smaller than an API-description
// framework: a model only needs to know when to use a method, its JSON input,
// its result semantics, and whether it reads or proposes.
type Capability struct {
	ID              string         `json:"id"`
	Kind            CapabilityKind `json:"kind"`
	Description     string         `json:"description"`
	UseWhen         string         `json:"useWhen,omitempty"`
	InputSemantics  string         `json:"inputSemantics"`
	ResultSemantics string         `json:"resultSemantics"`
	Origin          string         `json:"origin"`
}

type CapabilityCall struct {
	ID        string          `json:"id"`
	Arguments json.RawMessage `json:"arguments"`
}

type CapabilityResult struct {
	Index  int             `json:"index"`
	Call   CapabilityCall  `json:"call"`
	Output json.RawMessage `json:"output,omitempty"`
	Error  *RuntimeError   `json:"error,omitempty"`
	// Value preserves the authoritative Core result type for in-process
	// callers. It is deliberately excluded from the Plugin wire contract,
	// which uses Output as canonical JSON.
	Value any `json:"-"`
}

type RuntimeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

const (
	ErrorRuntime              = "runtime_error"
	ErrorCapabilityNotFound   = "capability_not_found"
	ErrorCapabilityNotAllowed = "capability_not_allowed"
	ErrorCapabilityInput      = "capability_input_invalid"
	ErrorCapabilityExecution  = "capability_execution_failed"
	ErrorModel                = "model_error"
	ErrorPluginProtocol       = "plugin_protocol_error"
	ErrorValidation           = "validation_error"
	ErrorTimeout              = "timeout"
	ErrorCanceled             = "canceled"
)

// Host is the complete authority surface visible to a Runtime. It exposes
// descriptors and executes bounded READ calls; it never exposes repositories,
// SQL handles, or domain services. PROPOSE descriptors are selection targets
// for a final intent and are never directly mutated through ExecuteReads.
type Host interface {
	ListCapabilities() []Capability
	ExecuteReads(context.Context, []CapabilityCall) ([]CapabilityResult, error)
}

type Limits struct {
	MaxRounds        int `json:"maxRounds"`
	MaxCalls         int `json:"maxCalls"`
	MaxParallelCalls int `json:"maxParallelCalls"`
	MaxContextBytes  int `json:"maxContextBytes"`
	MaxOutputBytes   int `json:"maxOutputBytes"`
}

func DefaultLimits() Limits {
	return Limits{MaxRounds: 4, MaxCalls: 8, MaxParallelCalls: 4, MaxContextBytes: 256 * 1024, MaxOutputBytes: 1024 * 1024}
}

// Request is caller-neutral. Instructions define the business-specific final
// contract; Runtime implementations own capability selection and iteration.
type Request struct {
	RequestID    string            `json:"requestId"`
	Purpose      ai.Action         `json:"purpose"`
	Instructions string            `json:"instructions"`
	Context      []ai.Message      `json:"context,omitempty"`
	Input        string            `json:"input"`
	OutputSchema json.RawMessage   `json:"outputSchema"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	Limits       Limits            `json:"limits"`
}

type Result struct {
	RequestID  string                `json:"requestId"`
	Purpose    ai.Action             `json:"purpose"`
	Output     json.RawMessage       `json:"output"`
	Evidence   []CapabilityResult    `json:"evidence,omitempty"`
	Provenance ai.ResponseProvenance `json:"-"`
	Usage      *ai.Usage             `json:"usage,omitempty"`
	Metadata   map[string]string     `json:"metadata,omitempty"`
}

// Runtime is the one high-level AI contract used by Assistant, Source
// interpretation, and future AI callers. Native and Plugin implementations
// both satisfy this exact interface.
type Runtime interface {
	Execute(context.Context, Request) (Result, error)
}
