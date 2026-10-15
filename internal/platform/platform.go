// Package platform owns the single authoritative fact "what operating system
// is this process running on" for the frontend. It deliberately exposes
// nothing else (no architecture, kernel version, hostname, username, machine
// ID, or OS build number) because no current caller needs it — Settings only
// needs to decide whether to show a macOS-specific credential option instead
// of guessing from browser/renderer heuristics.
package platform

import "runtime"

// The three values this application currently distinguishes. Product target
// baseline is macOS and Windows 11 x64; every other GOOS reports Other rather
// than failing or guessing.
const (
	MacOS   = "MACOS"
	Windows = "WINDOWS"
	Other   = "OTHER"
)

// FromGOOS is the pure, directly-testable mapping this package is built
// around — production code should normally call Current() instead.
func FromGOOS(goos string) string {
	switch goos {
	case "darwin":
		return MacOS
	case "windows":
		return Windows
	default:
		return Other
	}
}

// Current returns this running process's platform fact.
func Current() string {
	return FromGOOS(runtime.GOOS)
}
