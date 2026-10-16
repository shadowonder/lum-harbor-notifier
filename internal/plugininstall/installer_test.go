package plugininstall_test

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugininstall"
)

func TestInstallPlacesDiscoverableArtifactByManifestIdentity(t *testing.T) {
	sourceParent := t.TempDir()
	source := filepath.Join(sourceParent, "unrelated-source-name")
	writeArtifact(t, source, "company-assistant", "1.2.3", "bin/assistant", true)
	supportPath := filepath.Join(source, "support", "defaults.json")
	if err := os.MkdirAll(filepath.Dir(supportPath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(supportPath, []byte(`{"enabled":true}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(supportPath), 0o550); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Dir(supportPath), 0o750) })
	originalManifest, err := os.ReadFile(filepath.Join(source, plugin.ManifestFileName))
	if err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "configured-plugins")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	registryBefore, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("initial discovery: failures=%v err=%v", failures, err)
	}
	host, err := plugin.NewHost(registryBefore, plugin.HostOptions{
		StartupTimeout: time.Second, RequestTimeout: time.Second, ShutdownTimeout: time.Second,
		Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	installer, err := plugininstall.New(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := installer.Install(source)
	if err != nil {
		t.Fatal(err)
	}
	wantDestination := filepath.Join(root, "company-assistant")
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(wantDestination, "support"), 0o750) })
	if result.ExtensionID != "company-assistant" || result.Version != "1.2.3" || result.Destination != wantDestination || !result.RestartRequired {
		t.Fatalf("unexpected install result: %+v", result)
	}
	if len(registryBefore.List()) != 0 || len(host.Snapshot().Extensions) != 0 {
		t.Fatal("installation mutated the existing Registry or Host")
	}

	registryAfter, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover installed artifact: failures=%v err=%v", failures, err)
	}
	extensions := registryAfter.List()
	if len(extensions) != 1 || extensions[0].Manifest.ID != "company-assistant" {
		t.Fatalf("installed extensions: %+v", extensions)
	}
	executableInfo, err := os.Stat(filepath.Join(wantDestination, "bin", "assistant"))
	if err != nil {
		t.Fatal(err)
	}
	if executableInfo.Mode().Perm() != 0o751 {
		t.Fatalf("executable mode: got %o want 751", executableInfo.Mode().Perm())
	}
	if data, err := os.ReadFile(filepath.Join(wantDestination, "support", "defaults.json")); err != nil || string(data) != `{"enabled":true}` {
		t.Fatalf("support file: data=%q err=%v", data, err)
	}
	if info, err := os.Stat(filepath.Join(wantDestination, "support")); err != nil || info.Mode().Perm() != 0o550 {
		t.Fatalf("support directory mode: info=%v err=%v", info, err)
	}
	if data, err := os.ReadFile(filepath.Join(source, plugin.ManifestFileName)); err != nil || string(data) != string(originalManifest) {
		t.Fatalf("source artifact changed: err=%v", err)
	}
}

func TestInstallRejectsExistingDestinationWithoutMutation(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "company-assistant")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(existing, "keep")
	if err := os.WriteFile(marker, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "artifact")
	writeArtifact(t, source, "company-assistant", "2.0.0", "assistant", true)
	installer, err := plugininstall.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := installer.Install(source); !errors.Is(err, plugininstall.ErrAlreadyInstalled) {
		t.Fatalf("expected already-installed error, got %v", err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "original" {
		t.Fatalf("existing destination changed: data=%q err=%v", data, err)
	}
	assertNoStaging(t, root)
}

func TestConcurrentInstallPublishesOneCompleteArtifactAndCleansStaging(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(t.TempDir(), "artifact")
	writeArtifact(t, source, "company-assistant", "1.0.0", "assistant", true)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		installer, err := plugininstall.New(root)
		if err != nil {
			t.Fatal(err)
		}
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := installer.Install(source)
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	var succeeded, alreadyInstalled int
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, plugininstall.ErrAlreadyInstalled):
			alreadyInstalled++
		default:
			t.Fatalf("unexpected concurrent install error: %v", err)
		}
	}
	if succeeded != 1 || alreadyInstalled != 1 {
		t.Fatalf("concurrent results: succeeded=%d already-installed=%d", succeeded, alreadyInstalled)
	}
	assertNoStaging(t, root)
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 || len(registry.List()) != 1 {
		t.Fatalf("discover concurrently installed artifact: registry=%v failures=%v err=%v", registry, failures, err)
	}
}

func TestInstallRejectsInvalidArtifactBeforeDestinationMutation(t *testing.T) {
	tests := []struct {
		name  string
		build func(*testing.T, string)
	}{
		{
			name: "malformed manifest",
			build: func(t *testing.T, source string) {
				if err := os.MkdirAll(source, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(source, plugin.ManifestFileName), []byte(`{"manifestVersion":1}`), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "missing executable",
			build: func(t *testing.T, source string) {
				writeArtifact(t, source, "company-assistant", "1.0.0", "assistant", false)
			},
		},
		{
			// POSIX executable-bit rejection is Unix/macOS-only: Windows has
			// no such filesystem-permission concept (os.FileInfo.Mode never
			// reports it there), so this invariant is intentionally not
			// enforced by requireExecutablePermission on Windows — see
			// internal/plugininstall/executable_windows.go.
			name: "non executable file",
			build: func(t *testing.T, source string) {
				if runtime.GOOS == "windows" {
					t.Skip("POSIX executable-bit rejection does not apply on Windows")
				}
				writeArtifact(t, source, "company-assistant", "1.0.0", "assistant", true)
				if err := os.Chmod(filepath.Join(source, "assistant"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink in artifact",
			build: func(t *testing.T, source string) {
				writeArtifact(t, source, "company-assistant", "1.0.0", "assistant", true)
				if err := os.Symlink(filepath.Join(source, "assistant"), filepath.Join(source, "alias")); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "artifact")
			test.build(t, source)
			root := filepath.Join(t.TempDir(), "plugins-not-created")
			installer, err := plugininstall.New(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := installer.Install(source); err == nil {
				t.Fatal("expected invalid artifact to be rejected")
			}
			if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid artifact mutated plugin root: %v", err)
			}
		})
	}
}

func TestNewRequiresResolvedAbsolutePluginRoot(t *testing.T) {
	if _, err := plugininstall.New("relative/plugins"); err == nil {
		t.Fatal("expected relative plugin root to be rejected")
	}
}

func TestRemoveDeletesInstalledArtifactByExtensionID(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "artifact")
	writeArtifact(t, source, "company-assistant", "1.0.0", "bin/assistant", true)
	installer, err := plugininstall.New(root)
	if err != nil {
		t.Fatal(err)
	}
	result, err := installer.Install(source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(result.Destination); err != nil {
		t.Fatalf("expected installed artifact present before remove: %v", err)
	}

	if err := installer.Remove("company-assistant"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(result.Destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected artifact removed, stat err: %v", err)
	}
}

func TestRemoveRejectsUnknownExtensionID(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	installer, err := plugininstall.New(root)
	if err != nil {
		t.Fatal(err)
	}
	err = installer.Remove("does-not-exist")
	if !errors.Is(err, plugininstall.ErrNotInstalled) {
		t.Fatalf("expected ErrNotInstalled, got %v", err)
	}
}

func TestRemoveRejectsEmptyExtensionID(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	installer, err := plugininstall.New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := installer.Remove("   "); err == nil {
		t.Fatal("expected empty extension id rejection")
	}
}

func TestRemoveRejectsPathTraversalExtensionID(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	installer, err := plugininstall.New(root)
	if err != nil {
		t.Fatal(err)
	}
	err = installer.Remove("../escaped")
	if err == nil || strings.Contains(err.Error(), "not installed") {
		t.Fatalf("expected path traversal rejection distinct from not-installed, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Dir(root)); statErr != nil {
		t.Fatalf("parent directory unexpectedly affected: %v", statErr)
	}
}

func TestRemovePreservesUnrelatedInstalledExtensions(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	installer, err := plugininstall.New(root)
	if err != nil {
		t.Fatal(err)
	}
	sourceA := filepath.Join(t.TempDir(), "artifact-a")
	writeArtifact(t, sourceA, "extension-a", "1.0.0", "bin/a", true)
	sourceB := filepath.Join(t.TempDir(), "artifact-b")
	writeArtifact(t, sourceB, "extension-b", "1.0.0", "bin/b", true)
	if _, err := installer.Install(sourceA); err != nil {
		t.Fatal(err)
	}
	resultB, err := installer.Install(sourceB)
	if err != nil {
		t.Fatal(err)
	}

	if err := installer.Remove("extension-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "extension-a")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected extension-a removed: %v", err)
	}
	if _, err := os.Stat(resultB.Destination); err != nil {
		t.Fatalf("expected extension-b preserved: %v", err)
	}
}

func writeArtifact(t *testing.T, directory, id, version, executable string, writeExecutable bool) {
	t.Helper()
	manifest := plugin.Manifest{
		ManifestVersion: plugin.SupportedManifestVersion,
		ID:              id, Name: "Test Plugin", Version: version,
		ProtocolVersion: plugin.SupportedProtocolVersion,
		Executable:      executable,
		Capabilities:    []plugin.CapabilityDeclaration{{ID: plugin.CapabilitySourceFetch}},
		Configuration:   plugin.PluginConfigSchema{Version: 1},
	}
	writeArtifactWithManifest(t, directory, manifest, writeExecutable)
}

func writeArtifactWithManifest(t *testing.T, directory string, manifest plugin.Manifest, writeExecutable bool) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, filepath.Dir(manifest.Executable)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, plugin.ManifestFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if writeExecutable {
		if err := os.WriteFile(filepath.Join(directory, manifest.Executable), []byte("#!/bin/sh\nexit 0\n"), 0o751); err != nil {
			t.Fatal(err)
		}
	}
}

func assertNoStaging(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".install-") {
			t.Fatalf("staging directory remained: %s", entry.Name())
		}
	}
}

func TestInstallPackagedPluginArchive(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-plugin")
	manifest := plugin.Manifest{
		PackageFormatVersion: 1,
		OS:                   []string{runtime.GOOS},
		Arch:                 []string{runtime.GOARCH},
		ManifestVersion:      plugin.SupportedManifestVersion,
		ID:                   "lh-plugin-packaged",
		Name:                 "Packaged Plugin",
		Version:              "1.0.0",
		ProtocolVersion:      plugin.SupportedProtocolVersion,
		Executable:           "bin/run",
		Capabilities:         []plugin.CapabilityDeclaration{{ID: plugin.CapabilitySourceFetch}},
		Configuration:        plugin.PluginConfigSchema{Version: 1},
	}
	writeArtifactWithManifest(t, sourceDir, manifest, true)

	pkgPath := filepath.Join(t.TempDir(), "lh-plugin-packaged-1.0.0.lhplugin")
	if err := plugininstall.PackDirectory(sourceDir, pkgPath); err != nil {
		t.Fatalf("PackDirectory: %v", err)
	}

	root := filepath.Join(t.TempDir(), "configured-plugins")
	installer, err := plugininstall.New(root)
	if err != nil {
		t.Fatal(err)
	}

	result, err := installer.Install(pkgPath)
	if err != nil {
		t.Fatalf("Install package: %v", err)
	}
	if result.ExtensionID != "lh-plugin-packaged" || result.Version != "1.0.0" || !result.RestartRequired {
		t.Fatalf("unexpected install result: %+v", result)
	}

	// Verify discovery
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("Discover: failures=%v err=%v", failures, err)
	}
	exts := registry.List()
	if len(exts) != 1 || exts[0].Manifest.ID != "lh-plugin-packaged" {
		t.Fatalf("unexpected discovery: %+v", exts)
	}
	assertNoStaging(t, root)
}

func TestInstallPackageRejectsPathTraversal(t *testing.T) {
	pkgPath := filepath.Join(t.TempDir(), "malicious.lhplugin")
	f, err := os.Create(pkgPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)

	// Add plugin.json
	manifest := plugin.Manifest{
		ManifestVersion: plugin.SupportedManifestVersion,
		ID:              "lh-plugin-traversal",
		Name:            "Traversal",
		Version:         "1.0.0",
		ProtocolVersion: plugin.SupportedProtocolVersion,
		Executable:      "run",
		Capabilities:    []plugin.CapabilityDeclaration{{ID: plugin.CapabilitySourceFetch}},
		Configuration:   plugin.PluginConfigSchema{Version: 1},
	}
	mData, _ := json.Marshal(manifest)
	mw, err := zw.Create(plugin.ManifestFileName)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = mw.Write(mData)

	// Add path traversal entry
	tw, err := zw.Create("../escaped.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write([]byte("malicious"))

	_ = zw.Close()
	_ = f.Close()

	root := filepath.Join(t.TempDir(), "configured-plugins")
	installer, err := plugininstall.New(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = installer.Install(pkgPath)
	if !errors.Is(err, plugininstall.ErrInvalidPackage) {
		t.Fatalf("expected ErrInvalidPackage for path traversal, got %v", err)
	}
	assertNoStaging(t, root)
}

func TestInstallPackageRejectsSymlink(t *testing.T) {
	pkgPath := filepath.Join(t.TempDir(), "symlink.lhplugin")
	f, err := os.Create(pkgPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)

	header := &zip.FileHeader{
		Name: "link-to-etc",
	}
	header.SetMode(os.ModeSymlink | 0o777)
	w, err := zw.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("/etc/passwd"))

	_ = zw.Close()
	_ = f.Close()

	root := filepath.Join(t.TempDir(), "configured-plugins")
	installer, err := plugininstall.New(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = installer.Install(pkgPath)
	if !errors.Is(err, plugininstall.ErrInvalidPackage) {
		t.Fatalf("expected ErrInvalidPackage for symlink, got %v", err)
	}
	assertNoStaging(t, root)
}

func TestInstallPackageRejectsMissingManifest(t *testing.T) {
	pkgPath := filepath.Join(t.TempDir(), "nomanifest.lhplugin")
	f, err := os.Create(pkgPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("somefile.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("content"))
	_ = zw.Close()
	_ = f.Close()

	root := filepath.Join(t.TempDir(), "configured-plugins")
	installer, err := plugininstall.New(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = installer.Install(pkgPath)
	if !errors.Is(err, plugininstall.ErrInvalidPackage) {
		t.Fatalf("expected ErrInvalidPackage for missing manifest, got %v", err)
	}
	assertNoStaging(t, root)
}

func TestInstallPackageRejectsIncompatibleOS(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-plugin")
	manifest := plugin.Manifest{
		OS:              []string{"windows-fake-os"},
		ManifestVersion: plugin.SupportedManifestVersion,
		ID:              "lh-plugin-os-test",
		Name:            "OS Test",
		Version:         "1.0.0",
		ProtocolVersion: plugin.SupportedProtocolVersion,
		Executable:      "run",
		Capabilities:    []plugin.CapabilityDeclaration{{ID: plugin.CapabilitySourceFetch}},
		Configuration:   plugin.PluginConfigSchema{Version: 1},
	}
	writeArtifactWithManifest(t, sourceDir, manifest, true)

	pkgPath := filepath.Join(t.TempDir(), "os-test.lhplugin")
	if err := plugininstall.PackDirectory(sourceDir, pkgPath); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "configured-plugins")
	installer, err := plugininstall.New(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = installer.Install(pkgPath)
	if !errors.Is(err, plugininstall.ErrIncompatiblePlatform) {
		t.Fatalf("expected ErrIncompatiblePlatform, got %v", err)
	}
	assertNoStaging(t, root)
}

func TestInstallPackageRejectsIncompatibleArch(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-plugin")
	manifest := plugin.Manifest{
		Arch:            []string{"mips64"},
		ManifestVersion: plugin.SupportedManifestVersion,
		ID:              "lh-plugin-arch-test",
		Name:            "Arch Test",
		Version:         "1.0.0",
		ProtocolVersion: plugin.SupportedProtocolVersion,
		Executable:      "run",
		Capabilities:    []plugin.CapabilityDeclaration{{ID: plugin.CapabilitySourceFetch}},
		Configuration:   plugin.PluginConfigSchema{Version: 1},
	}
	writeArtifactWithManifest(t, sourceDir, manifest, true)

	pkgPath := filepath.Join(t.TempDir(), "arch-test.lhplugin")
	if err := plugininstall.PackDirectory(sourceDir, pkgPath); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "configured-plugins")
	installer, err := plugininstall.New(root)
	if err != nil {
		t.Fatal(err)
	}

	_, err = installer.Install(pkgPath)
	if !errors.Is(err, plugininstall.ErrIncompatiblePlatform) {
		t.Fatalf("expected ErrIncompatiblePlatform, got %v", err)
	}
	assertNoStaging(t, root)
}

func TestInstallPackageRejectsDuplicate(t *testing.T) {
	sourceDir := filepath.Join(t.TempDir(), "source-plugin")
	manifest := plugin.Manifest{
		ManifestVersion: plugin.SupportedManifestVersion,
		ID:              "lh-plugin-dup",
		Name:            "Dup Plugin",
		Version:         "1.0.0",
		ProtocolVersion: plugin.SupportedProtocolVersion,
		Executable:      "run",
		Capabilities:    []plugin.CapabilityDeclaration{{ID: plugin.CapabilitySourceFetch}},
		Configuration:   plugin.PluginConfigSchema{Version: 1},
	}
	writeArtifactWithManifest(t, sourceDir, manifest, true)

	pkgPath := filepath.Join(t.TempDir(), "dup.lhplugin")
	if err := plugininstall.PackDirectory(sourceDir, pkgPath); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join(t.TempDir(), "configured-plugins")
	installer, err := plugininstall.New(root)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := installer.Install(pkgPath); err != nil {
		t.Fatalf("initial install failed: %v", err)
	}

	_, err = installer.Install(pkgPath)
	if !errors.Is(err, plugininstall.ErrAlreadyInstalled) {
		t.Fatalf("expected ErrAlreadyInstalled, got %v", err)
	}
	assertNoStaging(t, root)
}

func TestExtractPackage_DuplicateArchiveEntry(t *testing.T) {
	pkgPath := filepath.Join(t.TempDir(), "duplicate-entry.lhplugin")
	f, err := os.Create(pkgPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w1, _ := zw.Create("file.txt")
	_, _ = w1.Write([]byte("content1"))
	w2, _ := zw.Create("file.txt")
	_, _ = w2.Write([]byte("content2"))
	zw.Close()
	f.Close()

	targetDir := t.TempDir()
	err = plugininstall.ExtractPackage(pkgPath, targetDir)
	if err == nil || !errors.Is(err, plugininstall.ErrInvalidPackage) {
		t.Fatalf("expected ErrInvalidPackage for duplicate entry, got %v", err)
	}
}

func TestExtractPackage_DriveLetterPath(t *testing.T) {
	pkgPath := filepath.Join(t.TempDir(), "drive-letter.lhplugin")
	f, err := os.Create(pkgPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w1, _ := zw.Create("C:evil.txt")
	_, _ = w1.Write([]byte("evil"))
	zw.Close()
	f.Close()

	targetDir := t.TempDir()
	err = plugininstall.ExtractPackage(pkgPath, targetDir)
	if err == nil || !errors.Is(err, plugininstall.ErrInvalidPackage) {
		t.Fatalf("expected ErrInvalidPackage for drive letter path, got %v", err)
	}
}

func TestRemove_InvalidExtensionID(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugins")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	installer, err := plugininstall.New(root)
	if err != nil {
		t.Fatal(err)
	}

	invalidIDs := []string{".", "..", "foo/bar", "foo\\bar", "../escaped", "has space"}
	for _, id := range invalidIDs {
		if err := installer.Remove(id); err == nil {
			t.Errorf("expected error for invalid extension ID %q, got nil", id)
		}
	}
	// Verify root directory still exists and was not removed
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("root directory was improperly removed: %v", err)
	}
}

func TestShippedPluginPlatformMetadataAndNegativeCompatibility(t *testing.T) {
	manifests := []struct {
		id   string
		path string
	}{
		{id: "lh-plugin-ai-runtime", path: filepath.Join("..", "..", "..", "lum-harbor-plugins", "lh-plugin-ai-runtime", "plugin.json")},
		{id: "lh-plugin-synthetic-project", path: filepath.Join("..", "..", "..", "lum-harbor-plugins", "lh-plugin-synthetic-project", "plugin.json")},
		{id: "lh-plugin-apple-reminders", path: filepath.Join("..", "..", "..", "lum-harbor-plugins", "lh-plugin-apple-reminders", "plugin.json")},
		{id: "lh-plugin-macos-notifications", path: filepath.Join("..", "..", "..", "lum-harbor-plugins", "lh-plugin-macos-notifications", "plugin.json")},
	}

	for _, tc := range manifests {
		t.Run(tc.id, func(t *testing.T) {
			data, err := os.ReadFile(tc.path)
			if errors.Is(err, os.ErrNotExist) {
				t.Skipf("shipped manifest not found in external plugin repository: %s", tc.path)
			}
			if err != nil {
				t.Fatalf("read manifest %s: %v", tc.path, err)
			}
			var m plugin.Manifest
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatalf("unmarshal manifest %s: %v", tc.path, err)
			}

			// Truthfulness check: manifest MUST NOT omit OS or Arch (must not be treated as portable)
			if len(m.OS) == 0 {
				t.Fatalf("manifest %s must not omit OS (omission treats package as portable to any OS)", tc.id)
			}
			if len(m.Arch) == 0 {
				t.Fatalf("manifest %s must not omit Arch (omission treats package as portable to any architecture)", tc.id)
			}
			if len(m.OS) != 1 || m.OS[0] != "darwin" {
				t.Fatalf("manifest %s expected OS [darwin], got %v", tc.id, m.OS)
			}
			if len(m.Arch) != 1 || m.Arch[0] != "arm64" {
				t.Fatalf("manifest %s expected Arch [arm64], got %v", tc.id, m.Arch)
			}

			// Positive install check: matching platform (darwin arm64) installs successfully
			sourceDir := filepath.Join(t.TempDir(), tc.id+"-src")
			writeArtifactWithManifest(t, sourceDir, m, true)
			pkgPath := filepath.Join(t.TempDir(), tc.id+".lhplugin")
			if err := plugininstall.PackDirectory(sourceDir, pkgPath); err != nil {
				t.Fatal(err)
			}
			installer, err := plugininstall.New(filepath.Join(t.TempDir(), "plugins"))
			if err != nil {
				t.Fatal(err)
			}
			res, err := installer.Install(pkgPath)
			if err != nil {
				t.Fatalf("expected manifest %s to install on darwin/arm64, got %v", tc.id, err)
			}
			if res.ExtensionID != tc.id {
				t.Fatalf("expected extension id %s, got %s", tc.id, res.ExtensionID)
			}

			// Negative compatibility proof:
			// A Darwin-only package cannot be accepted as Windows-compatible or portable.
			// When the declared OS is incompatible with the host system, the installer
			// fails closed with ErrIncompatiblePlatform.
			incompatManifest := m
			incompatManifest.OS = []string{"windows"}
			incompatSource := filepath.Join(t.TempDir(), tc.id+"-incompat-src")
			writeArtifactWithManifest(t, incompatSource, incompatManifest, true)
			incompatPkg := filepath.Join(t.TempDir(), tc.id+"-incompat.lhplugin")
			if err := plugininstall.PackDirectory(incompatSource, incompatPkg); err != nil {
				t.Fatal(err)
			}
			incompatInstaller, err := plugininstall.New(filepath.Join(t.TempDir(), "plugins-incompat"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = incompatInstaller.Install(incompatPkg)
			if !errors.Is(err, plugininstall.ErrIncompatiblePlatform) {
				t.Fatalf("expected ErrIncompatiblePlatform for incompatible OS on %s, got %v", tc.id, err)
			}
		})
	}
}
