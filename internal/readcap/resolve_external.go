package readcap

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"lum-harbor-notifier/internal/foundation"
)

// ErrAmbiguousExternalID is returned when an external ID lookup without a system
// matches multiple rows in external_refs across different systems.
var ErrAmbiguousExternalID = errors.New("readcap: ambiguous external id across multiple systems")

// ResolveExternalInput is the validated input for CapabilityResolveExternal.
type ResolveExternalInput struct {
	// System and ExternalID together are the exact (system, external_id)
	// identity external_refs enforces uniqueness on. System is optional;
	// if omitted, exactly one unique match is required. ExternalID is required,
	// non-empty after trimming. Wire keys: "system", "externalId".
	System     string `json:"system,omitempty"`
	ExternalID string `json:"externalId"`
}

// ResolveExternalOutput is the result of CapabilityResolveExternal.
type ResolveExternalOutput struct {
	EntityID string
	// EntityType is the resolved Entity's stable kind (e.g. "PLANNER_ITEM",
	// "WORK_ITEM"), read from the same entities row the identity resolves
	// to.
	EntityType string
}

func (s *Service) resolveExternal(ctx context.Context, input ResolveExternalInput) (ResolveExternalOutput, error) {
	system := strings.TrimSpace(input.System)
	externalID := strings.TrimSpace(input.ExternalID)
	if externalID == "" {
		return ResolveExternalOutput{}, fmt.Errorf("%w: %s requires a non-empty externalId", ErrInvalidInput, CapabilityResolveExternal)
	}

	if system != "" {
		ref, err := s.identity.GetExternalRefByIdentity(ctx, system, externalID)
		if err != nil {
			if errors.Is(err, foundation.ErrExternalRefNotFound) {
				return ResolveExternalOutput{}, fmt.Errorf("%w: external identity %s/%s", ErrEntityNotFound, system, externalID)
			}
			return ResolveExternalOutput{}, fmt.Errorf("%s: %w", CapabilityResolveExternal, err)
		}

		entityType, err := s.identity.GetEntityType(ctx, ref.EntityID)
		if err != nil {
			if errors.Is(err, foundation.ErrEntityNotFound) {
				return ResolveExternalOutput{}, fmt.Errorf("%w: external identity %s/%s", ErrEntityNotFound, system, externalID)
			}
			return ResolveExternalOutput{}, fmt.Errorf("%s: %w", CapabilityResolveExternal, err)
		}

		return ResolveExternalOutput{EntityID: ref.EntityID, EntityType: entityType}, nil
	}

	// Outcome B: system is omitted. Authoritative unique lookup by external_id via Foundation repository.
	ref, err := s.identity.GetExternalRefByExternalID(ctx, externalID)
	if err != nil {
		if errors.Is(err, foundation.ErrExternalRefNotFound) {
			return ResolveExternalOutput{}, fmt.Errorf("%w: external id %q", ErrEntityNotFound, externalID)
		}
		if errors.Is(err, foundation.ErrAmbiguousExternalRef) {
			return ResolveExternalOutput{}, fmt.Errorf("%w: external id %q", ErrAmbiguousExternalID, externalID)
		}
		return ResolveExternalOutput{}, fmt.Errorf("%s: %w", CapabilityResolveExternal, err)
	}

	entityType, err := s.identity.GetEntityType(ctx, ref.EntityID)
	if err != nil {
		if errors.Is(err, foundation.ErrEntityNotFound) {
			return ResolveExternalOutput{}, fmt.Errorf("%w: external id %q", ErrEntityNotFound, externalID)
		}
		return ResolveExternalOutput{}, fmt.Errorf("%s: %w", CapabilityResolveExternal, err)
	}

	return ResolveExternalOutput{EntityID: ref.EntityID, EntityType: entityType}, nil
}
