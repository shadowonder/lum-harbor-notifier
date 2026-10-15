// Package trackingreevaluation owns the narrow durable handoff from a
// tracking-linked Todo completion to asynchronous reevaluation.
package trackingreevaluation

import "time"

type Status string

const (
	StatusPending    Status = "PENDING"
	StatusProcessing Status = "PROCESSING"
	StatusCompleted  Status = "COMPLETED"
	StatusFailed     Status = "FAILED"
)

type Request struct {
	ID                   string
	PlannerItemID        string
	CompletionRevisionID string
	TrackedItemID        string
	Status               Status
	AttemptCount         int64
	StartedAt            *time.Time
	CompletedAt          *time.Time
	LastError            *string
	ProposalID           *string
	AIRunID              *string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type EvidenceRevision struct {
	SourceRevisionID string
	CurrentHead      bool
}

type HistoryFact struct {
	EventType  string `json:"eventType"`
	Summary    string `json:"summary"`
	OccurredAt string `json:"occurredAt"`
}
