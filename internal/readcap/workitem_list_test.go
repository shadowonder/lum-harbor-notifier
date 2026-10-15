package readcap_test

import (
	"errors"
	"testing"

	"lum-harbor-notifier/internal/readcap"
)

func TestWorkItemListReturnsBoundedItems(t *testing.T) {
	f := newFixture(t)
	f.workItem(t, "Item Alpha")
	f.workItem(t, "Item Beta")
	f.workItem(t, "Item Gamma")

	// 1. List with default limit
	result, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityWorkItemList,
		Input:        mustEncode(t, readcap.WorkItemListInput{}),
	})
	if err != nil {
		t.Fatal(err)
	}

	output, ok := result.Output.(readcap.WorkItemListOutput)
	if !ok {
		t.Fatalf("expected WorkItemListOutput, got %T", result.Output)
	}
	if output.Total != 3 {
		t.Fatalf("expected total 3, got %d", output.Total)
	}
	if len(output.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(output.Items))
	}

	// 2. List with custom limit
	limit := 2
	result, err = f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityWorkItemList,
		Input:        mustEncode(t, readcap.WorkItemListInput{Limit: &limit}),
	})
	if err != nil {
		t.Fatal(err)
	}
	output = result.Output.(readcap.WorkItemListOutput)
	if output.Total != 3 {
		t.Fatalf("expected total 3, got %d", output.Total)
	}
	if len(output.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(output.Items))
	}

	// 3. List with empty input "{}"
	result, err = f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityWorkItemList,
		Input:        []byte(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	output = result.Output.(readcap.WorkItemListOutput)
	if output.Total != 3 {
		t.Fatalf("expected total 3, got %d", output.Total)
	}

	// 4. Invalid non-positive limit rejected
	badLimit := 0
	_, err = f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityWorkItemList,
		Input:        mustEncode(t, readcap.WorkItemListInput{Limit: &badLimit}),
	})
	if !errors.Is(err, readcap.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for limit 0, got %v", err)
	}
}
