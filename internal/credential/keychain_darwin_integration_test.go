//go:build darwin

package credential

import (
	"context"
	"os"
	"testing"
)

// This opt-in test touches the real login Keychain entry used by Lum Harbor.
// It preserves and restores any pre-existing value so ordinary unit tests
// never prompt for Keychain access or mutate developer state.
func TestKeychainOpenAIStoreLifecycleIntegration(t *testing.T) {
	if os.Getenv("LUM_HARBOR_KEYCHAIN_INTEGRATION") != "1" {
		t.Skip("set LUM_HARBOR_KEYCHAIN_INTEGRATION=1 to exercise the real macOS Keychain")
	}
	ctx := context.Background()
	store := NewOpenAIStore(true)
	prior, hadPrior, err := store.Resolve(ctx)
	if err != nil {
		t.Fatalf("capture prior credential: %v", err)
	}
	t.Cleanup(func() {
		if hadPrior {
			_ = store.Set(ctx, prior)
		} else {
			_ = store.Delete(ctx)
		}
	})

	if err := store.Set(ctx, "slice2-keychain-test-one"); err != nil {
		t.Fatal(err)
	}
	if exists, err := store.Exists(ctx); err != nil || !exists {
		t.Fatalf("expected credential to exist: exists=%v err=%v", exists, err)
	}
	if err := store.Set(ctx, "slice2-keychain-test-two"); err != nil {
		t.Fatal(err)
	}
	resolved, exists, err := store.Resolve(ctx)
	if err != nil || !exists || resolved != "slice2-keychain-test-two" {
		t.Fatalf("replacement was not resolved: exists=%v err=%v", exists, err)
	}
	if err := store.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if exists, err := store.Exists(ctx); err != nil || exists {
		t.Fatalf("expected credential deletion: exists=%v err=%v", exists, err)
	}
}
