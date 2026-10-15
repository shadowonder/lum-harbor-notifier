//go:build windows

package plugininstall

import "io/fs"

// requireExecutablePermission is intentionally a no-op on Windows.
// os.FileInfo.Mode() never reports POSIX executable bits for Windows files
// (Windows has no such concept at the filesystem-permission level: a
// regular file's mode is derived only from the FILE_ATTRIBUTE_READONLY
// flag, so Perm()&0o111 is always 0 for a file, executable or not).
// Applying the Unix rule here would deterministically reject every
// Windows plugin artifact regardless of validity.
//
// The meaningful invariants are preserved elsewhere and unconditionally
// for every platform: the caller already required a regular file, and
// plugin.LoadExtension already required the manifest-declared executable
// to exist and stay inside the extension directory after symlink/reparse
// resolution. Whether the file is genuinely launchable is proven honestly
// at process-start time by exec.Cmd.Start, which fails explicitly if the
// target is not a real Windows executable image.
func requireExecutablePermission(fs.FileInfo, string) error { return nil }
