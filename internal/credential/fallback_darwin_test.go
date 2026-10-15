//go:build darwin

package credential

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func setupIsolatedCredentialHome(t *testing.T) {
	t.Helper()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
}

func TestApplicationManagedStoreSetExistsResolveDelete(t *testing.T) {
	setupIsolatedCredentialHome(t)
	ctx := context.Background()
	store := newApplicationManagedOpenAIStore()

	if exists, err := store.Exists(ctx); err != nil || exists {
		t.Fatalf("expected no credential initially: exists=%v err=%v", exists, err)
	}

	if err := store.Set(ctx, "fallback-secret-one"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if exists, err := store.Exists(ctx); err != nil || !exists {
		t.Fatalf("expected credential to exist: exists=%v err=%v", exists, err)
	}
	value, exists, err := store.Resolve(ctx)
	if err != nil || !exists || value != "fallback-secret-one" {
		t.Fatalf("resolve mismatch: value=%q exists=%v err=%v", value, exists, err)
	}

	if err := store.Set(ctx, "fallback-secret-two"); err != nil {
		t.Fatalf("replace: %v", err)
	}
	value, exists, err = store.Resolve(ctx)
	if err != nil || !exists || value != "fallback-secret-two" {
		t.Fatalf("replacement mismatch: value=%q exists=%v err=%v", value, exists, err)
	}

	if err := store.Delete(ctx); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if exists, err := store.Exists(ctx); err != nil || exists {
		t.Fatalf("expected credential deleted: exists=%v err=%v", exists, err)
	}
	if err := store.Delete(ctx); err != nil {
		t.Fatalf("delete of already-missing credential must be a no-op: %v", err)
	}
}

func TestApplicationManagedStorePersistsAcrossFreshInstance(t *testing.T) {
	setupIsolatedCredentialHome(t)
	ctx := context.Background()

	first := newApplicationManagedOpenAIStore()
	if err := first.Set(ctx, "fallback-persisted-secret"); err != nil {
		t.Fatalf("set: %v", err)
	}

	second := newApplicationManagedOpenAIStore()
	value, exists, err := second.Resolve(ctx)
	if err != nil || !exists || value != "fallback-persisted-secret" {
		t.Fatalf("expected persisted secret via a fresh store instance: value=%q exists=%v err=%v", value, exists, err)
	}
}

func TestApplicationManagedStoreRejectsEmptySecret(t *testing.T) {
	setupIsolatedCredentialHome(t)
	store := newApplicationManagedOpenAIStore()
	if err := store.Set(context.Background(), ""); err == nil {
		t.Fatal("expected empty credential to be rejected")
	}
}

func TestApplicationManagedStoreFilePermissionsAreUserOnly(t *testing.T) {
	setupIsolatedCredentialHome(t)
	ctx := context.Background()
	store := newApplicationManagedOpenAIStore()
	if err := store.Set(ctx, "fallback-permission-check"); err != nil {
		t.Fatalf("set: %v", err)
	}

	path, err := credentialFilePath()
	if err != nil {
		t.Fatalf("credentialFilePath: %v", err)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat credential file: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("expected owner-only file permissions, got %o", perm)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat credential directory: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("expected owner-only directory permissions, got %o", perm)
	}
}

func TestApplicationManagedStoreCorrectsOverlyBroadDirectoryPermissions(t *testing.T) {
	setupIsolatedCredentialHome(t)
	path, err := credentialFilePath()
	if err != nil {
		t.Fatalf("credentialFilePath: %v", err)
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("pre-create overly broad directory: %v", err)
	}

	ctx := context.Background()
	store := newApplicationManagedOpenAIStore()
	if err := store.Set(ctx, "fallback-permission-correction"); err != nil {
		t.Fatalf("set: %v", err)
	}

	info, err := os.Stat(directory)
	if err != nil {
		t.Fatalf("stat credential directory: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("expected directory permissions to be corrected to owner-only, got %o", perm)
	}
}

func TestApplicationManagedStoreEmptyFileResolvesAsMissing(t *testing.T) {
	setupIsolatedCredentialHome(t)
	path, err := credentialFilePath()
	if err != nil {
		t.Fatalf("credentialFilePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create credential directory: %v", err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("write empty credential file: %v", err)
	}

	store := newApplicationManagedOpenAIStore()
	value, exists, err := store.Resolve(context.Background())
	if err != nil || exists || value != "" {
		t.Fatalf("expected empty file to resolve as missing: value=%q exists=%v err=%v", value, exists, err)
	}
}
