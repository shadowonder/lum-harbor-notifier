// Package aiadapter maps the generic plugin transport to Lum Harbor's
// provider-neutral AI runtime contracts. The plugin core does not depend on AI.
package aiadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/plugin"
)

type Provider struct {
	invoker     plugin.Invoker
	extensionID string
	instance    *plugin.Instance
}

func NewProvider(invoker plugin.Invoker, extensionID string, instance *plugin.Instance) *Provider {
	return &Provider{invoker: invoker, extensionID: extensionID, instance: cloneInstance(instance)}
}

func (a *Provider) Execute(ctx context.Context, value ai.Request) (ai.Response, error) {
	invokeCtx := ctx
	var cancel context.CancelFunc = func() {}
	if a.instance != nil && len(a.instance.Config) > 0 {
		var cfg struct {
			TimeoutSeconds float64 `json:"timeout_seconds"`
		}
		if err := json.Unmarshal(a.instance.Config, &cfg); err == nil && cfg.TimeoutSeconds > 0 {
			duration := time.Duration(cfg.TimeoutSeconds * float64(time.Second))
			if deadline, hasDeadline := ctx.Deadline(); !hasDeadline || time.Until(deadline) > duration {
				invokeCtx, cancel = context.WithTimeout(ctx, duration)
			}
		}
	}
	defer cancel()

	var result response
	if err := a.invoke(invokeCtx, plugin.CapabilityAIProvider, requestToWire(value), &result); err != nil {
		return ai.Response{}, err
	}
	return responseFromWire(result), nil
}

type PreMiddleware struct {
	invoker     plugin.Invoker
	extensionID string
	instance    *plugin.Instance
}

func NewPreMiddleware(invoker plugin.Invoker, extensionID string, instance *plugin.Instance) *PreMiddleware {
	return &PreMiddleware{invoker: invoker, extensionID: extensionID, instance: cloneInstance(instance)}
}

func (a *PreMiddleware) Process(ctx context.Context, value ai.Request) (ai.PreRequestResult, error) {
	var result preResult
	if err := invoke(ctx, a.invoker, a.extensionID, plugin.CapabilityAIPreRequest, a.instance, requestToWire(value), &result); err != nil {
		return ai.PreRequestResult{}, err
	}
	switch result.Decision {
	case ai.DecisionContinue:
		if result.Request != nil || result.Response != nil {
			return ai.PreRequestResult{}, fmt.Errorf("plugin pre middleware CONTINUE must not include a payload")
		}
		return ai.PreRequestResult{Decision: ai.DecisionContinue}, nil
	case ai.DecisionReplace:
		if result.Request == nil || result.Response != nil {
			return ai.PreRequestResult{}, fmt.Errorf("plugin pre middleware REPLACE requires only request")
		}
		return ai.PreRequestResult{Decision: ai.DecisionReplace, Request: requestFromWire(*result.Request)}, nil
	case ai.DecisionStop:
		if result.Response == nil || result.Request != nil {
			return ai.PreRequestResult{}, fmt.Errorf("plugin pre middleware STOP requires only response")
		}
		return ai.PreRequestResult{Decision: ai.DecisionStop, Response: responseFromWire(*result.Response)}, nil
	default:
		return ai.PreRequestResult{}, fmt.Errorf("plugin pre middleware returned unsupported decision %q", result.Decision)
	}
}

type PostMiddleware struct {
	invoker     plugin.Invoker
	extensionID string
	instance    *plugin.Instance
}

func NewPostMiddleware(invoker plugin.Invoker, extensionID string, instance *plugin.Instance) *PostMiddleware {
	return &PostMiddleware{invoker: invoker, extensionID: extensionID, instance: cloneInstance(instance)}
}

func (a *PostMiddleware) Process(ctx context.Context, value ai.Response) (ai.PostResponseResult, error) {
	var result postResult
	if err := invoke(ctx, a.invoker, a.extensionID, plugin.CapabilityAIPostResponse, a.instance, responseToWire(value), &result); err != nil {
		return ai.PostResponseResult{}, err
	}
	switch result.Decision {
	case ai.DecisionContinue:
		if result.Response != nil {
			return ai.PostResponseResult{}, fmt.Errorf("plugin post middleware CONTINUE must not include a payload")
		}
		return ai.PostResponseResult{Decision: ai.DecisionContinue}, nil
	case ai.DecisionReplace, ai.DecisionStop:
		if result.Response == nil {
			return ai.PostResponseResult{}, fmt.Errorf("plugin post middleware %s requires response", result.Decision)
		}
		converted := responseFromWire(*result.Response)
		converted.Provenance = value.Provenance
		return ai.PostResponseResult{Decision: result.Decision, Response: converted}, nil
	default:
		return ai.PostResponseResult{}, fmt.Errorf("plugin post middleware returned unsupported decision %q", result.Decision)
	}
}

func (a *Provider) invoke(ctx context.Context, capability plugin.CapabilityID, input any, output any) error {
	return invoke(ctx, a.invoker, a.extensionID, capability, a.instance, input, output)
}

func invoke(ctx context.Context, invoker plugin.Invoker, extensionID string, capability plugin.CapabilityID, instance *plugin.Instance, input any, output any) error {
	if invoker == nil {
		return fmt.Errorf("plugin AI adapter invoker must not be nil")
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("encode plugin AI capability %q input: %w", capability, err)
	}
	result, err := invoker.Invoke(ctx, plugin.Invocation{
		ExtensionID: extensionID, CapabilityID: capability, Instance: cloneInstance(instance), Input: encoded,
	})
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(result))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("decode plugin AI capability %q result: %w", capability, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("decode plugin AI capability %q result: trailing JSON", capability)
	}
	return nil
}

func cloneInstance(instance *plugin.Instance) *plugin.Instance {
	if instance == nil {
		return nil
	}
	cloned := *instance
	cloned.Config = append(json.RawMessage(nil), instance.Config...)
	return &cloned
}
