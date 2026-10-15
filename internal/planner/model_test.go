package planner

import "testing"

func TestItemTypeValid(t *testing.T) {
	tests := []struct {
		name  string
		value ItemType
		valid bool
	}{
		{name: "todo", value: ItemTypeTodo, valid: true},
		{name: "checkpoint", value: ItemTypeCheckPoint, valid: true},
		{name: "follow up", value: ItemTypeFollowUp, valid: true},
		{name: "reminder", value: ItemTypeReminder, valid: true},
		{name: "unknown", value: ItemType("MAGIC_TASK"), valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.value.Valid(); got != test.valid {
				t.Fatalf(
					"expected Valid() to return %v for %q, got %v",
					test.valid,
					test.value,
					got,
				)
			}
		})
	}
}

// TestStatusValid verifies the normalized Planner lifecycle vocabulary.
func TestStatusValid(t *testing.T) {
	tests := []struct {
		name  string
		value Status
		valid bool
	}{
		{
			name:  "open",
			value: StatusOpen,
			valid: true,
		},
		{
			name:  "completed",
			value: StatusCompleted,
			valid: true,
		},
		{
			name:  "unsupported status",
			value: Status("IN_PROGRESS"),
			valid: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.value.Valid(); got != test.valid {
				t.Fatalf(
					"expected Valid() to return %v for %q, got %v",
					test.valid,
					test.value,
					got,
				)
			}
		})
	}
}

func TestAuthorityTypeValid(t *testing.T) {
	tests := []struct {
		name  string
		value AuthorityType
		valid bool
	}{
		{
			name:  "local",
			value: AuthorityLocal,
			valid: true,
		},
		{
			name:  "external",
			value: AuthorityExternal,
			valid: true,
		},
		{
			name:  "unknown",
			value: AuthorityType("MAGIC_SOURCE"),
			valid: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.value.Valid(); got != test.valid {
				t.Fatalf(
					"expected Valid() to return %v for %q, got %v",
					test.valid,
					test.value,
					got,
				)
			}
		})
	}
}
