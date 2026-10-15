package readcap

import (
	"context"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/history"
)

// HistoryReadForWorkItemInput is the validated input for CapabilityHistoryReadForWorkItem.
type HistoryReadForWorkItemInput struct {
	WorkItemID string `json:"workItemId"`
}

// HistoryEventSummary is the bounded projection of one History event.
type HistoryEventSummary struct {
	ID         string            `json:"id"`
	Type       history.EventType `json:"type"`
	Summary    string            `json:"summary"`
	Actor      history.ActorType `json:"actor"`
	OccurredAt time.Time         `json:"occurredAt"`
}

// HistoryReadForWorkItemOutput is the result of CapabilityHistoryReadForWorkItem.
type HistoryReadForWorkItemOutput struct {
	WorkItemID string                `json:"workItemId"`
	Events     []HistoryEventSummary `json:"events"`
	Truncated  bool                  `json:"truncated"`
}

const maxHistoryReadEvents = 10

func (s *Service) historyReadForWorkItem(ctx context.Context, input HistoryReadForWorkItemInput) (HistoryReadForWorkItemOutput, error) {
	workItemID := strings.TrimSpace(input.WorkItemID)
	if workItemID == "" {
		return HistoryReadForWorkItemOutput{}, fmt.Errorf("%w: %s requires a non-empty workItemId", ErrInvalidInput, CapabilityHistoryReadForWorkItem)
	}
	if s.history == nil {
		return HistoryReadForWorkItemOutput{}, fmt.Errorf("%s: history service is not configured", CapabilityHistoryReadForWorkItem)
	}

	page, err := s.history.QueryWorkItem(ctx, workItemID)
	if err != nil {
		return HistoryReadForWorkItemOutput{}, fmt.Errorf("%s: %w", CapabilityHistoryReadForWorkItem, err)
	}

	events := page.Events
	truncated := page.Truncated
	if len(events) > maxHistoryReadEvents {
		events = events[:maxHistoryReadEvents]
		truncated = true
	}

	summaries := make([]HistoryEventSummary, 0, len(events))
	for _, e := range events {
		summaries = append(summaries, HistoryEventSummary{
			ID:         e.ID,
			Type:       e.Type,
			Summary:    e.Summary,
			Actor:      e.Actor,
			OccurredAt: e.OccurredAt,
		})
	}

	return HistoryReadForWorkItemOutput{
		WorkItemID: workItemID,
		Events:     summaries,
		Truncated:  truncated,
	}, nil
}
