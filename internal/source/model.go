// Package source owns durable external-observation identity, immutable
// revisions, change detection, and per-instance source checkpoints.
package source

import (
	"encoding/json"
	"time"
)

type Observation struct {
	ExternalID      string
	SourceKind      string
	ObservedAt      time.Time
	SourceUpdatedAt *time.Time
	Payload         json.RawMessage
}

type FetchBatch struct {
	ExtensionID  string
	InstanceID   string
	CapabilityID string
	Checkpoint   FetchCheckpoint
	Records      []Observation
	NextCursor   *string
}

// FetchCheckpoint binds one remote fetch to the exact durable checkpoint
// generation from which it started. It is Source-owned correctness state and
// is never serialized to the plugin.
type FetchCheckpoint struct {
	ExtensionID  string
	InstanceID   string
	CapabilityID string
	Exists       bool
	Version      int64
	Cursor       *string
}

type SourceRecord struct {
	ID                 string
	ExtensionID        string
	InstanceID         string
	SourceKind         string
	ExternalID         string
	CurrentRevisionID  string
	CurrentContentHash string
	SourceUpdatedAt    *time.Time
	FirstSeenAt        time.Time
	LastSeenAt         time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type SourceRevision struct {
	RevisionNo      int64
	ID              string
	SourceRecordID  string
	ContentHash     string
	Payload         json.RawMessage
	SourceUpdatedAt *time.Time
	ObservedAt      time.Time
	CreatedAt       time.Time
}

type SyncState struct {
	ExtensionID       string
	InstanceID        string
	CapabilityID      string
	Cursor            *string
	CheckpointVersion int64
	LastSuccessAt     time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ChangeType string

const (
	ChangeCreated ChangeType = "CREATED"
	ChangeUpdated ChangeType = "UPDATED"
)

type Change struct {
	Type       ChangeType
	RecordID   string
	RevisionID string
}

type IngestionResult struct {
	ExtensionID             string
	InstanceID              string
	CapabilityID            string
	Changes                 []Change
	NewlyChangedRevisionIDs []string
	CreatedCount            int
	UpdatedCount            int
	UnchangedCount          int
	StaleCount              int
	CursorUpdated           bool
	Cursor                  *string
	CheckpointVersion       int64
}

// CurrentSourceHead is one bounded current-evidence fact: a SourceRecord's
// stable identity plus the exact content of its current SourceRevision, as of
// this read. It is a read-only projection for downstream callers (a future
// deterministic reconciler) and creates no new persisted state.
//
// It never carries Planner, WorkItem, or other domain fields: Source Layer
// exposes evidence, not domain truth.
type CurrentSourceHead struct {
	RecordID           string
	ExtensionID        string
	InstanceID         string
	SourceKind         string
	ExternalID         string
	CurrentRevisionID  string
	CurrentContentHash string
	Payload            json.RawMessage
	SourceUpdatedAt    *time.Time
	ObservedAt         time.Time
	LastSeenAt         time.Time
}

type ProcessingStatus string

const (
	ProcessingStatusPending    ProcessingStatus = "PENDING"
	ProcessingStatusProcessing ProcessingStatus = "PROCESSING"
	ProcessingStatusCompleted  ProcessingStatus = "COMPLETED"
	ProcessingStatusFailed     ProcessingStatus = "FAILED"
)

type ProcessingMode string

const (
	ProcessingModeSemanticProposal  ProcessingMode = "SEMANTIC_PROPOSAL"
	ProcessingModeDeterministicSync ProcessingMode = "DETERMINISTIC_SYNC"
	ProcessingModeDirectMutation    ProcessingMode = "DIRECT_MUTATION"
)

type ProcessingState struct {
	SourceRevisionID string
	State            ProcessingStatus
	Mode             ProcessingMode
	StartedAt        *time.Time
	CompletedAt      *time.Time
	LastError        *string
	ProposalID       *string
	RevisionID       *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type PendingSemanticSource struct {
	SourceRevisionID string
	State            ProcessingStatus
	Mode             ProcessingMode
	StartedAt        *time.Time
	CompletedAt      *time.Time
	LastError        *string
	ProposalID       *string
	RevisionID       *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Payload          json.RawMessage
	ContentHash      string
	ExtensionID      string
	InstanceID       string
	SourceKind       string
	ExternalID       string
}
