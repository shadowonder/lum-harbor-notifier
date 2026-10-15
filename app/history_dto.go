package app

type HistoryQueryRequestDTO struct {
	Days   int    `json:"days"`
	Scope  string `json:"scope"`
	Search string `json:"search"`
}

type HistoryEventDTO struct {
	ID               string         `json:"id"`
	EventType        string         `json:"eventType"`
	Scope            string         `json:"scope"`
	Actor            string         `json:"actor"`
	Summary          string         `json:"summary"`
	OccurredAt       string         `json:"occurredAt"`
	RecordedAt       string         `json:"recordedAt"`
	CorrelationID    string         `json:"correlationId"`
	CausationEventID *string        `json:"causationEventId"`
	SubjectEntityID  *string        `json:"subjectEntityId"`
	WorkItemID       *string        `json:"workItemId"`
	RevisionID       *string        `json:"revisionId"`
	ProposalID       *string        `json:"proposalId"`
	AIRunID          *string        `json:"aiRunId"`
	SourceRevisionID *string        `json:"sourceRevisionId"`
	Before           map[string]any `json:"before"`
	After            map[string]any `json:"after"`
}

type HistoryEventPageDTO struct {
	AsOf      string            `json:"asOf"`
	Events    []HistoryEventDTO `json:"events"`
	Truncated bool              `json:"truncated"`
}

type HistoryTraceSummaryDTO struct {
	CorrelationID         string   `json:"correlationId"`
	FirstOccurredAt       string   `json:"firstOccurredAt"`
	LastOccurredAt        string   `json:"lastOccurredAt"`
	RepresentativeSummary string   `json:"representativeSummary"`
	RepresentativeScope   string   `json:"representativeScope"`
	EventCount            int      `json:"eventCount"`
	WorkItemID            *string  `json:"workItemId"`
	Scopes                []string `json:"scopes"`
}

type HistoryTraceListDTO struct {
	Traces    []HistoryTraceSummaryDTO `json:"traces"`
	Truncated bool                     `json:"truncated"`
}

type HistoryTraceDTO struct {
	CorrelationID string            `json:"correlationId"`
	Events        []HistoryEventDTO `json:"events"`
	Truncated     bool              `json:"truncated"`
}

type HistoryCausalChainDTO struct {
	TargetEventID string            `json:"targetEventId"`
	TargetEvent   *HistoryEventDTO  `json:"targetEvent,omitempty"`
	RootEvent     *HistoryEventDTO  `json:"rootEvent,omitempty"`
	Events        []HistoryEventDTO `json:"events"`
	// RootSource is the originating Source revision the chain explains back
	// to, including its captured payload (for example the user's prompt).
	RootSource *HistoryCausalSourceDTO `json:"rootSource,omitempty"`
}

type HistoryCausalSourceDTO struct {
	SourceRevisionID string `json:"sourceRevisionId"`
	ExtensionID      string `json:"extensionId"`
	InstanceID       string `json:"instanceId"`
	SourceKind       string `json:"sourceKind"`
	PayloadJSON      string `json:"payloadJson"`
}
