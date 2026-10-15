package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/proposal"
)

func TestParseClockTime_Formats(t *testing.T) {
	cases := []struct {
		input       string
		expectedH   int
		expectedM   int
		expectedS   int
		expectError bool
	}{
		{input: "15:00", expectedH: 15, expectedM: 0, expectedS: 0},
		{input: "15:04:05", expectedH: 15, expectedM: 4, expectedS: 5},
		{input: "3:00 PM", expectedH: 15, expectedM: 0, expectedS: 0},
		{input: "3:00PM", expectedH: 15, expectedM: 0, expectedS: 0},
		{input: "3:00 pm", expectedH: 15, expectedM: 0, expectedS: 0},
		{input: "3 PM", expectedH: 15, expectedM: 0, expectedS: 0},
		{input: "3pm", expectedH: 15, expectedM: 0, expectedS: 0},
		{input: "9:30 AM", expectedH: 9, expectedM: 30, expectedS: 0},
		{input: "9:30am", expectedH: 9, expectedM: 30, expectedS: 0},
		{input: "09:00", expectedH: 9, expectedM: 0, expectedS: 0},
		{input: "15", expectedH: 15, expectedM: 0, expectedS: 0},
		{input: "afternoon", expectError: true},
		{input: "morning", expectError: true},
		{input: "later", expectError: true},
		{input: "", expectError: true},
	}

	for _, tc := range cases {
		h, m, s, err := parseClockTime(tc.input)
		if tc.expectError {
			if err == nil {
				t.Errorf("parseClockTime(%q): expected error, got %02d:%02d:%02d", tc.input, h, m, s)
			}
		} else {
			if err != nil {
				t.Errorf("parseClockTime(%q) unexpected error: %v", tc.input, err)
			}
			if h != tc.expectedH || m != tc.expectedM || s != tc.expectedS {
				t.Errorf("parseClockTime(%q) = %02d:%02d:%02d, want %02d:%02d:%02d", tc.input, h, m, s, tc.expectedH, tc.expectedM, tc.expectedS)
			}
		}
	}
}

func TestResolveLocation_Cases(t *testing.T) {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Nil or empty defaults to defaultLoc
	loc, err := resolveLocation(nil, chicago)
	if err != nil || loc != chicago {
		t.Errorf("resolveLocation(nil): got %v, err %v", loc, err)
	}

	empty := "  "
	loc, err = resolveLocation(&empty, chicago)
	if err != nil || loc != chicago {
		t.Errorf("resolveLocation(empty): got %v, err %v", loc, err)
	}

	// 2. Named timezones
	eastern := "Eastern"
	loc, err = resolveLocation(&eastern, chicago)
	if err != nil || loc.String() != "America/New_York" {
		t.Errorf("resolveLocation(Eastern): got %v, err %v", loc, err)
	}

	central := "cst"
	loc, err = resolveLocation(&central, chicago)
	if err != nil || loc.String() != "America/Chicago" {
		t.Errorf("resolveLocation(cst): got %v, err %v", loc, err)
	}

	utc := "UTC"
	loc, err = resolveLocation(&utc, chicago)
	if err != nil || loc != time.UTC {
		t.Errorf("resolveLocation(UTC): got %v, err %v", loc, err)
	}

	// 3. Fixed offset
	offset := "-04:00"
	loc, err = resolveLocation(&offset, chicago)
	if err != nil {
		t.Errorf("resolveLocation(-04:00) error: %v", err)
	} else {
		now := time.Date(2026, 9, 24, 15, 0, 0, 0, loc)
		_, off := now.Zone()
		if off != -4*3600 {
			t.Errorf("expected -14400s offset, got %d", off)
		}
	}

	// 4. Invalid timezone
	invalid := "MartianStandardTime"
	_, err = resolveLocation(&invalid, chicago)
	if err == nil {
		t.Errorf("resolveLocation(invalid): expected error, got nil")
	}
}

func TestCombineDateAndTime_Cases(t *testing.T) {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}

	// Case A: Date + local clock time (no timezone specified -> uses defaultLoc)
	res, err := combineDateAndTime("2026-09-24", "15:00", nil, chicago)
	if err != nil {
		t.Fatalf("combineDateAndTime: %v", err)
	}
	// On Sep 24, Chicago is CDT (-05:00)
	if res != "2026-09-24T15:00:00-05:00" {
		t.Errorf("expected 2026-09-24T15:00:00-05:00, got %s", res)
	}

	// Case B: Date + explicit Eastern timezone
	tzEastern := "Eastern"
	res, err = combineDateAndTime("2026-09-24", "3:00 PM", &tzEastern, chicago)
	if err != nil {
		t.Fatalf("combineDateAndTime with Eastern: %v", err)
	}
	// On Sep 24, New York is EDT (-04:00)
	if res != "2026-09-24T15:00:00-04:00" {
		t.Errorf("expected 2026-09-24T15:00:00-04:00, got %s", res)
	}

	// Case C: Date + explicit UTC offset
	tzOffset := "+08:00"
	res, err = combineDateAndTime("2026-09-24", "15:00", &tzOffset, chicago)
	if err != nil {
		t.Fatalf("combineDateAndTime with offset: %v", err)
	}
	if res != "2026-09-24T15:00:00+08:00" {
		t.Errorf("expected 2026-09-24T15:00:00+08:00, got %s", res)
	}

	// Case D: Ambiguous time returns error
	_, err = combineDateAndTime("2026-09-24", "afternoon", nil, chicago)
	if err == nil {
		t.Errorf("expected error for ambiguous clock time, got nil")
	}
}

func TestNormalizeDateOnly(t *testing.T) {
	cases := []struct {
		input    string
		expected string
		hasErr   bool
	}{
		{input: "2026-09-24", expected: "2026-09-24"},
		{input: "2026-09-24T15:00:00-05:00", expected: "2026-09-24"},
		{input: "2026-09-24 15:00:00", expected: "2026-09-24"},
		{input: "invalid", hasErr: true},
	}
	for _, tc := range cases {
		out, err := normalizeDateOnly(tc.input)
		if tc.hasErr {
			if err == nil {
				t.Errorf("normalizeDateOnly(%q): expected error, got %s", tc.input, out)
			}
		} else {
			if err != nil || out != tc.expected {
				t.Errorf("normalizeDateOnly(%q) = %q, want %q (err: %v)", tc.input, out, tc.expected, err)
			}
		}
	}
}

func TestNormalizeRFC3339(t *testing.T) {
	chicago, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatal(err)
	}

	// RFC3339 with offset passes through
	s := "2026-09-24T15:00:00-05:00"
	out, err := normalizeRFC3339(s, chicago)
	if err != nil || out != s {
		t.Errorf("normalizeRFC3339(%q) = %q, want %q (err: %v)", s, out, s, err)
	}

	// ISO without offset is parsed in default location
	sNoOffset := "2026-09-24T15:00:00"
	out, err = normalizeRFC3339(sNoOffset, chicago)
	if err != nil || !strings.Contains(out, "2026-09-24T15:00:00") {
		t.Errorf("normalizeRFC3339(%q) = %q (err: %v)", sNoOffset, out, err)
	}
}

func TestProposeRegistry_PlannerItemCreate_NormalizationScenarios(t *testing.T) {
	app := newRegistryTestApp(t)
	registry := NewProposeRegistry()
	ctx := context.Background()

	// Scenario 1: Date only -> DueDate populated, DueAt nil
	p, err := registry.Execute(ctx, app, proposal.OperationTypePlannerItemCreate, nil, json.RawMessage(`{
		"title": "Submit expense report",
		"due_date": "2026-09-24"
	}`), nil)
	if err != nil {
		t.Fatalf("Dispatch DateOnly failed: %v", err)
	}
	if p.PlannerItemPayload == nil {
		t.Fatalf("missing PlannerItemPayload")
	}
	if p.PlannerItemPayload.DueDate == nil || *p.PlannerItemPayload.DueDate != "2026-09-24" {
		t.Errorf("expected DueDate=2026-09-24, got %v", p.PlannerItemPayload.DueDate)
	}
	if p.PlannerItemPayload.DueAt != nil {
		t.Errorf("expected DueAt=nil for date-only, got %v", *p.PlannerItemPayload.DueAt)
	}

	// Scenario 2: Date + local time -> DueAt populated, DueDate nil (no conflict)
	p, err = registry.Execute(ctx, app, proposal.OperationTypePlannerItemCreate, nil, json.RawMessage(`{
		"title": "Buy groceries",
		"due_date": "2026-09-24",
		"due_time": "15:00"
	}`), nil)
	if err != nil {
		t.Fatalf("Dispatch Date+Time failed: %v", err)
	}
	if p.PlannerItemPayload.DueAt == nil || !strings.HasPrefix(*p.PlannerItemPayload.DueAt, "2026-09-24T15:00:00") {
		t.Errorf("expected DueAt starting with 2026-09-24T15:00:00, got %v", p.PlannerItemPayload.DueAt)
	}
	if p.PlannerItemPayload.DueDate != nil {
		t.Errorf("expected DueDate=nil when DueAt is present, got %v", *p.PlannerItemPayload.DueDate)
	}

	// Scenario 3: Date + explicit Eastern timezone -> DueAt with -04:00 (EDT), DueDate nil
	p, err = registry.Execute(ctx, app, proposal.OperationTypePlannerItemCreate, nil, json.RawMessage(`{
		"title": "Call London",
		"due_date": "2026-09-24",
		"due_time": "3 PM",
		"timezone": "Eastern"
	}`), nil)
	if err != nil {
		t.Fatalf("Dispatch Date+Time+Timezone failed: %v", err)
	}
	if p.PlannerItemPayload.DueAt == nil || *p.PlannerItemPayload.DueAt != "2026-09-24T15:00:00-04:00" {
		t.Errorf("expected DueAt=2026-09-24T15:00:00-04:00, got %v", p.PlannerItemPayload.DueAt)
	}
	if p.PlannerItemPayload.DueDate != nil {
		t.Errorf("expected DueDate=nil when DueAt is present, got %v", *p.PlannerItemPayload.DueDate)
	}

	// Scenario 4: Notes and details alias support
	p, err = registry.Execute(ctx, app, proposal.OperationTypePlannerItemCreate, nil, json.RawMessage(`{
		"title": "Check inbox",
		"notes": "Urgent emails only"
	}`), nil)
	if err != nil {
		t.Fatalf("Dispatch with notes failed: %v", err)
	}
	if p.PlannerItemPayload.Details == nil || *p.PlannerItemPayload.Details != "Urgent emails only" {
		t.Errorf("expected Details='Urgent emails only', got %v", p.PlannerItemPayload.Details)
	}

	// Details alias mapped to notes in ValidateArguments
	p, err = registry.Execute(ctx, app, proposal.OperationTypePlannerItemCreate, nil, json.RawMessage(`{
		"title": "Check inbox details",
		"details": "Secondary details"
	}`), nil)
	if err != nil {
		t.Fatalf("Dispatch with details alias failed: %v", err)
	}
	if p.PlannerItemPayload.Details == nil || *p.PlannerItemPayload.Details != "Secondary details" {
		t.Errorf("expected Details='Secondary details', got %v", p.PlannerItemPayload.Details)
	}

	// Scenario 5: Ambiguous clock time rejected
	_, err = registry.Execute(ctx, app, proposal.OperationTypePlannerItemCreate, nil, json.RawMessage(`{
		"title": "Review docs",
		"due_date": "2026-09-24",
		"due_time": "afternoon"
	}`), nil)
	if err == nil {
		t.Errorf("expected dispatch to fail on ambiguous clock time, got nil")
	}

	// Scenario 6: Backward compatibility for direct due_at
	p, err = registry.Execute(ctx, app, proposal.OperationTypePlannerItemCreate, nil, json.RawMessage(`{
		"title": "Doctor appointment",
		"due_at": "2026-09-24T15:00:00-05:00"
	}`), nil)
	if err != nil {
		t.Fatalf("Dispatch direct due_at failed: %v", err)
	}
	if p.PlannerItemPayload.DueAt == nil || *p.PlannerItemPayload.DueAt != "2026-09-24T15:00:00-05:00" {
		t.Errorf("expected DueAt=2026-09-24T15:00:00-05:00, got %v", p.PlannerItemPayload.DueAt)
	}
	if p.PlannerItemPayload.DueDate != nil {
		t.Errorf("expected DueDate=nil when direct DueAt is passed, got %v", *p.PlannerItemPayload.DueDate)
	}
}
