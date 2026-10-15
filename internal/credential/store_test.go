package credential

import "testing"

func TestNewOpenAIStoreReturnsPlatformBoundary(t *testing.T) {
	if NewOpenAIStore(true) == nil {
		t.Fatal("expected platform credential store for system credential manager")
	}
	if NewOpenAIStore(false) == nil {
		t.Fatal("expected platform credential store for application-managed fallback")
	}
}
