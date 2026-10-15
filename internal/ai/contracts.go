// Package ai defines Lum Harbor's provider-neutral AI execution boundary.
// Business packages call Service rather than provider-specific clients.
package ai

import "encoding/json"

// Action identifies the business purpose and validation policy of an AI call.
// It is an immutable execution invariant once Service.Execute begins.
type Action string

const (
	ActionNotificationEvaluate      Action = "notification.evaluate"
	ActionSourceClean               Action = "source.clean"
	ActionSourceInterpret           Action = "source.interpret"
	ActionAssistantAsk              Action = "assistant.ask"
	ActionPlannerProposal           Action = "planner.proposal"
	ActionWorkItemAnalyze           Action = "work_item.analyze"
	ActionRuntimeVerify             Action = "runtime.verify"
	ActionRuntimeCapabilityDecision Action = "runtime.capability_decision"
)

// Role identifies the portable role of one message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is provider-neutral prompt content.
type Message struct {
	Role    Role
	Content string
}

// Request is the canonical input accepted by every AI provider and middleware.
// RequestID, Action, and OutputSchema are immutable during one execution.
type Request struct {
	RequestID    string
	Action       Action
	Messages     []Message
	OutputSchema json.RawMessage
	Metadata     map[string]string
}

// ProviderInfo identifies a provider selected or invoked by the runtime.
type ProviderInfo struct {
	ID     string
	Name   string
	Source ProviderSource
}

// ResponseOriginKind identifies who created the initial canonical response
// candidate. Post middleware transformations do not change this origin.
type ResponseOriginKind string

const (
	ResponseOriginProvider      ResponseOriginKind = "PROVIDER"
	ResponseOriginPreMiddleware ResponseOriginKind = "PRE_MIDDLEWARE"
)

// ResponseOrigin identifies the provider or pre middleware that created the
// initial canonical response candidate. It is not a transformation history.
type ResponseOrigin struct {
	Kind ResponseOriginKind
	ID   string
	Name string
}

// ResponseProvenance is runtime-owned execution provenance for one response.
// ActualProvider is non-nil exactly when ProviderInvoked is true.
type ResponseProvenance struct {
	ProviderInvoked bool
	ActualProvider  *ProviderInfo
	Origin          ResponseOrigin
}

// Usage contains optional provider-neutral usage information.
type Usage struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
	TotalTokens  int `json:"totalTokens"`
}

// Response is the canonical result returned to AI callers.
type Response struct {
	RequestID  string
	Action     Action
	Output     json.RawMessage
	Provenance ResponseProvenance
	Usage      *Usage
	Metadata   map[string]string
}

func cloneRequest(request Request) Request {
	cloned := request
	cloned.Messages = append([]Message(nil), request.Messages...)
	cloned.OutputSchema = append(json.RawMessage(nil), request.OutputSchema...)
	cloned.Metadata = cloneStringMap(request.Metadata)
	return cloned
}

func cloneResponse(response Response) Response {
	cloned := response
	cloned.Output = append(json.RawMessage(nil), response.Output...)
	cloned.Metadata = cloneStringMap(response.Metadata)
	if response.Provenance.ActualProvider != nil {
		provider := *response.Provenance.ActualProvider
		cloned.Provenance.ActualProvider = &provider
	}
	if response.Usage != nil {
		usage := *response.Usage
		cloned.Usage = &usage
	}
	return cloned
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
