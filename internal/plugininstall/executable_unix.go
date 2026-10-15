//go:build !windows

package plugininstall

import (
	"fmt"
	"io/fs"
)

// requireExecutablePermission enforces the Unix/macOS executable-bit
// invariant: an installed plugin artifact's declared executable must
// actually be runnable by its owner, group, or everyone.
func requireExecutablePermission(info fs.FileInfo, path string) error {
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("plugin executable %q has no executable permission", path)
	}
	return nil
}
