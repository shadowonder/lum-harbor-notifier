package platform

import (
	"runtime"
	"testing"
)

func TestFromGOOSMapsDarwinToMacOS(t *testing.T) {
	if got := FromGOOS("darwin"); got != MacOS {
		t.Fatalf("FromGOOS(darwin) = %q, want %q", got, MacOS)
	}
}

func TestFromGOOSMapsWindowsToWindows(t *testing.T) {
	if got := FromGOOS("windows"); got != Windows {
		t.Fatalf("FromGOOS(windows) = %q, want %q", got, Windows)
	}
}

func TestFromGOOSMapsUnknownToOther(t *testing.T) {
	for _, goos := range []string{"linux", "freebsd", "js", "", "DARWIN", "Windows"} {
		if got := FromGOOS(goos); got != Other {
			t.Fatalf("FromGOOS(%q) = %q, want %q", goos, got, Other)
		}
	}
}

// TestCurrentMatchesActualBuildTarget locks Current() to whatever GOOS this
// test binary was actually built for, so it stays meaningful across the
// macOS/Windows cross-compiled targets this project builds for, without
// hardcoding an assumption about which host runs the test suite.
func TestCurrentMatchesActualBuildTarget(t *testing.T) {
	if got, want := Current(), FromGOOS(runtime.GOOS); got != want {
		t.Fatalf("Current() = %q, want %q (runtime.GOOS=%q)", got, want, runtime.GOOS)
	}
}
