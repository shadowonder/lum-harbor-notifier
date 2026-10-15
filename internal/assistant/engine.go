package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/airuntime"
	"lum-harbor-notifier/internal/proposalcap"
	"lum-harbor-notifier/internal/readcap"
)

type ReadCapabilitySource interface {
	ListDescriptors() []readcap.Descriptor
	Execute(context.Context, readcap.Request) (readcap.Result, error)
}

type ReadEvidence struct {
	CapabilityID string          `json:"capability_id"`
	Input        json.RawMessage `json:"input,omitempty"`
	Output       any             `json:"output"`
}

type IterationResult struct {
	Kind        ResultKind
	Text        string
	Candidate   *MutationCandidate
	Plan        *MutationPlan
	Base        *WorkItemSnapshot
	NoChange    bool
	ReadRequest *readcap.Request
	ReadResult  *readcap.Result
	Evidence    []ReadEvidence
	Provenance  ai.ResponseProvenance
}

// RuntimeAdapter owns only Assistant input/context shaping and typed final
// decoding. The high-level Runtime owns all reasoning and capability rounds.
type RuntimeAdapter struct {
	runtime       airuntime.Runtime
	proposeSource proposalcap.Source
}

func NewRuntimeAdapter(runtime airuntime.Runtime, proposeSource proposalcap.Source) (*RuntimeAdapter, error) {
	if runtime == nil || proposeSource == nil {
		return nil, errors.New("assistant runtime adapter requires AI runtime and proposal descriptors")
	}
	return &RuntimeAdapter{runtime: runtime, proposeSource: proposeSource}, nil
}

// NewNativeRuntimeAdapter is the convenience composition used by tests and
// embedded callers that supply Core's lower-level model executor directly.
func NewNativeRuntimeAdapter(model Executor, reads ReadCapabilitySource, proposes proposalcap.Source) (*RuntimeAdapter, error) {
	host, err := airuntime.NewCapabilityHost(reads, proposes)
	if err != nil {
		return nil, err
	}
	runtime, err := airuntime.NewNative(model, host)
	if err != nil {
		return nil, err
	}
	return NewRuntimeAdapter(runtime, proposes)
}

func (e *RuntimeAdapter) Iterate(ctx context.Context, requestID, message string, base *WorkItemSnapshot) (IterationResult, error) {
	return e.IterateWithContext(ctx, requestID, message, ApplicationContext{}, base)
}

func (e *RuntimeAdapter) IterateWithContext(ctx context.Context, requestID, message string, appCtx ApplicationContext, base *WorkItemSnapshot) (IterationResult, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return IterationResult{}, ErrEmptyMessage
	}
	if utf8.RuneCountInString(message) > maxMessageRunes {
		return IterationResult{}, ErrMessageTooLong
	}
	story := newAssistantExecutionStory(ctx, requestID, message)
	result, err := e.runtime.Execute(ctx, airuntime.Request{
		RequestID:    requestID,
		Purpose:      ai.ActionAssistantAsk,
		Instructions: prompt2SystemPrompt + "\n\n" + CapabilityDecisionGuidance,
		Context:      assistantContextMessages(appCtx, base),
		Input:        message,
		OutputSchema: append(json.RawMessage(nil), prompt2Schema...),
		Metadata:     map[string]string{"caller": "assistant"},
		Limits:       airuntime.Limits{MaxRounds: 3, MaxCalls: 2, MaxParallelCalls: 2, MaxContextBytes: 256 * 1024, MaxOutputBytes: 1024 * 1024},
	})
	if err != nil {
		story.addStep("runtime -> FAILED: " + err.Error())
		story.finish(IterationResult{}, err)
		return IterationResult{}, fmt.Errorf("%w: %v", ErrRuntime, err)
	}
	decoded, err := DecodePrompt2Result(result.Output)
	if err != nil {
		story.addStep("runtime -> INVALID_RESPONSE: " + err.Error())
		story.finish(IterationResult{}, err)
		return IterationResult{}, err
	}
	descriptors := e.proposeSource.ListDescriptors()
	if decoded.Kind == KindMutationCandidate && decoded.Candidate != nil && !candidatePermitted(descriptors, base, *decoded.Candidate) {
		decoded.Kind, decoded.Candidate = KindUnsupported, nil
	}
	if decoded.Kind == KindMutationCandidate && decoded.Plan != nil {
		for _, operation := range decoded.Plan.Operations {
			if !candidatePermitted(descriptors, base, operation) {
				decoded.Kind, decoded.Plan = KindUnsupported, nil
				break
			}
		}
	}
	decoded.Base = base
	decoded.Provenance = result.Provenance
	for _, item := range result.Evidence {
		output := item.Value
		if output == nil && len(item.Output) > 0 {
			_ = json.Unmarshal(item.Output, &output)
		}
		evidence := ReadEvidence{CapabilityID: item.Call.ID, Input: append(json.RawMessage(nil), item.Call.Arguments...), Output: output}
		decoded.Evidence = append(decoded.Evidence, evidence)
		if decoded.ReadRequest == nil {
			req := readcap.Request{CapabilityID: item.Call.ID, Input: append(json.RawMessage(nil), item.Call.Arguments...)}
			res := readcap.Result{CapabilityID: item.Call.ID, Output: output}
			decoded.ReadRequest, decoded.ReadResult = &req, &res
		}
	}
	summary, _ := summarizeIterationResult(decoded)
	story.addStep("runtime -> " + summary)
	story.finish(decoded, nil)
	return decoded, nil
}

func assistantContextMessages(appCtx ApplicationContext, item *WorkItemSnapshot) []ai.Message {
	authContext := appCtx.FormatAuthoritativeContext(item)
	if item != nil {
		envelope := contextEnvelope{Surface: appCtx.Surface, Tracking: item.Tracking, WorkItem: workItemContext{
			ID: item.ID, Title: item.Title, Description: item.Description, Lifecycle: item.Lifecycle,
			CreatedAt: item.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: item.UpdatedAt.UTC().Format(time.RFC3339),
		}}
		if item.CompletedAt != nil {
			value := item.CompletedAt.UTC().Format(time.RFC3339)
			envelope.WorkItem.CompletedAt = &value
		}
		encoded, _ := json.MarshalIndent(envelope, "", "  ")
		authContext += "\nAuthoritative selected WorkItem context:\n" + string(encoded)
	}
	return []ai.Message{{Role: ai.RoleSystem, Content: authContext}}
}

func candidatePermitted(descriptors []proposalcap.Descriptor, base *WorkItemSnapshot, candidate MutationCandidate) bool {
	for _, descriptor := range descriptors {
		if descriptor.ID == candidate.CapabilityID {
			return base != nil || !descriptor.RequiresWorkItemContext
		}
	}
	return false
}
