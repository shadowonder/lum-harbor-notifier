package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/airuntime"
	"lum-harbor-notifier/internal/applicationruntime"
	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/identity"
	"lum-harbor-notifier/internal/openai"
	"lum-harbor-notifier/internal/plugin"
)

type AIControlPlaneReader interface {
	Projection() applicationruntime.AIProjection
}

type BuiltInOpenAISettingsDTO struct {
	Endpoint                   string `json:"endpoint"`
	Model                      string `json:"model"`
	RequestTimeoutSeconds      int    `json:"requestTimeoutSeconds"`
	UseSystemCredentialManager bool   `json:"useSystemCredentialManager"`
	CredentialConfigured       bool   `json:"credentialConfigured"`
	RestartRequired            bool   `json:"restartRequired"`
}

type SaveBuiltInOpenAISettingsRequestDTO struct {
	Endpoint                   string `json:"endpoint"`
	Model                      string `json:"model"`
	RequestTimeoutSeconds      int    `json:"requestTimeoutSeconds"`
	UseSystemCredentialManager bool   `json:"useSystemCredentialManager"`
}

type SetBuiltInOpenAICredentialRequestDTO struct {
	Credential string `json:"credential"`
}

type AICredentialStateDTO struct {
	CredentialConfigured bool `json:"credentialConfigured"`
	RestartRequired      bool `json:"restartRequired"`
}

type AIRuntimeVerificationDTO struct {
	Success               bool   `json:"success"`
	EffectiveProviderID   string `json:"effectiveProviderId"`
	EffectiveProviderName string `json:"effectiveProviderName"`
	Message               string `json:"message"`
	ElapsedMilliseconds   int64  `json:"elapsedMilliseconds"`
}

type AIRuntimeDTO struct {
	Status                     string              `json:"status"`
	Issue                      string              `json:"issue,omitempty"`
	Mode                       string              `json:"mode"`
	EffectiveProviderID        string              `json:"effectiveProviderId"`
	EffectiveProviderName      string              `json:"effectiveProviderName"`
	ProviderSource             string              `json:"providerSource"`
	BuiltInConfigEditable      bool                `json:"builtInConfigEditable"`
	ManagingExtensionID        string              `json:"managingExtensionId,omitempty"`
	ManagingExtensionName      string              `json:"managingExtensionName,omitempty"`
	ManagingProviderInstanceID string              `json:"managingProviderInstanceId,omitempty"`
	Stages                     []AIRuntimeStageDTO `json:"stages"`
}

type AIRuntimeStageDTO struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Stage             string   `json:"stage"`
	Order             int      `json:"order"`
	Actions           []string `json:"actions"`
	AllActions        bool     `json:"allActions"`
	Enabled           bool     `json:"enabled"`
	Available         bool     `json:"available"`
	ExtensionID       string   `json:"extensionId,omitempty"`
	ExtensionName     string   `json:"extensionName,omitempty"`
	InstanceID        string   `json:"instanceId,omitempty"`
	UnavailableReason string   `json:"unavailableReason,omitempty"`
}

// GetAIRuntime returns the application-owned, frontend-safe projection of the
// exact production AI composition. It never substitutes preview data.
func (a *App) GetAIRuntime() (AIRuntimeDTO, error) {
	if a.aiControlPlane == nil {
		return AIRuntimeDTO{}, fmt.Errorf("AI runtime control plane is not initialized")
	}
	projection := a.aiControlPlane.Projection()
	result := AIRuntimeDTO{
		Status: projection.Status, Issue: projection.Issue, Mode: string(projection.Mode),
		EffectiveProviderID:        projection.EffectiveProviderID,
		EffectiveProviderName:      projection.EffectiveProviderName,
		ProviderSource:             string(projection.ProviderSource),
		BuiltInConfigEditable:      projection.BuiltInConfigEditable,
		ManagingExtensionID:        projection.ManagingExtensionID,
		ManagingExtensionName:      projection.ManagingExtensionName,
		ManagingProviderInstanceID: projection.ManagingProviderInstanceID,
		Stages:                     make([]AIRuntimeStageDTO, 0, len(projection.Stages)),
	}
	for _, stage := range projection.Stages {
		result.Stages = append(result.Stages, AIRuntimeStageDTO{
			ID: stage.ID, Name: stage.Name, Stage: stage.Stage, Order: stage.Order,
			Actions: append([]string(nil), stage.Actions...), AllActions: stage.AllActions,
			Enabled: stage.Enabled, Available: stage.Available,
			ExtensionID: stage.ExtensionID, ExtensionName: stage.ExtensionName,
			InstanceID: stage.InstanceID, UnavailableReason: stage.UnavailableReason,
		})
	}
	return result, nil
}

func (a *App) GetBuiltInOpenAISettings() (BuiltInOpenAISettingsDTO, error) {
	a.aiConfigMu.Lock()
	defer a.aiConfigMu.Unlock()
	return a.readBuiltInOpenAISettings(appContext(a.ctx))
}

func (a *App) SaveBuiltInOpenAISettings(request SaveBuiltInOpenAISettingsRequestDTO) (BuiltInOpenAISettingsDTO, error) {
	a.aiConfigMu.Lock()
	defer a.aiConfigMu.Unlock()
	cfg, err := config.Load()
	if err != nil {
		return BuiltInOpenAISettingsDTO{}, fmt.Errorf("load current runtime configuration: %w", err)
	}
	cfg.AI.BuiltInOpenAI = config.BuiltInOpenAIConfig{
		Endpoint: request.Endpoint, Model: request.Model, RequestTimeoutSeconds: request.RequestTimeoutSeconds,
		UseSystemCredentialManager: request.UseSystemCredentialManager,
	}
	if err := config.Save(cfg); err != nil {
		return BuiltInOpenAISettingsDTO{}, fmt.Errorf("save built-in OpenAI configuration: %w", err)
	}
	a.aiRestartRequired = true
	return a.readBuiltInOpenAISettings(appContext(a.ctx))
}

func (a *App) SetBuiltInOpenAICredential(request SetBuiltInOpenAICredentialRequestDTO) (AICredentialStateDTO, error) {
	a.aiConfigMu.Lock()
	defer a.aiConfigMu.Unlock()
	if a.aiCredentialStoreResolver == nil {
		return AICredentialStateDTO{}, fmt.Errorf("built-in OpenAI credential store is not initialized")
	}
	if strings.TrimSpace(request.Credential) == "" {
		return AICredentialStateDTO{}, fmt.Errorf("credential must not be empty")
	}
	cfg, err := config.Load()
	if err != nil {
		return AICredentialStateDTO{}, fmt.Errorf("load current runtime configuration: %w", err)
	}
	store := a.aiCredentialStoreResolver(cfg.AI.BuiltInOpenAI.UseSystemCredentialManager)
	if err := store.Set(appContext(a.ctx), request.Credential); err != nil {
		return AICredentialStateDTO{}, fmt.Errorf("store built-in OpenAI credential securely: operation failed")
	}
	a.aiRestartRequired = true
	return AICredentialStateDTO{CredentialConfigured: true, RestartRequired: true}, nil
}

func (a *App) DeleteBuiltInOpenAICredential() (AICredentialStateDTO, error) {
	a.aiConfigMu.Lock()
	defer a.aiConfigMu.Unlock()
	if a.aiCredentialStoreResolver == nil {
		return AICredentialStateDTO{}, fmt.Errorf("built-in OpenAI credential store is not initialized")
	}
	cfg, err := config.Load()
	if err != nil {
		return AICredentialStateDTO{}, fmt.Errorf("load current runtime configuration: %w", err)
	}
	store := a.aiCredentialStoreResolver(cfg.AI.BuiltInOpenAI.UseSystemCredentialManager)
	if err := store.Delete(appContext(a.ctx)); err != nil {
		return AICredentialStateDTO{}, fmt.Errorf("delete built-in OpenAI credential securely: operation failed")
	}
	a.aiRestartRequired = true
	return AICredentialStateDTO{CredentialConfigured: false, RestartRequired: true}, nil
}

func (a *App) VerifyAIRuntime() (AIRuntimeVerificationDTO, error) {
	if a.aiControlPlane == nil || (a.aiRuntime == nil && a.aiExecutor == nil) {
		return AIRuntimeVerificationDTO{}, fmt.Errorf("AI runtime is not initialized")
	}
	projection := a.aiControlPlane.Projection()
	result := AIRuntimeVerificationDTO{
		EffectiveProviderID:   projection.EffectiveProviderID,
		EffectiveProviderName: projection.EffectiveProviderName,
	}
	if projection.Status != applicationruntime.AIRuntimeReady {
		result.Message = projection.Issue
		if result.Message == "" {
			result.Message = "Current AI runtime is unavailable."
		}
		return result, nil
	}
	requestID, err := identity.New()
	if err != nil {
		return AIRuntimeVerificationDTO{}, err
	}
	const verifyInput = "Confirm this AI runtime can produce a structured response by returning {\"ok\":true}."
	verifySchema := json.RawMessage(`{"type":"object","properties":{"ok":{"type":"boolean","const":true}},"required":["ok"],"additionalProperties":false}`)
	started := time.Now()
	var output json.RawMessage
	var executeErr error
	if a.aiRuntime != nil {
		response, err := a.aiRuntime.Execute(appContext(a.ctx), airuntime.Request{
			RequestID: requestID, Purpose: ai.ActionRuntimeVerify,
			Instructions: "Return only JSON matching the supplied schema.",
			Input:        verifyInput, OutputSchema: verifySchema, Limits: airuntime.DefaultLimits(),
			Metadata: map[string]string{"caller": "settings-verification"},
		})
		output, executeErr = response.Output, err
	} else {
		response, err := a.aiExecutor.Execute(appContext(a.ctx), ai.Request{
			RequestID: requestID, Action: ai.ActionRuntimeVerify,
			Messages: []ai.Message{
				{Role: ai.RoleSystem, Content: "Return only JSON matching the supplied schema."},
				{Role: ai.RoleUser, Content: verifyInput},
			},
			OutputSchema: verifySchema,
		})
		output, executeErr = response.Output, err
	}
	result.ElapsedMilliseconds = time.Since(started).Milliseconds()
	if executeErr != nil {
		var rpcErr *plugin.RPCError
		switch {
		case errors.As(executeErr, &rpcErr) && rpcErr.Code == "configuration_error":
			result.Message = fmt.Sprintf("configuration_error: %s", rpcErr.Message)
		case errors.Is(executeErr, openai.ErrAuthentication):
			result.Message = "OpenAI credential was rejected."
		case errors.Is(executeErr, context.DeadlineExceeded):
			result.Message = "AI runtime verification timed out."
		default:
			result.Message = "AI runtime verification failed in the current pipeline."
		}
		return result, nil
	}
	var payload struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(output, &payload); err != nil || !payload.OK {
		result.Message = "AI runtime returned an invalid verification response."
		return result, nil
	}
	result.Success = true
	result.Message = "Current AI runtime verified successfully."
	return result, nil
}

// readBuiltInOpenAISettings reflects the currently saved/desired credential
// store's truth, not the store the process started with: it loads config
// fresh and resolves the store from that fresh UseSystemCredentialManager
// value on every call, so toggling store mode without a restart is reflected
// immediately (e.g. switching to the fallback store while Keychain still
// holds an old credential must show "missing" for the now-desired fallback,
// not "configured" from the no-longer-selected Keychain entry).
func (a *App) readBuiltInOpenAISettings(ctx context.Context) (BuiltInOpenAISettingsDTO, error) {
	if a.aiCredentialStoreResolver == nil {
		return BuiltInOpenAISettingsDTO{}, fmt.Errorf("built-in OpenAI credential store is not initialized")
	}
	cfg, err := config.Load()
	if err != nil {
		return BuiltInOpenAISettingsDTO{}, err
	}
	saved := cfg.AI.BuiltInOpenAI
	store := a.aiCredentialStoreResolver(saved.UseSystemCredentialManager)
	exists, err := store.Exists(ctx)
	if err != nil {
		return BuiltInOpenAISettingsDTO{}, fmt.Errorf("check built-in OpenAI credential state: operation failed")
	}
	return BuiltInOpenAISettingsDTO{
		Endpoint: saved.Endpoint, Model: saved.Model, RequestTimeoutSeconds: saved.RequestTimeoutSeconds,
		UseSystemCredentialManager: saved.UseSystemCredentialManager,
		CredentialConfigured:       exists,
		RestartRequired:            a.aiRestartRequired || saved != a.startupAIConfig,
	}, nil
}
