package readcap_test

import (
	"fmt"
	"testing"

	"lum-harbor-notifier/internal/readcap"
)

// TestCoreCapabilitiesExecuteFromHandWrittenJSON proves the M15 Slice 2
// requirement that a future M16 caller needs only a Descriptor's ID plus
// plain JSON text — never readcap.WorkItemReadInput, ResolveExternalInput,
// or SearchInput, and no capability-specific Go decode switch of its own.
// Every JSON literal below is hand-written, not built by marshaling one of
// this package's Go input types.
func TestCoreCapabilitiesExecuteFromHandWrittenJSON(t *testing.T) {
	f := newFixture(t)
	item := f.workItem(t, "Hand-written JSON target")
	externalItem := f.externalPlannerItem(t, "APPLE_REMINDER:instance-1", "reminder-json", "From a plugin")

	t.Run("workitem.read", func(t *testing.T) {
		raw := []byte(fmt.Sprintf(`{"workItemId":%q}`, item.ID))
		result, err := f.readcap.Execute(f.ctx, readcap.Request{CapabilityID: readcap.CapabilityWorkItemRead, Input: raw})
		if err != nil {
			t.Fatal(err)
		}
		output := result.Output.(readcap.WorkItemReadOutput)
		if output.WorkItem.ID != item.ID {
			t.Fatalf("expected WorkItem %q, got %+v", item.ID, output.WorkItem)
		}
	})

	t.Run("entity.resolve_external", func(t *testing.T) {
		raw := []byte(`{"system":"APPLE_REMINDER:instance-1","externalId":"reminder-json"}`)
		result, err := f.readcap.Execute(f.ctx, readcap.Request{CapabilityID: readcap.CapabilityResolveExternal, Input: raw})
		if err != nil {
			t.Fatal(err)
		}
		output := result.Output.(readcap.ResolveExternalOutput)
		if output.EntityID != externalItem.ID {
			t.Fatalf("expected entity %q, got %+v", externalItem.ID, output)
		}
	})

	t.Run("entity.search", func(t *testing.T) {
		raw := []byte(`{"query":"Hand-written JSON","limit":5}`)
		result, err := f.readcap.Execute(f.ctx, readcap.Request{CapabilityID: readcap.CapabilitySearch, Input: raw})
		if err != nil {
			t.Fatal(err)
		}
		output := result.Output.(readcap.SearchOutput)
		if len(output.Candidates) != 1 || output.Candidates[0].EntityID != item.ID {
			t.Fatalf("expected exactly the one matching candidate, got %+v", output.Candidates)
		}
	})
}
