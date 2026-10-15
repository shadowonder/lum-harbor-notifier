package assistant

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	// CandidateVersion is the candidate output contract version.
	CandidateVersion = 1

	maxCandidateReasonRunes = 500
	maxPlanTitleRunes       = 200
	maxPlanTextRunes        = 1000
)

// MutationCandidate is the generic, validated AI selection of one backend
// Proposal capability. It carries no target identity, no preconditions, and
// no domain vocabulary: CapabilityID and Arguments are opaque to this
// package. Backend-owned descriptor lookup, argument validation, and Proposal
// creation belong to the application layer, never here.
type MutationCandidate struct {
	CapabilityID string
	Arguments    json.RawMessage
	Reason       *string
}

type rawCandidate struct {
	Version      *int            `json:"version"`
	CapabilityID *string         `json:"capability_id"`
	Arguments    json.RawMessage `json:"arguments"`
	Reason       *string         `json:"reason"`
}

// DecodeCandidate strictly validates and normalizes the generic candidate
// envelope. Every failure wraps ErrMalformedResult. It validates only shape
// (version, non-empty capability_id, arguments is a JSON object) - never any
// particular capability's argument contract, which is backend-owned and
// checked later by the application layer against the exact descriptor the
// model was shown.
func DecodeCandidate(output json.RawMessage) (MutationCandidate, error) {
	fail := func(format string, args ...any) (MutationCandidate, error) {
		return MutationCandidate{}, fmt.Errorf("%w: candidate: %s", ErrMalformedResult, fmt.Sprintf(format, args...))
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	var raw rawCandidate
	if err := decoder.Decode(&raw); err != nil {
		return fail("%v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fail("trailing data")
	}
	if raw.Version == nil || *raw.Version != CandidateVersion {
		return fail("unsupported or missing version")
	}
	if raw.CapabilityID == nil || strings.TrimSpace(*raw.CapabilityID) == "" {
		return fail("missing required capability_id")
	}
	capabilityID := strings.TrimSpace(*raw.CapabilityID)

	args, problem := normalizeCandidateArguments(raw.Arguments)
	if problem != "" {
		return fail("%s", problem)
	}
	reason, ok := optionalBoundedText(raw.Reason, maxCandidateReasonRunes)
	if !ok {
		return fail("reason is too long")
	}
	return MutationCandidate{CapabilityID: capabilityID, Arguments: args, Reason: reason}, nil
}

// normalizeCandidateArguments accepts arguments as a JSON object or as a JSON
// string containing one, and returns the object; a non-empty problem describes
// why it is not one.
func normalizeCandidateArguments(raw json.RawMessage) (json.RawMessage, string) {
	if len(raw) == 0 {
		return json.RawMessage("{}"), ""
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var unquoted string
		if err := json.Unmarshal(trimmed, &unquoted); err != nil {
			return nil, fmt.Sprintf("arguments string is not valid JSON string: %v", err)
		}
		trimmed = bytes.TrimSpace([]byte(unquoted))
	}
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, "arguments must be a JSON object"
	}
	var parsedObj map[string]any
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	if err := dec.Decode(&parsedObj); err == nil {
		if cleanJSON, errMarshal := json.Marshal(parsedObj); errMarshal == nil {
			trimmed = cleanJSON
		}
	}
	return trimmed, ""
}

func optionalBoundedText(value *string, maxRunes int) (*string, bool) {
	if value == nil {
		return nil, true
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, true
	}
	if utf8.RuneCountInString(trimmed) > maxRunes {
		return nil, false
	}
	return &trimmed, true
}

// MutationPlan is the generic, validated AI selection of one reviewable
// Proposal made of ordered capability operations, for a request that needs
// more than one change. Like MutationCandidate it is opaque to this package:
// the application prepares and preflights it through the capability registry
// before anything is persisted. Operation count limits, references, and
// arguments are validated there, never here.
type MutationPlan struct {
	Title      string
	Summary    *string
	Reason     *string
	Operations []MutationCandidate
}

type rawPlan struct {
	Title      *string            `json:"title"`
	Summary    *string            `json:"summary"`
	Reason     *string            `json:"reason"`
	Operations []rawPlanOperation `json:"operations"`
}

type rawPlanOperation struct {
	SequenceNo   *int            `json:"sequence_no"`
	CapabilityID *string         `json:"capability_id"`
	Arguments    json.RawMessage `json:"arguments"`
	Reason       *string         `json:"reason"`
}

// DecodePlan strictly validates the generic plan envelope: a title, at least
// one operation, operations numbered 0..n-1 in order, and each operation's
// capability_id and arguments object. Every failure wraps ErrMalformedResult.
func DecodePlan(output json.RawMessage) (MutationPlan, error) {
	fail := func(format string, args ...any) (MutationPlan, error) {
		return MutationPlan{}, fmt.Errorf("%w: plan: %s", ErrMalformedResult, fmt.Sprintf(format, args...))
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	decoder.DisallowUnknownFields()
	var raw rawPlan
	if err := decoder.Decode(&raw); err != nil {
		return fail("%v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fail("trailing data")
	}
	if raw.Title == nil || strings.TrimSpace(*raw.Title) == "" {
		return fail("missing required title")
	}
	title := strings.TrimSpace(*raw.Title)
	if utf8.RuneCountInString(title) > maxPlanTitleRunes {
		return fail("title is too long")
	}
	summary, ok := optionalBoundedText(raw.Summary, maxPlanTextRunes)
	if !ok {
		return fail("summary is too long")
	}
	reason, ok := optionalBoundedText(raw.Reason, maxPlanTextRunes)
	if !ok {
		return fail("reason is too long")
	}
	if len(raw.Operations) == 0 {
		return fail("operations must not be empty")
	}
	operations := make([]MutationCandidate, 0, len(raw.Operations))
	for index, op := range raw.Operations {
		if op.SequenceNo == nil || *op.SequenceNo != index {
			return fail("operation %d: sequence_no must be %d", index, index)
		}
		if op.CapabilityID == nil || strings.TrimSpace(*op.CapabilityID) == "" {
			return fail("operation %d: missing required capability_id", index)
		}
		args, problem := normalizeCandidateArguments(op.Arguments)
		if problem != "" {
			return fail("operation %d: %s", index, problem)
		}
		opReason, ok := optionalBoundedText(op.Reason, maxCandidateReasonRunes)
		if !ok {
			return fail("operation %d: reason is too long", index)
		}
		operations = append(operations, MutationCandidate{CapabilityID: strings.TrimSpace(*op.CapabilityID), Arguments: args, Reason: opReason})
	}
	return MutationPlan{Title: title, Summary: summary, Reason: reason, Operations: operations}, nil
}
