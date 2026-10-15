package plugin_test

import (
	"encoding/json"
	"testing"
	"time"

	"lum-harbor-notifier/internal/plugin"
)

func TestSourceFetchResultValidation(t *testing.T) {
	now := time.Now().UTC()
	valid := plugin.SourceRecord{
		ExternalID: "item-1", SourceKind: "dummy.todo", ObservedAt: now,
		SourceUpdatedAt: &now, Payload: json.RawMessage(`{"title":"one"}`),
	}
	if err := (plugin.SourceFetchResult{Records: []plugin.SourceRecord{valid}}).Validate(); err != nil {
		t.Fatalf("valid result: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*plugin.SourceRecord)
	}{
		{name: "external id", mutate: func(record *plugin.SourceRecord) { record.ExternalID = "" }},
		{name: "source kind", mutate: func(record *plugin.SourceRecord) { record.SourceKind = "" }},
		{name: "observed at", mutate: func(record *plugin.SourceRecord) { record.ObservedAt = time.Time{} }},
		{name: "source updated at", mutate: func(record *plugin.SourceRecord) { zero := time.Time{}; record.SourceUpdatedAt = &zero }},
		{name: "payload", mutate: func(record *plugin.SourceRecord) { record.Payload = json.RawMessage(`{`) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := valid
			test.mutate(&record)
			if err := (plugin.SourceFetchResult{Records: []plugin.SourceRecord{record}}).Validate(); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}
