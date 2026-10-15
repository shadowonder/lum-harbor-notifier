package ai

import (
	"context"
	"fmt"
	"strings"
)

// Provider executes canonical requests without access to Lum Harbor services
// or storage. Implementations must be safe for concurrent Execute calls.
type Provider interface {
	Execute(ctx context.Context, request Request) (Response, error)
}

// ProviderRegistration binds a stable runtime identity to a provider adapter.
type ProviderRegistration struct {
	ID       string
	Name     string
	Provider Provider
}

// ProviderSource states who supplied the effective provider.
type ProviderSource string

const (
	ProviderSourceBuiltIn ProviderSource = "BUILTIN"
	ProviderSourcePlugin  ProviderSource = "PLUGIN"
)

// RuntimeMode describes the globally effective provider mode.
type RuntimeMode string

const (
	RuntimeModeBuiltIn        RuntimeMode = "BUILTIN"
	RuntimeModePluginProvider RuntimeMode = "PLUGIN_PROVIDER"
	RuntimeModePluginRuntime  RuntimeMode = "PLUGIN_RUNTIME"
)

// RuntimeStatus is the backend source of truth for future AI settings UI.
type RuntimeStatus struct {
	Mode                  RuntimeMode
	EffectiveProviderID   string
	EffectiveProviderName string
	ProviderSource        ProviderSource
	BuiltInConfigEditable bool
}

type resolvedProvider struct {
	registration ProviderRegistration
	info         ProviderInfo
}

type providerResolver struct {
	builtIn     ProviderRegistration
	replacement *ProviderRegistration
}

func newProviderResolver(builtIn ProviderRegistration, replacement *ProviderRegistration) (providerResolver, error) {
	if err := validateProviderRegistration("built-in", builtIn); err != nil {
		return providerResolver{}, err
	}

	resolver := providerResolver{builtIn: builtIn}
	if replacement != nil {
		if err := validateProviderRegistration("replacement", *replacement); err != nil {
			return providerResolver{}, err
		}
		copy := *replacement
		resolver.replacement = &copy
	}
	return resolver, nil
}

func validateProviderRegistration(kind string, registration ProviderRegistration) error {
	if strings.TrimSpace(registration.ID) == "" {
		return fmt.Errorf("ai %s provider ID must not be empty", kind)
	}
	if strings.TrimSpace(registration.Name) == "" {
		return fmt.Errorf("ai %s provider name must not be empty", kind)
	}
	if registration.Provider == nil {
		return fmt.Errorf("ai %s provider %q must not be nil", kind, registration.ID)
	}
	return nil
}

func (r providerResolver) resolve() resolvedProvider {
	if r.replacement != nil {
		return resolvedProvider{
			registration: *r.replacement,
			info:         ProviderInfo{ID: r.replacement.ID, Name: r.replacement.Name, Source: ProviderSourcePlugin},
		}
	}
	return resolvedProvider{
		registration: r.builtIn,
		info:         ProviderInfo{ID: r.builtIn.ID, Name: r.builtIn.Name, Source: ProviderSourceBuiltIn},
	}
}

func (r providerResolver) status() RuntimeStatus {
	provider := r.resolve()
	mode := RuntimeModeBuiltIn
	editable := true
	if provider.info.Source == ProviderSourcePlugin {
		mode = RuntimeModePluginProvider
		editable = false
	}
	return RuntimeStatus{
		Mode:                  mode,
		EffectiveProviderID:   provider.info.ID,
		EffectiveProviderName: provider.info.Name,
		ProviderSource:        provider.info.Source,
		BuiltInConfigEditable: editable,
	}
}
