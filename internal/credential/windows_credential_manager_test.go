//go:build windows

package credential

import (
	"context"
	"os"
	"testing"
)

// This opt-in test touches the real Windows Credential Manager entry used by
// Lum Harbor. It preserves and restores any pre-existing value so ordinary
// unit tests never mutate developer state. It can only run on an actual
// Windows host; it is not exercised by this project's cross-compiled build
// verification, which only confirms the package compiles for windows/amd64.
func TestWindowsCredentialManagerStoreLifecycleIntegration(t *testing.T) {
	if os.Getenv("LUM_HARBOR_WINDOWS_CREDENTIAL_INTEGRATION") != "1" {
		t.Skip("set LUM_HARBOR_WINDOWS_CREDENTIAL_INTEGRATION=1 to exercise the real Windows Credential Manager")
	}
	ctx := context.Background()
	store := NewOpenAIStore(false)
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

	if err := store.Set(ctx, "slice2-windows-credential-test-one"); err != nil {
		t.Fatal(err)
	}
	if exists, err := store.Exists(ctx); err != nil || !exists {
		t.Fatalf("expected credential to exist: exists=%v err=%v", exists, err)
	}
	if err := store.Set(ctx, "slice2-windows-credential-test-two"); err != nil {
		t.Fatal(err)
	}
	resolved, exists, err := store.Resolve(ctx)
	if err != nil || !exists || resolved != "slice2-windows-credential-test-two" {
		t.Fatalf("replacement was not resolved: exists=%v err=%v", exists, err)
	}
	if err := store.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if exists, err := store.Exists(ctx); err != nil || exists {
		t.Fatalf("expected credential deletion: exists=%v err=%v", exists, err)
	}
}

func TestNewOpenAIStoreIgnoresSystemCredentialManagerFlagOnWindows(t *testing.T) {
	if NewOpenAIStore(true) == nil || NewOpenAIStore(false) == nil {
		t.Fatal("expected a Windows Credential Manager store regardless of the macOS-only flag")
	}
}
