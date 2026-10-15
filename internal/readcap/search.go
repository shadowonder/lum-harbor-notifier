package readcap

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/workitem"
)

const (
	// defaultSearchLimit applies when Limit is zero or negative.
	defaultSearchLimit = 10
	// maxSearchLimit is the hard cap no caller can exceed, regardless of
	// what Limit it requests.
	maxSearchLimit = 20
	// maxSearchQueryLen bounds Query so a pathological input cannot drive an
	// unbounded scan cost even before it reaches SQL.
	maxSearchQueryLen = 200
)

// entityKindWorkItem and entityKindPlannerItem are the two entity kinds
// entity.search covers in Slice 1 — the same entity_type values
// internal/workitem and internal/planner already write to entities.
const (
	entityKindWorkItem    = "WORK_ITEM"
	entityKindPlannerItem = "PLANNER_ITEM"
)

// SearchInput is the validated input for CapabilitySearch.
type SearchInput struct {
	// Query is a literal (no wildcard) search term. Required, non-empty and
	// length-bounded after trimming. Wire key: "query".
	Query string `json:"query"`
	// Limit is an optional positive result cap. Zero/negative uses
	// defaultSearchLimit; anything above maxSearchLimit is clamped down.
	// Wire key: "limit".
	Limit int `json:"limit,omitempty"`
}

// SearchCandidate is one bounded search result. It is never a full
// aggregate — only enough to identify and label the entity.
type SearchCandidate struct {
	EntityID     string
	EntityKind   string
	DisplayLabel string
	// MatchHint is a short, human-readable reason this candidate matched
	// ("exact id" or "title match"); it is not a score or ranking.
	MatchHint string
}

// SearchOutput is the result of CapabilitySearch.
type SearchOutput struct {
	Candidates []SearchCandidate
}

func (s *Service) search(ctx context.Context, input SearchInput) (SearchOutput, error) {
	query := strings.TrimSpace(input.Query)
	if query == "" {
		return SearchOutput{}, fmt.Errorf("%w: %s requires a non-empty Query", ErrInvalidInput, CapabilitySearch)
	}
	if len(query) > maxSearchQueryLen {
		return SearchOutput{}, fmt.Errorf("%w: %s query exceeds %d characters", ErrInvalidInput, CapabilitySearch, maxSearchQueryLen)
	}

	limit := input.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	var candidates []SearchCandidate
	seen := make(map[string]bool)

	// Precedence: an exact stable entity ID always comes first. Entity IDs
	// are drawn from the single shared `entities` table, so a query can
	// exactly match at most one of these two domains, never both.
	if item, err := s.workItems.GetByID(ctx, query); err == nil {
		candidates = append(candidates, SearchCandidate{
			EntityID: item.ID, EntityKind: entityKindWorkItem,
			DisplayLabel: item.Title, MatchHint: "exact id",
		})
		seen[item.ID] = true
	} else if !errors.Is(err, workitem.ErrNotFound) {
		return SearchOutput{}, fmt.Errorf("%s: %w", CapabilitySearch, err)
	}
	if len(candidates) == 0 {
		if item, err := s.planner.GetByID(ctx, query); err == nil {
			candidates = append(candidates, SearchCandidate{
				EntityID: item.ID, EntityKind: entityKindPlannerItem,
				DisplayLabel: item.Title, MatchHint: "exact id",
			})
			seen[item.ID] = true
		} else if !errors.Is(err, planner.ErrNotFound) {
			return SearchOutput{}, fmt.Errorf("%s: %w", CapabilitySearch, err)
		}
	}

	remaining := limit - len(candidates)
	if remaining > 0 {
		titleMatches, err := s.searchByTitle(ctx, query, remaining, seen)
		if err != nil {
			return SearchOutput{}, err
		}
		if len(titleMatches) > remaining {
			titleMatches = titleMatches[:remaining]
		}
		candidates = append(candidates, titleMatches...)
	}

	return SearchOutput{Candidates: candidates}, nil
}

// searchByTitle merges bounded, per-domain title matches into one
// deterministically ordered list, excluding anything already in seen.
// Each domain's own query is independently limited, so this never performs
// an unbounded scan even before merging.
func (s *Service) searchByTitle(ctx context.Context, query string, limit int, seen map[string]bool) ([]SearchCandidate, error) {
	workItems, err := s.workItems.SearchByTitle(ctx, query, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", CapabilitySearch, err)
	}
	plannerItems, err := s.planner.SearchByTitle(ctx, query, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", CapabilitySearch, err)
	}

	var matches []SearchCandidate
	for _, item := range workItems {
		if seen[item.ID] {
			continue
		}
		matches = append(matches, SearchCandidate{
			EntityID: item.ID, EntityKind: entityKindWorkItem,
			DisplayLabel: item.Title, MatchHint: "title match",
		})
	}
	for _, item := range plannerItems {
		if seen[item.ID] {
			continue
		}
		matches = append(matches, SearchCandidate{
			EntityID: item.ID, EntityKind: entityKindPlannerItem,
			DisplayLabel: item.Title, MatchHint: "title match",
		})
	}

	sort.Slice(matches, func(i, j int) bool {
		li, lj := strings.ToLower(matches[i].DisplayLabel), strings.ToLower(matches[j].DisplayLabel)
		if li != lj {
			return li < lj
		}
		if matches[i].EntityKind != matches[j].EntityKind {
			return matches[i].EntityKind < matches[j].EntityKind
		}
		return matches[i].EntityID < matches[j].EntityID
	})

	return matches, nil
}
