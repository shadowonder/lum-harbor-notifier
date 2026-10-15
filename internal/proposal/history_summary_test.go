package proposal

import "testing"

func TestProposalHistorySummaryNamesTheMutatedDomain(t *testing.T) {
	cases := map[string]string{
		OperationTypePlannerItemCreate:     "Proposal applied to Planner",
		OperationTypeTrackedItemCreate:     "Proposal applied to Tracking",
		OperationTypeWorkflowCreate:        "Proposal applied to Tracking",
		OperationTypeWorkItemUpdateDetails: "Proposal applied to Work Item",
		OperationTypeWorkItemSetLifecycle:  "Proposal applied to Work Item",
	}
	for operationType, want := range cases {
		got := proposalTransitionSummary(&Proposal{OperationType: operationType, Status: StatusApplied})
		if got != want {
			t.Errorf("%s: got %q, want %q", operationType, got, want)
		}
	}
}
