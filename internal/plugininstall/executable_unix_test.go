//go:build !windows

package plugininstall

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequireExecutablePermissionRejectsNonExecutableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireExecutablePermission(info, path); err == nil {
		t.Fatal("expected a non-executable file to be rejected on Unix")
	}
}

func TestRequireExecutablePermissionAcceptsExecutableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact")
	if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireExecutablePermission(info, path); err != nil {
		t.Fatalf("expected an executable file to be accepted on Unix: %v", err)
	}
}
