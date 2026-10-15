//go:build darwin

package credential

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"lum-harbor-notifier/internal/appdir"
)

// applicationManagedOpenAIStore is Lum Harbor's own credential persistence,
// used on macOS only when the product's "Use system credential manager"
// setting is turned off.
//
// Threat model (must stay honest, never overstated): this store is a plain
// file protected only by POSIX filesystem permissions (owner-only directory
// and file mode). It is NOT encrypted at rest, and it is weaker than the
// macOS Keychain, which is protected by the OS keychain subsystem and can be
// tied to device unlock state. This store is appropriate for a user who
// cannot or does not want to use the system Keychain, not as a security
// upgrade. In particular, this deliberately does not encrypt the secret and
// then store the decryption key next to it in an ordinary file — that would
// only simulate protection while adding no real barrier for anyone who can
// already read the app's own directory.
type applicationManagedOpenAIStore struct{}

func newApplicationManagedOpenAIStore() OpenAIStore { return applicationManagedOpenAIStore{} }

const credentialFileName = "builtin-openai-credential.secret"

func credentialFilePath() (string, error) {
	directory, err := appdir.ConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve application credential directory: %w", err)
	}
	return filepath.Join(directory, "credentials", credentialFileName), nil
}

// ensureCredentialDir creates (or corrects) the credentials directory so it
// is never readable or writable by anyone other than the owning user, even
// if it already existed with looser permissions from an earlier version or
// external tampering.
func ensureCredentialDir(path string) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create application credential directory %q: %w", directory, err)
	}
	info, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("stat application credential directory %q: %w", directory, err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(directory, 0o700); err != nil {
			return fmt.Errorf("restrict application credential directory %q to owner-only: %w", directory, err)
		}
	}
	return nil
}

func (applicationManagedOpenAIStore) Set(ctx context.Context, value string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if value == "" {
		return errors.New("application-managed credential value must not be empty")
	}
	path, err := credentialFilePath()
	if err != nil {
		return err
	}
	if err := ensureCredentialDir(path); err != nil {
		return err
	}
	directory := filepath.Dir(path)
	temp, err := os.CreateTemp(directory, ".credential-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary application credential file: %w", err)
	}
	tempPath := temp.Name()
	published := false
	defer func() {
		if !published {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := temp.WriteString(value); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary application credential file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary application credential file: %w", err)
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		return fmt.Errorf("set application credential file permissions: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish application credential file: %w", err)
	}
	published = true
	return nil
}

func (s applicationManagedOpenAIStore) Exists(ctx context.Context) (bool, error) {
	_, exists, err := s.Resolve(ctx)
	return exists, err
}

func (applicationManagedOpenAIStore) Resolve(ctx context.Context) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	path, err := credentialFilePath()
	if err != nil {
		return "", false, err
	}
	if err := ensureCredentialDir(path); err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read application credential file: %w", err)
	}
	if len(data) == 0 {
		return "", false, nil
	}
	return string(data), true, nil
}

func (applicationManagedOpenAIStore) Delete(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := credentialFilePath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete application credential file: %w", err)
	}
	return nil
}
