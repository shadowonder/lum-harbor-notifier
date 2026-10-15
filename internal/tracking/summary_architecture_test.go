package tracking

import (
	"os"
	"strings"
	"testing"
)

func TestWorkItemSummaryReadUsesFixedSetQueries(t *testing.T) {
	source, err := os.ReadFile("summary.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	expected := []string{
		"ListWorkflowsForWorkItemOverview", "ListWorkflowDefinitionsForOverview",
		"ListTrackedItemsForWorkItemOverview", "ListWorkflowStepsForWorkItemOverview",
		"ListStepStatesForWorkItemOverview", "ListMonitorPointsForOverview",
	}
	for _, method := range expected {
		if strings.Count(text, "r.queries."+method+"(ctx)") != 1 {
			t.Fatalf("overview summary must call fixed query %s exactly once", method)
		}
	}
	if strings.Count(text, "r.queries.") != len(expected) {
		t.Fatalf("overview summary query set changed: expected %d fixed calls", len(expected))
	}
}
