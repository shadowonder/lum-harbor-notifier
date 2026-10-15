// Package ailineage persists durable, provider-neutral AI execution lineage.
// It is deliberately separate from the storage-free AI runtime core.
package ailineage

import (
	"encoding/json"
	"time"

	"lum-harbor-notifier/internal/ai"
)

type Status string

const (
	StatusRunning   Status = "RUNNING"
	StatusSucceeded Status = "SUCCEEDED"
	StatusFailed    Status = "FAILED"
)

const SourceRolePrimary = "PRIMARY_SOURCE"

type Run struct {
	ID               string
	ProcessingID     string
	RequestID        string
	Action           ai.Action
	Status           Status
	CanonicalRequest json.RawMessage
	Output           json.RawMessage
	ErrorText        *string
	Provenance       *ai.ResponseProvenance
	UpstreamRunID    *string
	StartedAt        time.Time
	CompletedAt      *time.Time
}

type StartRun struct {
	ID               string
	ProcessingID     string
	RequestID        string
	Action           ai.Action
	CanonicalRequest json.RawMessage
	SourceRevisionID string
	UpstreamRunID    *string
	StartedAt        time.Time
}
