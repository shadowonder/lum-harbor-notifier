package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrInvalidRequest classifies canonical request validation failures.
	ErrInvalidRequest = errors.New("invalid AI request")
	// ErrMiddleware classifies middleware failures and invariant violations.
	ErrMiddleware = errors.New("AI middleware failure")
	// ErrProvider classifies effective-provider execution failures.
	ErrProvider = errors.New("AI provider failure")
	// ErrInvalidProviderResponse classifies response correlation failures.
	ErrInvalidProviderResponse = errors.New("invalid AI provider response")
	// ErrFinalValidation classifies final structured-output failures.
	ErrFinalValidation = errors.New("AI final response validation failure")
)

// Config defines an immutable AI runtime assembled at application startup.
type Config struct {
	BuiltInProvider     ProviderRegistration
	ReplacementProvider *ProviderRegistration
	PreMiddleware       []PreMiddlewareBinding
	PostMiddleware      []PostMiddlewareBinding
	Validator           ResponseValidator
}

// ExecutionPlan is the point-in-time pipeline selected for one Action.
type ExecutionPlan struct {
	Action            Action
	PreMiddleware     []PreMiddlewareBinding
	Provider          ProviderRegistration
	EffectiveProvider ProviderInfo
	PostMiddleware    []PostMiddlewareBinding
}

// Service is Lum Harbor's sole AI execution boundary. Its configuration is
// copied at construction and then read-only, making Execute safe to call from
// multiple goroutines when registered adapters honor their concurrency contract.
type Service struct {
	resolver       providerResolver
	preMiddleware  []PreMiddlewareBinding
	postMiddleware []PostMiddlewareBinding
	validator      ResponseValidator
}

// NewService validates and freezes provider and middleware configuration.
func NewService(config Config) (*Service, error) {
	resolver, err := newProviderResolver(config.BuiltInProvider, config.ReplacementProvider)
	if err != nil {
		return nil, err
	}
	pre, err := validatePreBindings(config.PreMiddleware)
	if err != nil {
		return nil, err
	}
	post, err := validatePostBindings(config.PostMiddleware)
	if err != nil {
		return nil, err
	}
	return &Service{resolver: resolver, preMiddleware: pre, postMiddleware: post, validator: config.Validator}, nil
}

// RuntimeStatus reports the globally effective provider and whether built-in
// provider configuration should be editable in a future settings UI.
func (s *Service) RuntimeStatus() RuntimeStatus {
	return s.resolver.status()
}

// Execute validates and runs the fixed pre/provider/post/final-validation
// pipeline. A pre STOP skips the provider but still passes through post stages.
func (s *Service) Execute(ctx context.Context, request Request) (Response, error) {
	if ctx == nil {
		return Response{}, fmt.Errorf("%w: context must not be nil", ErrInvalidRequest)
	}
	request = cloneRequest(request)
	if err := validateRequest(request); err != nil {
		return Response{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}

	plan := s.buildExecutionPlan(request.Action)
	originalRequestID := request.RequestID
	originalAction := request.Action
	originalSchema := append(json.RawMessage(nil), request.OutputSchema...)

	var response Response
	stopped := false
	for _, binding := range plan.PreMiddleware {
		if err := ctx.Err(); err != nil {
			return Response{}, fmt.Errorf("%w: action %q pre middleware %q: %w", ErrMiddleware, request.Action, binding.ID, err)
		}
		result, err := binding.Middleware.Process(ctx, cloneRequest(request))
		if err != nil {
			return Response{}, fmt.Errorf("%w: action %q pre middleware %q: %w", ErrMiddleware, request.Action, binding.ID, err)
		}
		switch result.Decision {
		case DecisionContinue:
			// The returned payload is deliberately ignored.
		case DecisionReplace:
			replacement := cloneRequest(result.Request)
			if err := validateRequestInvariants(replacement, originalRequestID, originalAction, originalSchema); err != nil {
				return Response{}, fmt.Errorf("%w: action %q pre middleware %q violated request invariant: %w", ErrMiddleware, originalAction, binding.ID, err)
			}
			if err := validateRequest(replacement); err != nil {
				return Response{}, fmt.Errorf("%w: action %q pre middleware %q returned invalid replacement: %w", ErrMiddleware, originalAction, binding.ID, err)
			}
			request = replacement
		case DecisionStop:
			response = cloneResponse(result.Response)
			if err := validateResponseCorrelation(response, originalRequestID, originalAction); err != nil {
				return Response{}, fmt.Errorf("%w: action %q pre middleware %q returned invalid STOP response: %w", ErrMiddleware, originalAction, binding.ID, err)
			}
			stampPreMiddlewareProvenance(&response, binding)
			stopped = true
		default:
			return Response{}, fmt.Errorf("%w: action %q pre middleware %q returned unsupported decision %q", ErrMiddleware, originalAction, binding.ID, result.Decision)
		}
		if stopped {
			break
		}
	}

	if !stopped {
		if err := ctx.Err(); err != nil {
			return Response{}, fmt.Errorf("%w: action %q provider %q: %w", ErrProvider, originalAction, plan.Provider.ID, err)
		}
		providerResponse, err := plan.Provider.Provider.Execute(ctx, cloneRequest(request))
		if err != nil {
			return Response{}, fmt.Errorf("%w: action %q provider %q: %w", ErrProvider, originalAction, plan.Provider.ID, err)
		}
		response = cloneResponse(providerResponse)
		if err := validateResponseCorrelation(response, originalRequestID, originalAction); err != nil {
			return Response{}, fmt.Errorf("%w: action %q provider %q: %w", ErrInvalidProviderResponse, originalAction, plan.Provider.ID, err)
		}
		stampProviderProvenance(&response, plan.EffectiveProvider)
	}

	for _, binding := range plan.PostMiddleware {
		if err := ctx.Err(); err != nil {
			return Response{}, fmt.Errorf("%w: action %q post middleware %q: %w", ErrMiddleware, originalAction, binding.ID, err)
		}
		result, err := binding.Middleware.Process(ctx, cloneResponse(response))
		if err != nil {
			return Response{}, fmt.Errorf("%w: action %q post middleware %q: %w", ErrMiddleware, originalAction, binding.ID, err)
		}
		switch result.Decision {
		case DecisionContinue:
			// The returned payload is deliberately ignored.
		case DecisionReplace, DecisionStop:
			replacement := cloneResponse(result.Response)
			if err := validateResponseInvariants(replacement, originalRequestID, originalAction, response.Provenance); err != nil {
				return Response{}, fmt.Errorf("%w: action %q post middleware %q violated response invariant: %w", ErrMiddleware, originalAction, binding.ID, err)
			}
			response = replacement
		default:
			return Response{}, fmt.Errorf("%w: action %q post middleware %q returned unsupported decision %q", ErrMiddleware, originalAction, binding.ID, result.Decision)
		}
		if result.Decision == DecisionStop {
			break
		}
	}

	if err := validateJSONResponse(response); err != nil {
		return Response{}, fmt.Errorf("%w: action %q: %w", ErrFinalValidation, originalAction, err)
	}
	if s.validator != nil {
		if err := s.validator.Validate(ctx, cloneRequest(request), cloneResponse(response)); err != nil {
			return Response{}, fmt.Errorf("%w: action %q: %w", ErrFinalValidation, originalAction, err)
		}
	}
	return cloneResponse(response), nil
}

func (s *Service) buildExecutionPlan(action Action) ExecutionPlan {
	provider := s.resolver.resolve()
	plan := ExecutionPlan{Action: action, Provider: provider.registration, EffectiveProvider: provider.info}
	for _, binding := range s.preMiddleware {
		if appliesToAction(binding.AllActions, binding.Actions, action) {
			plan.PreMiddleware = append(plan.PreMiddleware, binding)
		}
	}
	for _, binding := range s.postMiddleware {
		if appliesToAction(binding.AllActions, binding.Actions, action) {
			plan.PostMiddleware = append(plan.PostMiddleware, binding)
		}
	}
	return plan
}

func validateRequest(request Request) error {
	if strings.TrimSpace(request.RequestID) == "" {
		return fmt.Errorf("request ID must not be empty")
	}
	if strings.TrimSpace(string(request.Action)) == "" {
		return fmt.Errorf("action must not be empty")
	}
	if len(request.Messages) == 0 {
		return fmt.Errorf("messages must not be empty")
	}
	for index, message := range request.Messages {
		if strings.TrimSpace(string(message.Role)) == "" {
			return fmt.Errorf("message %d role must not be empty", index)
		}
	}
	if len(request.OutputSchema) == 0 || !json.Valid(request.OutputSchema) {
		return fmt.Errorf("output schema must be valid JSON")
	}
	return nil
}

func validateRequestInvariants(request Request, requestID string, action Action, outputSchema json.RawMessage) error {
	if request.RequestID != requestID {
		return fmt.Errorf("request ID changed from %q to %q", requestID, request.RequestID)
	}
	if request.Action != action {
		return fmt.Errorf("action changed from %q to %q", action, request.Action)
	}
	if !bytes.Equal(request.OutputSchema, outputSchema) {
		return fmt.Errorf("output schema changed")
	}
	return nil
}

func validateResponseCorrelation(response Response, requestID string, action Action) error {
	if response.RequestID != requestID {
		return fmt.Errorf("request ID %q does not match %q", response.RequestID, requestID)
	}
	if response.Action != action {
		return fmt.Errorf("action %q does not match %q", response.Action, action)
	}
	return nil
}

func validateResponseInvariants(response Response, requestID string, action Action, provenance ResponseProvenance) error {
	if err := validateResponseCorrelation(response, requestID, action); err != nil {
		return err
	}
	if !equalResponseProvenance(response.Provenance, provenance) {
		return fmt.Errorf("response provenance changed")
	}
	return nil
}

func stampProviderProvenance(response *Response, provider ProviderInfo) {
	actualProvider := provider
	response.Provenance = ResponseProvenance{
		ProviderInvoked: true,
		ActualProvider:  &actualProvider,
		Origin: ResponseOrigin{
			Kind: ResponseOriginProvider,
			ID:   provider.ID,
			Name: provider.Name,
		},
	}
}

func stampPreMiddlewareProvenance(response *Response, binding PreMiddlewareBinding) {
	response.Provenance = ResponseProvenance{
		ProviderInvoked: false,
		ActualProvider:  nil,
		Origin: ResponseOrigin{
			Kind: ResponseOriginPreMiddleware,
			ID:   binding.ID,
			Name: binding.Name,
		},
	}
}

func equalResponseProvenance(left ResponseProvenance, right ResponseProvenance) bool {
	if left.ProviderInvoked != right.ProviderInvoked || left.Origin != right.Origin {
		return false
	}
	if left.ActualProvider == nil || right.ActualProvider == nil {
		return left.ActualProvider == nil && right.ActualProvider == nil
	}
	return *left.ActualProvider == *right.ActualProvider
}
