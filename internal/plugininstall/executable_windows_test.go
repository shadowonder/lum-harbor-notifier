//go:build windows

package plugininstall

import (
	"os"
	"path/filepath"
	"testing"
)

// This proves the exact defect Slice 1 found and Slice 3 fixes: a plugin
// artifact file that Go's os.FileInfo.Mode() reports with no POSIX
// executable bit (which is every regular file on Windows, always) must
// still be accepted, because Windows has no such permission concept. It
// compiles under the Windows cross-build; it has not been executed
// natively (no Windows host available this Slice).
func TestRequireExecutablePermissionAcceptsAnyRegularFileOnWindows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "artifact.exe")
	if err := os.WriteFile(path, []byte("x"), 0o444); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 != 0 {
		t.Fatalf("test assumption violated: Windows unexpectedly reported an executable bit: %v", info.Mode())
	}
	if err := requireExecutablePermission(info, path); err != nil {
		t.Fatalf("expected Windows to accept a regular file regardless of POSIX permission bits: %v", err)
	}
}
