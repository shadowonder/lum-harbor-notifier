package sourceprocessing

import "encoding/json"

const (
	CleanOutputVersionV1     = "source.clean.v1"
	InterpretOutputVersionV1 = "source.interpret.v1"
)

type ProcessRequest struct {
	SourceRevisionID      string
	InterpretationContext json.RawMessage
}

type CleanOutputV1 struct {
	Version            string          `json:"version"`
	NormalizedDocument json.RawMessage `json:"normalizedDocument"`
}

type InterpretOutputV1 struct {
	Version                string          `json:"version"`
	Summary                string          `json:"summary"`
	InterpretationDocument json.RawMessage `json:"interpretationDocument"`
}

type ProcessingResult struct {
	ProcessingID     string
	SourceRevisionID string
	CleanRunID       string
	InterpretRunID   string
	CleanResult      CleanOutputV1
	InterpretResult  InterpretOutputV1
	ReadEvidence     []map[string]any
}
