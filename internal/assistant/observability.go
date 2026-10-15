package assistant

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"lum-harbor-notifier/internal/ai"
)

const (
	maxInfoInputRunes  = 240
	maxInfoStepRunes   = 360
	maxInfoResultRunes = 320
	maxDebugCallRunes  = 1800
	maxDebugCallsRunes = 5600
)

// assistantExecutionStory is a scoped formatter for one RuntimeAdapter run.
// It is not a tracing system: all state is local to one call and produces at
// most one INFO/WARN record plus one DEBUG record.
type assistantExecutionStory struct {
	ctx       context.Context
	requestID string
	input     string
	steps     []string
	states    []string
	calls     []string
	providers []string
}

func newAssistantExecutionStory(ctx context.Context, requestID, input string) *assistantExecutionStory {
	return &assistantExecutionStory{
		ctx:       ctx,
		requestID: requestID,
		input:     safeLogPreview(input, maxInfoInputRunes),
		states:    []string{"STARTED"},
	}
}

func (s *assistantExecutionStory) addState(state string) {
	if len(s.states) == 0 || s.states[len(s.states)-1] != state {
		s.states = append(s.states, state)
	}
}

func (s *assistantExecutionStory) addStep(step string) {
	s.steps = append(s.steps, safeLogPreview(step, maxInfoStepRunes))
}

func (s *assistantExecutionStory) recordCall(request ai.Request, response ai.Response, err error) {
	s.observeProvider(response)
	if !slog.Default().Enabled(s.ctx, slog.LevelDebug) {
		return
	}
	detail := fmt.Sprintf("%s request={%s}", request.Action, summarizeMessages(request.Messages))
	if err != nil {
		detail += " error={" + safeLogPreview(err.Error(), 700) + "}"
	} else {
		detail += " response={" + safeLogPreview(string(response.Output), 900) + "}"
		if response.Usage != nil {
			detail += fmt.Sprintf(" usage={input:%d output:%d total:%d}", response.Usage.InputTokens, response.Usage.OutputTokens, response.Usage.TotalTokens)
		}
	}
	s.calls = append(s.calls, safeLogPreview(detail, maxDebugCallRunes))
}

func (s *assistantExecutionStory) observeProvider(response ai.Response) {
	provider := ""
	if response.Provenance.ActualProvider != nil {
		provider = response.Provenance.ActualProvider.ID
	} else if response.Provenance.Origin.ID != "" {
		provider = response.Provenance.Origin.ID
	}
	if provider == "" {
		return
	}
	for _, existing := range s.providers {
		if existing == provider {
			return
		}
	}
	s.providers = append(s.providers, provider)
}

func (s *assistantExecutionStory) finish(result IterationResult, err error) {
	resultSummary := ""
	destination := "none"
	level := slog.LevelInfo
	if err != nil {
		s.addState("FAILED")
		resultSummary = "FAILED: " + safeLogPreview(err.Error(), maxInfoResultRunes)
		level = slog.LevelWarn
	} else {
		resultSummary, destination = summarizeIterationResult(result)
		s.addState(stateForResult(result))
		s.addState("SUCCEEDED")
	}

	attrs := []slog.Attr{
		slog.String("request_id", s.requestID),
		slog.String("action", "assistant"),
		slog.String("provider", strings.Join(s.providers, ",")),
		slog.String("input", s.input),
		slog.String("steps", numberedSteps(s.steps)),
		slog.String("state", strings.Join(s.states, " -> ")),
		slog.String("result", resultSummary),
		slog.String("destination", destination),
	}
	slog.Default().LogAttrs(s.ctx, level, "AI execution", attrs...)

	if slog.Default().Enabled(s.ctx, slog.LevelDebug) {
		slog.Default().LogAttrs(s.ctx, slog.LevelDebug, "AI execution detail",
			slog.String("request_id", s.requestID),
			slog.String("calls", safeLogPreview(strings.Join(s.calls, " || "), maxDebugCallsRunes)),
		)
	}
}

func summarizeIterationResult(result IterationResult) (summary, destination string) {
	switch result.Kind {
	case KindMutationCandidate:
		capability := "unknown"
		if result.Candidate != nil {
			capability = result.Candidate.CapabilityID
		}
		if result.Plan != nil {
			ids := make([]string, 0, len(result.Plan.Operations))
			for _, operation := range result.Plan.Operations {
				ids = append(ids, operation.CapabilityID)
			}
			capability = fmt.Sprintf("plan(%d): %s", len(ids), strings.Join(ids, ", "))
		}
		return "MUTATION_CANDIDATE " + safeLogPreview(capability, 160), "proposal_dispatch"
	case KindAnswer:
		return "ANSWER: " + safeLogPreview(result.Text, maxInfoResultRunes), "assistant_response"
	case KindNeedsContext:
		return "NEEDS_CONTEXT: " + safeLogPreview(result.Text, maxInfoResultRunes), "assistant_response"
	case KindUnsupported:
		return "UNSUPPORTED: " + safeLogPreview(result.Text, maxInfoResultRunes), "assistant_response"
	default:
		return safeLogPreview(string(result.Kind), maxInfoResultRunes), "assistant_response"
	}
}

func stateForResult(result IterationResult) string {
	switch result.Kind {
	case KindMutationCandidate:
		return "PROPOSAL_SELECTED"
	case KindAnswer:
		return "ANSWERED"
	case KindNeedsContext:
		return "NEEDS_CONTEXT"
	case KindUnsupported:
		return "UNSUPPORTED"
	default:
		return "RESULT_PARSED"
	}
}

func numberedSteps(steps []string) string {
	if len(steps) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(steps))
	for index, step := range steps {
		parts = append(parts, fmt.Sprintf("%d. %s", index+1, step))
	}
	return strings.Join(parts, " | ")
}

func summarizeMessages(messages []ai.Message) string {
	parts := make([]string, 0, len(messages))
	for _, message := range messages {
		parts = append(parts, fmt.Sprintf("%s=%s", message.Role, safeLogPreview(message.Content, 700)))
	}
	return safeLogPreview(strings.Join(parts, "; "), 1400)
}

func safeLogPreview(value string, maxRunes int) string {
	return ai.SafeLogText(value, maxRunes)
}
