// Package credential owns the narrow secure-storage boundary for the built-in
// OpenAI credential. It deliberately does not expose a generic secret API.
package credential

import "context"

type OpenAIStore interface {
	Set(context.Context, string) error
	Exists(context.Context) (bool, error)
	Delete(context.Context) error
	Resolve(context.Context) (value string, exists bool, err error)
}

// NewOpenAIStore resolves the desired credential store for the current
// platform. useSystemCredentialManager is macOS-only product intent: true
// selects the real system Keychain, false selects Lum Harbor's own
// application-managed fallback store. Every other platform has exactly one
// production store and ignores the flag.
func NewOpenAIStore(useSystemCredentialManager bool) OpenAIStore {
	return newPlatformOpenAIStore(useSystemCredentialManager)
}
