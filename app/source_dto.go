package app

// LocalSourceCaptureRequestDTO is the compatibility DTO for injecting a text or note Source.
type LocalSourceCaptureRequestDTO struct {
	Title      string  `json:"title"`
	Content    string  `json:"content"`
	SourceKind *string `json:"sourceKind,omitempty"` // defaults to "NOTE" if nil or empty
	ExternalID *string `json:"externalId,omitempty"` // optional stable identifier for deduplication
}

// LocalSourceDTO is the compatibility DTO for a captured Source and its current revision.
type LocalSourceDTO struct {
	SourceRecordID   string `json:"sourceRecordId"`
	SourceRevisionID string `json:"sourceRevisionId"`
	SourceKind       string `json:"sourceKind"`
	Title            string `json:"title"`
	Content          string `json:"content"`
	ContentHash      string `json:"contentHash"`
	CreatedAt        string `json:"createdAt"`
}

// InterpretSourceRequestDTO conveys intent to interpret an immutable, durable source revision.
type InterpretSourceRequestDTO struct {
	SourceRevisionID string `json:"sourceRevisionId"`
}

// SourceInterpretationResultDTO reports the outcome of source interpretation and proposal generation.
type SourceInterpretationResultDTO struct {
	SourceRevisionID string       `json:"sourceRevisionId"`
	CleanRunID       string       `json:"cleanRunId"`
	InterpretRunID   string       `json:"interpretRunId"`
	Summary          string       `json:"summary"`
	HasProposal      bool         `json:"hasProposal"`
	Proposal         *ProposalDTO `json:"proposal,omitempty"`
	Notice           string       `json:"notice,omitempty"`
	AlreadyPending   bool         `json:"alreadyPending,omitempty"`
}
