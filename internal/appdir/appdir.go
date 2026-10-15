// Package appdir centralizes Lum Harbor's operating-system configuration path.
package appdir

import (
	"fmt"
	"os"
	"path/filepath"
)

const directoryName = "lum_harbor"

// ConfigDir returns the shared application directory beneath the current
// user's OS-specific configuration root. It does not create the directory;
// callers retain ownership of the permissions and lifecycle they require.
func ConfigDir() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("get user config directory: %w", err)
	}
	return filepath.Join(root, directoryName), nil
}
