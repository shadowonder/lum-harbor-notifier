package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type DiscoveryError struct {
	Directory string
	Err       error
}

func (e DiscoveryError) Error() string {
	return fmt.Sprintf("discover extension %q: %v", e.Directory, e.Err)
}

func Discover(root string) (*Registry, []DiscoveryError, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, fmt.Errorf("discover plugin root %q: %w", root, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	extensions := make([]Extension, 0, len(entries))
	var failures []DiscoveryError
	seenIDs := make(map[string]string)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(root, entry.Name())
		extension, err := LoadExtension(directory)
		if err != nil {
			failures = append(failures, DiscoveryError{Directory: directory, Err: err})
			continue
		}
		if firstDirectory, exists := seenIDs[extension.Manifest.ID]; exists {
			failures = append(failures, DiscoveryError{Directory: directory, Err: fmt.Errorf("%w: duplicate extension id %q (already discovered in %q)", ErrManifestInvalid, extension.Manifest.ID, firstDirectory)})
			continue
		}
		seenIDs[extension.Manifest.ID] = directory
		extensions = append(extensions, extension)
	}
	registry, err := NewRegistry(extensions)
	if err != nil {
		return nil, failures, err
	}
	return registry, failures, nil
}

// LoadExtension validates and resolves one unpacked extension directory using
// the same manifest and executable-containment rules as discovery.
func LoadExtension(directory string) (Extension, error) {
	manifest, err := LoadManifest(filepath.Join(directory, ManifestFileName))
	if err != nil {
		return Extension{}, err
	}
	executable, err := resolvedExecutablePath(directory, manifest.Executable)
	if err != nil {
		return Extension{}, fmt.Errorf("%w: extension %q: %v", ErrManifestInvalid, manifest.ID, err)
	}
	return Extension{Manifest: manifest, Directory: directory, ExecutablePath: executable}, nil
}
