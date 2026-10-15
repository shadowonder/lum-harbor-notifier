package readcap_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestReadcapDoesNotImportAI guards the M15 Slice 1 boundary: readcap is a
// bounded read seam over existing domain services, never an AI/tool-calling
// integration. If a future change makes readcap import internal/ai (directly
// or transitively), that is exactly the "AI feature creep" this Slice
// explicitly deferred to M16, and this test must fail loudly.
func TestReadcapDoesNotImportAI(t *testing.T) {
	assertNoDependency(t, "lum-harbor-notifier/internal/readcap", "lum-harbor-notifier/internal/ai")
}

// TestReadcapDoesNotImportAssistant guards the architectural boundary: readcap
// is a passive read provider seam and must never depend on internal/assistant.
// Assistant consumes readcap in M16 Slice 2, but readcap never imports Assistant.
func TestReadcapDoesNotImportAssistant(t *testing.T) {
	assertNoDependency(t, "lum-harbor-notifier/internal/readcap", "lum-harbor-notifier/internal/assistant")
}

// TestReadcapDoesNotImportProposal guards the readcap boundary: readcap only
// performs read-only operations and must never depend on the mutation Proposal subsystem.
func TestReadcapDoesNotImportProposal(t *testing.T) {
	assertNoDependency(t, "lum-harbor-notifier/internal/readcap", "lum-harbor-notifier/internal/proposal")
}

func assertNoDependency(t *testing.T, pkg, forbidden string) {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", pkg).Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}
	deps := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, dep := range deps {
		if dep == forbidden {
			t.Fatalf("%s must not depend on %s (found in `go list -deps` output)", pkg, forbidden)
		}
	}
}
