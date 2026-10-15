// Package plugininstall validates and places already-built plugin artifacts.
// Discovery and runtime lifecycle remain owned by package plugin.
package plugininstall

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"lum-harbor-notifier/internal/plugin"
)

var (
	ErrAlreadyInstalled     = errors.New("plugin artifact already installed")
	ErrNotInstalled         = errors.New("plugin artifact not installed")
	ErrInvalidPackage       = errors.New("invalid plugin package")
	ErrIncompatiblePlatform = errors.New("incompatible platform")
)

const (
	PackageExtension     = ".lhplugin"
	MaxPackageFileSize   = 200 * 1024 * 1024 // 200 MB max for a single file in archive
	MaxPackageTotalBytes = 500 * 1024 * 1024 // 500 MB max total extracted size
	MaxPackageEntryCount = 10000             // 10,000 entries max
)

type Result struct {
	ExtensionID     string
	Version         string
	Destination     string
	RestartRequired bool
}

type Installer struct {
	root string
}

// New creates an installer for an already-resolved configured plugin root.
// Requiring an absolute root keeps configuration-path policy at composition.
func New(pluginRoot string) (*Installer, error) {
	if strings.TrimSpace(pluginRoot) == "" {
		return nil, fmt.Errorf("plugin root must not be empty")
	}
	if !filepath.IsAbs(pluginRoot) {
		return nil, fmt.Errorf("plugin root must be absolute: %q", pluginRoot)
	}
	return &Installer{root: filepath.Clean(pluginRoot)}, nil
}

// Install validates and atomically places one plugin artifact, accepting either
// a packaged archive (*.lhplugin) or an unpacked artifact directory.
// The new artifact becomes visible to discovery on the next application start.
func (i *Installer) Install(source string) (result Result, resultErr error) {
	source, err := filepath.Abs(source)
	if err != nil {
		return Result{}, fmt.Errorf("resolve source artifact: %w", err)
	}
	source = filepath.Clean(source)

	info, err := os.Lstat(source)
	if err != nil {
		return Result{}, fmt.Errorf("inspect source artifact %q: %w", source, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Result{}, fmt.Errorf("source artifact %q must not be a symlink", source)
	}

	if !info.IsDir() {
		return i.installPackage(source)
	}
	return i.installDirectory(source)
}

func (i *Installer) installPackage(sourceFile string) (result Result, resultErr error) {
	if containsPath(sourceFile, i.root) {
		return Result{}, fmt.Errorf("plugin root %q must not be inside source artifact %q", i.root, sourceFile)
	}
	if err := os.MkdirAll(i.root, 0o700); err != nil {
		return Result{}, fmt.Errorf("create plugin root %q: %w", i.root, err)
	}

	staging, err := os.MkdirTemp(i.root, ".install-package-")
	if err != nil {
		return Result{}, fmt.Errorf("create plugin staging directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(staging); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("clean plugin staging directory %q: %w", staging, err)
		}
	}()

	if err := ExtractPackage(sourceFile, staging); err != nil {
		return Result{}, err
	}
	extension, err := validateExtension(staging)
	if err != nil {
		return Result{}, err
	}
	if err := validateCompatibility(extension.Manifest); err != nil {
		return Result{}, err
	}

	destination := filepath.Join(i.root, extension.Manifest.ID)
	result = Result{
		ExtensionID:     extension.Manifest.ID,
		Version:         extension.Manifest.Version,
		Destination:     destination,
		RestartRequired: true,
	}
	if _, err := os.Lstat(destination); err == nil {
		return Result{}, fmt.Errorf("%w: %s", ErrAlreadyInstalled, destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("inspect install destination %q: %w", destination, err)
	}

	if err := os.Rename(staging, destination); err != nil {
		if _, destinationErr := os.Lstat(destination); destinationErr == nil {
			return Result{}, fmt.Errorf("%w: %s", ErrAlreadyInstalled, destination)
		}
		return Result{}, fmt.Errorf("publish plugin artifact %q: %w", destination, err)
	}
	return result, nil
}

func (i *Installer) installDirectory(source string) (result Result, resultErr error) {
	if err := validateArtifactTree(source); err != nil {
		return Result{}, err
	}
	extension, err := validateExtension(source)
	if err != nil {
		return Result{}, err
	}
	if err := validateCompatibility(extension.Manifest); err != nil {
		return Result{}, err
	}
	if containsPath(source, i.root) {
		return Result{}, fmt.Errorf("plugin root %q must not be inside source artifact %q", i.root, source)
	}

	destination := filepath.Join(i.root, extension.Manifest.ID)
	result = Result{
		ExtensionID:     extension.Manifest.ID,
		Version:         extension.Manifest.Version,
		Destination:     destination,
		RestartRequired: true,
	}
	if _, err := os.Lstat(destination); err == nil {
		return Result{}, fmt.Errorf("%w: %s", ErrAlreadyInstalled, destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("inspect install destination %q: %w", destination, err)
	}
	if err := os.MkdirAll(i.root, 0o700); err != nil {
		return Result{}, fmt.Errorf("create plugin root %q: %w", i.root, err)
	}

	staging, err := os.MkdirTemp(i.root, ".install-"+extension.Manifest.ID+"-")
	if err != nil {
		return Result{}, fmt.Errorf("create plugin staging directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(staging); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("clean plugin staging directory %q: %w", staging, err)
		}
	}()

	if err := copyArtifactTree(source, staging); err != nil {
		return Result{}, err
	}
	staged, err := validateExtension(staging)
	if err != nil {
		return Result{}, fmt.Errorf("validate staged artifact: %w", err)
	}
	if staged.Manifest.ID != extension.Manifest.ID || staged.Manifest.Version != extension.Manifest.Version {
		return Result{}, fmt.Errorf("staged artifact identity changed during installation")
	}
	if _, err := os.Lstat(destination); err == nil {
		return Result{}, fmt.Errorf("%w: %s", ErrAlreadyInstalled, destination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Result{}, fmt.Errorf("inspect install destination %q: %w", destination, err)
	}
	if err := os.Rename(staging, destination); err != nil {
		if _, destinationErr := os.Lstat(destination); destinationErr == nil {
			return Result{}, fmt.Errorf("%w: %s", ErrAlreadyInstalled, destination)
		}
		return Result{}, fmt.Errorf("publish plugin artifact %q: %w", destination, err)
	}
	return result, nil
}

// Remove deletes an installed extension's artifact directory by validated
// manifest ID. It never accepts a caller-supplied path: the destination is
// always <root>/<extensionID>, the exact placement Install uses, so a caller
// can never direct removal outside the configured plugin root.
func (i *Installer) Remove(extensionID string) error {
	extensionID = strings.TrimSpace(extensionID)
	if extensionID == "" {
		return fmt.Errorf("extension id must not be empty")
	}
	if !plugin.IsValidPluginID(extensionID) {
		return fmt.Errorf("invalid extension id %q", extensionID)
	}
	destination := filepath.Join(i.root, extensionID)
	if !containsPath(i.root, destination) {
		return fmt.Errorf("extension id %q escapes the plugin root", extensionID)
	}
	info, err := os.Lstat(destination)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrNotInstalled, extensionID)
		}
		return fmt.Errorf("inspect installed extension %q: %w", extensionID, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("installed extension %q is not a regular directory", extensionID)
	}
	if err := os.RemoveAll(destination); err != nil {
		return fmt.Errorf("remove installed extension %q: %w", extensionID, err)
	}
	return nil
}

func validateArtifactTree(source string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return fmt.Errorf("inspect source artifact %q: %w", source, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("source artifact %q must be a directory, not a symlink", source)
	}
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("inspect artifact entry %q: %w", path, walkErr)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect artifact entry %q: %w", path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("artifact symlink is not supported: %q", path)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("artifact entry must be a regular file or directory: %q", path)
		}
		return nil
	})
}

func validateExtension(directory string) (plugin.Extension, error) {
	extension, err := plugin.LoadExtension(directory)
	if err != nil {
		return plugin.Extension{}, fmt.Errorf("validate plugin artifact %q: %w", directory, err)
	}
	info, err := os.Stat(extension.ExecutablePath)
	if err != nil {
		return plugin.Extension{}, fmt.Errorf("inspect plugin executable %q: %w", extension.ExecutablePath, err)
	}
	if !info.Mode().IsRegular() {
		return plugin.Extension{}, fmt.Errorf("plugin executable %q must be a regular file", extension.ExecutablePath)
	}
	if err := requireExecutablePermission(info, extension.ExecutablePath); err != nil {
		return plugin.Extension{}, err
	}
	return extension, nil
}

func validateCompatibility(m plugin.Manifest) error {
	if len(m.OS) > 0 {
		matched := false
		for _, osName := range m.OS {
			if isOSMatch(osName, runtime.GOOS) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%w: plugin requires OS %v, current system is %s", ErrIncompatiblePlatform, m.OS, runtime.GOOS)
		}
	}
	if len(m.Arch) > 0 {
		matched := false
		for _, archName := range m.Arch {
			if isArchMatch(archName, runtime.GOARCH) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%w: plugin requires architecture %v, current system is %s", ErrIncompatiblePlatform, m.Arch, runtime.GOARCH)
		}
	}
	return nil
}

func isOSMatch(declaredOS, currentOS string) bool {
	d := strings.ToLower(strings.TrimSpace(declaredOS))
	c := strings.ToLower(strings.TrimSpace(currentOS))
	if d == c || d == "any" || d == "all" {
		return true
	}
	if (d == "macos" || d == "darwin") && (c == "macos" || c == "darwin") {
		return true
	}
	return false
}

func isArchMatch(declaredArch, currentArch string) bool {
	d := strings.ToLower(strings.TrimSpace(declaredArch))
	c := strings.ToLower(strings.TrimSpace(currentArch))
	if d == c || d == "any" || d == "universal" || d == "all" {
		return true
	}
	if (d == "x86_64" || d == "amd64") && (c == "x86_64" || c == "amd64") {
		return true
	}
	if (d == "arm64" || d == "aarch64") && (c == "arm64" || c == "aarch64") {
		return true
	}
	return false
}

// ExtractPackage safely extracts a .lhplugin ZIP archive into targetDir.
// It verifies against path traversal (Zip Slip), symlinks, unsupported file modes,
// archive bombs (size and entry count limits), and ensures plugin.json exists.
func ExtractPackage(packageFile, targetDir string) (resultErr error) {
	targetDir = filepath.Clean(targetDir)
	info, err := os.Lstat(targetDir)
	if err != nil {
		return fmt.Errorf("inspect extraction target %q: %w", targetDir, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: extraction target %q must be a directory", ErrInvalidPackage, targetDir)
	}

	r, err := zip.OpenReader(packageFile)
	if err != nil {
		return fmt.Errorf("%w: open archive: %v", ErrInvalidPackage, err)
	}
	defer r.Close()

	if len(r.File) == 0 {
		return fmt.Errorf("%w: archive is empty", ErrInvalidPackage)
	}
	if len(r.File) > MaxPackageEntryCount {
		return fmt.Errorf("%w: archive contains too many files (%d > %d)", ErrInvalidPackage, len(r.File), MaxPackageEntryCount)
	}

	var totalBytes int64
	hasManifest := false
	seenEntries := make(map[string]struct{}, len(r.File))

	type dirMode struct {
		path string
		mode fs.FileMode
	}
	dirsToChmod := make([]dirMode, 0)

	for _, f := range r.File {
		name := f.Name
		if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") || (len(name) >= 2 && name[1] == ':') {
			return fmt.Errorf("%w: entry %q has absolute or drive-letter path", ErrInvalidPackage, name)
		}
		cleanName := filepath.Clean(filepath.FromSlash(name))
		if cleanName == "." {
			continue
		}
		if filepath.IsAbs(cleanName) || filepath.VolumeName(cleanName) != "" || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%w: entry %q attempts path traversal", ErrInvalidPackage, name)
		}
		if _, seen := seenEntries[cleanName]; seen {
			return fmt.Errorf("%w: duplicate archive entry %q", ErrInvalidPackage, name)
		}
		seenEntries[cleanName] = struct{}{}

		targetPath := filepath.Join(targetDir, cleanName)
		if !containsPath(targetDir, targetPath) {
			return fmt.Errorf("%w: entry %q escapes destination", ErrInvalidPackage, name)
		}

		if f.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: entry %q is a symlink", ErrInvalidPackage, name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return fmt.Errorf("create directory %q: %w", targetPath, err)
			}
			dirsToChmod = append(dirsToChmod, dirMode{path: targetPath, mode: f.Mode().Perm() | 0o700})
			continue
		}

		if !f.Mode().IsRegular() {
			return fmt.Errorf("%w: entry %q is not a regular file", ErrInvalidPackage, name)
		}

		if cleanName == plugin.ManifestFileName {
			hasManifest = true
		}

		parent := filepath.Dir(targetPath)
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("create directory %q: %w", parent, err)
		}

		if f.UncompressedSize64 > MaxPackageFileSize {
			return fmt.Errorf("%w: entry %q exceeds single-file limit (%d > %d)", ErrInvalidPackage, name, f.UncompressedSize64, MaxPackageFileSize)
		}
		totalBytes += int64(f.UncompressedSize64)
		if totalBytes > MaxPackageTotalBytes {
			return fmt.Errorf("%w: total uncompressed size exceeds limit (%d > %d)", ErrInvalidPackage, totalBytes, MaxPackageTotalBytes)
		}

		if err := extractRegularZipFile(f, targetPath); err != nil {
			return err
		}
	}

	if !hasManifest {
		return fmt.Errorf("%w: archive missing %s", ErrInvalidPackage, plugin.ManifestFileName)
	}

	for i := len(dirsToChmod) - 1; i >= 0; i-- {
		_ = os.Chmod(dirsToChmod[i].path, dirsToChmod[i].mode)
	}

	return nil
}

func extractRegularZipFile(f *zip.File, destination string) (resultErr error) {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("%w: open file %q in archive: %v", ErrInvalidPackage, f.Name, err)
	}
	defer rc.Close()

	mode := f.Mode().Perm()
	if mode&0o111 != 0 {
		mode = 0o755
	} else if mode == 0 {
		mode = 0o644
	} else {
		mode = mode | 0o600
	}

	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create file %q: %w", destination, err)
	}
	defer func() {
		if err := out.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("close file %q: %w", destination, err)
		}
	}()

	written, err := io.Copy(out, io.LimitReader(rc, MaxPackageFileSize+1))
	if err != nil {
		return fmt.Errorf("extract file %q: %w", destination, err)
	}
	if written > MaxPackageFileSize {
		return fmt.Errorf("%w: file %q exceeded size limit during extraction", ErrInvalidPackage, f.Name)
	}

	if err := out.Chmod(mode); err != nil {
		return fmt.Errorf("chmod file %q: %w", destination, err)
	}
	return nil
}

// PackDirectory packages the contents of sourceDir into a .lhplugin ZIP archive at destinationPackage.
func PackDirectory(sourceDir, destinationPackage string) (resultErr error) {
	sourceDir = filepath.Clean(sourceDir)
	info, err := os.Lstat(sourceDir)
	if err != nil {
		return fmt.Errorf("inspect source directory %q: %w", sourceDir, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("source %q must be a directory", sourceDir)
	}

	if err := os.MkdirAll(filepath.Dir(destinationPackage), 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	outFile, err := os.OpenFile(destinationPackage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("create package file %q: %w", destinationPackage, err)
	}
	defer func() {
		if err := outFile.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("close package file: %w", err)
		}
	}()

	zw := zip.NewWriter(outFile)
	defer func() {
		if err := zw.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("close zip writer: %w", err)
		}
	}()

	return filepath.WalkDir(sourceDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == sourceDir {
			return nil
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks not supported in package: %q", path)
		}

		header, err := zip.FileInfoHeader(entryInfo)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(rel)
		if entry.IsDir() {
			header.Name += "/"
			header.Method = zip.Store
		} else {
			header.Method = zip.Deflate
		}

		w, err := zw.CreateHeader(header)
		if err != nil {
			return fmt.Errorf("create zip header %q: %w", header.Name, err)
		}

		if !entry.IsDir() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			if _, err := io.Copy(w, f); err != nil {
				return err
			}
		}
		return nil
	})
}

func copyArtifactTree(source, destination string) error {
	type directoryMode struct {
		path string
		mode fs.FileMode
	}
	directories := make([]directoryMode, 0)
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("artifact path %q escapes source directory", path)
		}
		target := destination
		if relative != "." {
			target = filepath.Join(destination, relative)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect artifact entry %q: %w", path, err)
		}
		if entry.IsDir() {
			if path != source {
				if err := os.Mkdir(target, 0o700); err != nil {
					return fmt.Errorf("create staged directory %q: %w", target, err)
				}
			}
			directories = append(directories, directoryMode{path: target, mode: info.Mode().Perm()})
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("artifact entry must be a regular file: %q", path)
		}
		if err := copyRegularFile(path, target, info.Mode().Perm()); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := os.Chmod(directories[index].path, directories[index].mode); err != nil {
			return fmt.Errorf("preserve artifact directory mode %q: %w", directories[index].path, err)
		}
	}
	return nil
}

func copyRegularFile(source, destination string, mode fs.FileMode) (resultErr error) {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open artifact file %q: %w", source, err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("create staged artifact file %q: %w", destination, err)
	}
	defer func() {
		if err := output.Close(); err != nil && resultErr == nil {
			resultErr = fmt.Errorf("close staged artifact file %q: %w", destination, err)
		}
	}()
	if _, err := io.Copy(output, input); err != nil {
		return fmt.Errorf("copy artifact file %q: %w", source, err)
	}
	if err := output.Chmod(mode); err != nil {
		return fmt.Errorf("preserve artifact file mode %q: %w", destination, err)
	}
	return nil
}

func containsPath(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	return err == nil && relative != "." && relative != ".." && !filepath.IsAbs(relative) && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
