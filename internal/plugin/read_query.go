package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Bounds for the read.query typed contract. These exist so a Plugin result
// can never grow into an unbounded prompt payload — M15 Slice 2 is
// deliberately small: one opaque query string in, one small bounded
// summary/fact list out.
const (
	MaxReadQueryRunes     = 500
	MaxReadSummaryRunes   = 2000
	MaxReadFactCount      = 20
	MaxReadFactNameRunes  = 100
	MaxReadFactValueRunes = 500
)

// ReadQueryRequest is the read.query typed input. Query is an opaque domain
// lookup string — never SQL, never an expression language, never
// interpreted by the host. Its meaning is entirely up to the receiving
// Plugin's own declared semantics (CapabilityDeclaration.InputSemantics).
type ReadQueryRequest struct {
	Query string `json:"query"`
}

// ReadFact is one small named fact in a ReadQueryResult.
type ReadFact struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// ReadQueryResult is the read.query typed result: a short summary plus a
// small, bounded list of facts. It is deliberately not an arbitrary nested
// JSON tree, an unbounded array, a stream, or a binary payload — just enough
// bounded, trustworthy content for a future caller (M16) to place in a
// prompt.
type ReadQueryResult struct {
	Summary string     `json:"summary"`
	Facts   []ReadFact `json:"facts"`
}

// ReadQuerier is the read.query typed adapter, mirroring SourceFetcher's
// serialize/invoke/decode/validate/clone shape exactly.
type ReadQuerier struct {
	invoker Invoker
}

func NewReadQuerier(invoker Invoker) *ReadQuerier {
	return &ReadQuerier{invoker: invoker}
}

func (q *ReadQuerier) Query(ctx context.Context, instance Instance, request ReadQueryRequest) (ReadQueryResult, error) {
	if q == nil || q.invoker == nil {
		return ReadQueryResult{}, fmt.Errorf("read query invoker must not be nil")
	}
	if instance.CapabilityID != CapabilityReadQuery {
		return ReadQueryResult{}, fmt.Errorf("%w: instance %q requires %q", ErrCapabilityUnsupported, instance.ID, CapabilityReadQuery)
	}
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return ReadQueryResult{}, fmt.Errorf("read.query request must have a non-empty Query")
	}
	if utf8.RuneCountInString(query) > MaxReadQueryRunes {
		return ReadQueryResult{}, fmt.Errorf("read.query request Query exceeds %d characters", MaxReadQueryRunes)
	}
	input, err := json.Marshal(ReadQueryRequest{Query: query})
	if err != nil {
		return ReadQueryResult{}, fmt.Errorf("encode read.query request: %w", err)
	}
	output, err := q.invoker.Invoke(ctx, Invocation{
		ExtensionID:  instance.ExtensionID,
		CapabilityID: CapabilityReadQuery,
		Instance:     &instance,
		Input:        input,
	})
	if err != nil {
		return ReadQueryResult{}, err
	}
	var result ReadQueryResult
	if err := json.Unmarshal(output, &result); err != nil {
		return ReadQueryResult{}, fmt.Errorf("decode read.query result for instance %q: %w", instance.ID, err)
	}
	if err := result.Validate(); err != nil {
		return ReadQueryResult{}, fmt.Errorf("validate read.query result for instance %q: %w", instance.ID, err)
	}
	return cloneReadQueryResult(result), nil
}

// Validate enforces every read.query result bound. A Plugin process is an
// untrusted boundary (Plugin Runtime Contract V1 §15): a valid decode is not
// enough, every bound must be checked before this result can be trusted as
// prompt-safe content.
func (r ReadQueryResult) Validate() error {
	if !utf8.ValidString(r.Summary) {
		return fmt.Errorf("summary must be valid UTF-8")
	}
	if utf8.RuneCountInString(r.Summary) > MaxReadSummaryRunes {
		return fmt.Errorf("summary exceeds %d characters", MaxReadSummaryRunes)
	}
	if len(r.Facts) > MaxReadFactCount {
		return fmt.Errorf("facts exceeds %d entries", MaxReadFactCount)
	}
	for index, fact := range r.Facts {
		if strings.TrimSpace(fact.Name) == "" {
			return fmt.Errorf("fact %d name must not be empty", index)
		}
		if !utf8.ValidString(fact.Name) || !utf8.ValidString(fact.Value) {
			return fmt.Errorf("fact %d must be valid UTF-8", index)
		}
		if utf8.RuneCountInString(fact.Name) > MaxReadFactNameRunes {
			return fmt.Errorf("fact %d name exceeds %d characters", index, MaxReadFactNameRunes)
		}
		if utf8.RuneCountInString(fact.Value) > MaxReadFactValueRunes {
			return fmt.Errorf("fact %d value exceeds %d characters", index, MaxReadFactValueRunes)
		}
	}
	return nil
}

func cloneReadQueryResult(result ReadQueryResult) ReadQueryResult {
	cloned := result
	cloned.Facts = append([]ReadFact(nil), result.Facts...)
	return cloned
}
