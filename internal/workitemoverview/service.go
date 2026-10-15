// Package workitemoverview composes the WorkItem list with bounded Tracking
// and Attention summaries. It owns product projection semantics, not domain
// truth or persistence.
package workitemoverview

import (
	"context"
	"fmt"
	"time"

	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

type WorkItemReader interface {
	List(context.Context) ([]workitem.WorkItem, error)
}

type TrackingSummaryReader interface {
	ListWorkItemSummaries(context.Context) (map[string]tracking.WorkItemSummary, error)
}

type AttentionSummaryReader interface {
	ListWorkItemSummaries(context.Context) (map[string]attention.WorkItemSummary, error)
}

type TrackingTodoReader interface {
	ListOpenTrackingTodoDueTargets(context.Context) ([]planner.TrackingTodoDueTarget, error)
}

type Item struct {
	WorkItem           workitem.WorkItem
	Tracking           tracking.WorkItemSummary
	Attention          attention.WorkItemSummary
	TrackedItemSignals map[string]TrackedItemSignal
}

type SignalState string

const (
	SignalNeutral   SignalState = "NEUTRAL"
	SignalSatisfied SignalState = "SATISFIED"
	SignalWatch     SignalState = "WATCH"
	SignalProblem   SignalState = "PROBLEM"
)

// TrackedItemSignal is an Overview-only presentation projection. Its state is
// derived solely from the earliest open linked Todo due target. Monitoring,
// Attention and execution StepState remain visible facts but do not govern
// this presentation health signal.
type TrackedItemSignal struct {
	State                      SignalState
	HasActiveTrackingTodo      bool
	GoverningTodoDueAt         *time.Time
	ActionableAttentionCount   int
	HighestAttentionSeverity   *attention.Severity
	HasActiveExpectation       bool
	HasSatisfiedExpectation    bool
	HasOverdueUnmetExpectation bool
}

// View keeps section availability separate from valid empty summaries. A
// primary WorkItem read error still fails the request; either cross-domain
// summary may independently be unavailable without hiding WorkItems.
type View struct {
	Items              []Item
	TrackingAvailable  bool
	AttentionAvailable bool
}

type Service struct {
	workItems WorkItemReader
	tracking  TrackingSummaryReader
	attention AttentionSummaryReader
	planner   TrackingTodoReader
	now       func() time.Time
}

func NewService(workItems WorkItemReader, trackingReader TrackingSummaryReader, attentionReader AttentionSummaryReader, plannerReader TrackingTodoReader) *Service {
	return &Service{workItems: workItems, tracking: trackingReader, attention: attentionReader, planner: plannerReader, now: func() time.Time { return time.Now().UTC() }}
}

// SetClock replaces the presentation clock for deterministic tests.
func (s *Service) SetClock(now func() time.Time) {
	if now != nil {
		s.now = now
	}
}

func (s *Service) List(ctx context.Context) (*View, error) {
	items, err := s.workItems.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("load WorkItems: %w", err)
	}
	trackingSummaries, trackingErr := s.tracking.ListWorkItemSummaries(ctx)
	attentionSummaries, attentionErr := s.attention.ListWorkItemSummaries(ctx)
	dueTargets, dueTargetsErr := s.planner.ListOpenTrackingTodoDueTargets(ctx)
	earliestDueByTrackedItem := make(map[string]time.Time, len(dueTargets))
	if dueTargetsErr == nil {
		for _, target := range dueTargets {
			current, exists := earliestDueByTrackedItem[target.TrackedItemID]
			if !exists || target.DueAt.Before(current) {
				earliestDueByTrackedItem[target.TrackedItemID] = target.DueAt
			}
		}
	}
	now := s.now().UTC()
	view := &View{
		Items:              make([]Item, 0, len(items)),
		TrackingAvailable:  trackingErr == nil && dueTargetsErr == nil,
		AttentionAvailable: attentionErr == nil,
	}
	for _, workItem := range items {
		trackingSummary := tracking.WorkItemSummary{WorkItemID: workItem.ID, Items: []tracking.OverviewTrackedItem{}, Progressions: []tracking.TrackedItemProgression{}}
		if view.TrackingAvailable {
			if existing, ok := trackingSummaries[workItem.ID]; ok {
				trackingSummary = existing
			}
		}
		attentionSummary := attention.WorkItemSummary{WorkItemID: workItem.ID}
		if view.AttentionAvailable {
			if existing, ok := attentionSummaries[workItem.ID]; ok {
				attentionSummary = existing
			}
		}
		signals := make(map[string]TrackedItemSignal, len(trackingSummary.Items))
		for _, tracked := range trackingSummary.Items {
			itemAttention := attentionSummary.TrackedItems[tracked.ID]
			signal := TrackedItemSignal{
				State: SignalNeutral, ActionableAttentionCount: itemAttention.ActionableIssueCount,
				HighestAttentionSeverity:   itemAttention.HighestActiveSeverity,
				HasActiveExpectation:       tracked.Monitoring.HasActiveExpectation,
				HasSatisfiedExpectation:    tracked.Monitoring.HasSatisfiedExpectation,
				HasOverdueUnmetExpectation: itemAttention.HasOverdueUnmetExpectation,
			}
			if dueAt, ok := earliestDueByTrackedItem[tracked.ID]; ok {
				dueAt = dueAt.UTC()
				signal.HasActiveTrackingTodo = true
				signal.GoverningTodoDueAt = &dueAt
				switch remaining := dueAt.Sub(now); {
				case remaining <= 24*time.Hour:
					signal.State = SignalProblem
				case remaining <= 72*time.Hour:
					signal.State = SignalWatch
				default:
					signal.State = SignalSatisfied
				}
			}
			signals[tracked.ID] = signal
		}
		view.Items = append(view.Items, Item{WorkItem: workItem, Tracking: trackingSummary, Attention: attentionSummary, TrackedItemSignals: signals})
	}
	return view, nil
}
