package ai

import (
	"context"
	"fmt"
	"strings"
)

// Decision controls one middleware stage without exposing a Go next function.
type Decision string

const (
	DecisionContinue Decision = "CONTINUE"
	DecisionReplace  Decision = "REPLACE"
	DecisionStop     Decision = "STOP"
)

// PreRequestResult is the constrained result of pre-request middleware.
// Request is used only for REPLACE; Response is used only for STOP.
type PreRequestResult struct {
	Decision Decision
	Request  Request
	Response Response
}

// PostResponseResult is the constrained result of post-response middleware.
// Response is used for REPLACE and STOP and ignored for CONTINUE.
type PostResponseResult struct {
	Decision Decision
	Response Response
}

// PreRequestMiddleware may inspect a request, replace its mutable content, or
// short-circuit provider execution. Implementations must be concurrency-safe.
type PreRequestMiddleware interface {
	Process(ctx context.Context, request Request) (PreRequestResult, error)
}

// PostResponseMiddleware may filter or replace canonical response content.
// Implementations must be concurrency-safe.
type PostResponseMiddleware interface {
	Process(ctx context.Context, response Response) (PostResponseResult, error)
}

// PreMiddlewareBinding separates middleware implementation from action scope.
// Exactly one of AllActions or a non-empty Actions list must be configured.
type PreMiddlewareBinding struct {
	ID         string
	Name       string
	AllActions bool
	Actions    []Action
	Middleware PreRequestMiddleware
}

// PostMiddlewareBinding is the post-response equivalent of PreMiddlewareBinding.
type PostMiddlewareBinding struct {
	ID         string
	Name       string
	AllActions bool
	Actions    []Action
	Middleware PostResponseMiddleware
}

func validatePreBindings(bindings []PreMiddlewareBinding) ([]PreMiddlewareBinding, error) {
	cloned := make([]PreMiddlewareBinding, len(bindings))
	seen := make(map[string]struct{}, len(bindings))
	for index, binding := range bindings {
		if err := validateBinding(binding.ID, binding.AllActions, binding.Actions, binding.Middleware != nil, seen); err != nil {
			return nil, fmt.Errorf("ai pre middleware registration: %w", err)
		}
		binding.Actions = append([]Action(nil), binding.Actions...)
		cloned[index] = binding
	}
	return cloned, nil
}

func validatePostBindings(bindings []PostMiddlewareBinding) ([]PostMiddlewareBinding, error) {
	cloned := make([]PostMiddlewareBinding, len(bindings))
	seen := make(map[string]struct{}, len(bindings))
	for index, binding := range bindings {
		if err := validateBinding(binding.ID, binding.AllActions, binding.Actions, binding.Middleware != nil, seen); err != nil {
			return nil, fmt.Errorf("ai post middleware registration: %w", err)
		}
		binding.Actions = append([]Action(nil), binding.Actions...)
		cloned[index] = binding
	}
	return cloned, nil
}

func validateBinding(id string, allActions bool, actions []Action, middlewarePresent bool, seen map[string]struct{}) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("middleware ID must not be empty")
	}
	if _, exists := seen[id]; exists {
		return fmt.Errorf("duplicate middleware ID %q", id)
	}
	seen[id] = struct{}{}
	if !middlewarePresent {
		return fmt.Errorf("middleware %q implementation must not be nil", id)
	}
	if allActions == (len(actions) > 0) {
		return fmt.Errorf("middleware %q must select either all actions or a non-empty action list", id)
	}
	for _, action := range actions {
		if strings.TrimSpace(string(action)) == "" {
			return fmt.Errorf("middleware %q contains an empty action", id)
		}
	}
	return nil
}

func appliesToAction(allActions bool, actions []Action, action Action) bool {
	if allActions {
		return true
	}
	for _, candidate := range actions {
		if candidate == action {
			return true
		}
	}
	return false
}
