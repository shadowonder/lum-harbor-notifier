package readcap

import (
	"context"
	"fmt"
	"time"
)

const (
	defaultWorkItemListLimit = 20
	maxWorkItemListLimit     = 50
)

// WorkItemListInput specifies optional bounding parameters for listing WorkItems.
type WorkItemListInput struct {
	Limit *int `json:"limit,omitempty"`
}

// WorkItemSummaryFacts contains core summary facts for one WorkItem in a list.
type WorkItemSummaryFacts struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Lifecycle string    `json:"lifecycle"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WorkItemListOutput is the result of CapabilityWorkItemList.
type WorkItemListOutput struct {
	Items []WorkItemSummaryFacts `json:"items"`
	Total int                    `json:"total"`
}

func (s *Service) listWorkItems(ctx context.Context, input WorkItemListInput) (WorkItemListOutput, error) {
	limit := defaultWorkItemListLimit
	if input.Limit != nil {
		if *input.Limit <= 0 {
			return WorkItemListOutput{}, fmt.Errorf("%w: limit must be positive", ErrInvalidInput)
		}
		if *input.Limit > maxWorkItemListLimit {
			limit = maxWorkItemListLimit
		} else {
			limit = *input.Limit
		}
	}

	items, err := s.workItems.List(ctx)
	if err != nil {
		return WorkItemListOutput{}, fmt.Errorf("%s: %w", CapabilityWorkItemList, err)
	}

	total := len(items)
	if len(items) > limit {
		items = items[:limit]
	}

	facts := make([]WorkItemSummaryFacts, 0, len(items))
	for _, item := range items {
		facts = append(facts, WorkItemSummaryFacts{
			ID:        item.ID,
			Title:     item.Title,
			Lifecycle: string(item.Lifecycle),
			CreatedAt: item.CreatedAt,
			UpdatedAt: item.UpdatedAt,
		})
	}

	return WorkItemListOutput{
		Items: facts,
		Total: total,
	}, nil
}
