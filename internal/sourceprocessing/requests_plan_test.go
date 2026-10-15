package sourceprocessing

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
)

func TestInterpretSchemaOffersNullablePlanBesideCandidate(t *testing.T) {
	var schema struct {
		Properties struct {
			InterpretationDocument struct {
				Required   []string                   `json:"required"`
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"interpretationDocument"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(InterpretOutputSchemaV1(), &schema); err != nil {
		t.Fatalf("interpret schema is not valid JSON: %v", err)
	}
	doc := schema.Properties.InterpretationDocument
	if len(doc.Required) != 2 || doc.Required[0] != "candidate" || doc.Required[1] != "plan" {
		t.Fatalf("interpretationDocument must require candidate and plan: %v", doc.Required)
	}
	var plan struct {
		AnyOf []struct {
			Type                 string          `json:"type"`
			AdditionalProperties *bool           `json:"additionalProperties"`
			Required             []string        `json:"required"`
			Properties           json.RawMessage `json:"properties"`
		} `json:"anyOf"`
	}
	if err := json.Unmarshal(doc.Properties["plan"], &plan); err != nil || len(plan.AnyOf) != 2 || plan.AnyOf[0].Type != "null" {
		t.Fatalf("plan must be nullable: %s %v", doc.Properties["plan"], err)
	}
	object := plan.AnyOf[1]
	if object.AdditionalProperties == nil || *object.AdditionalProperties || len(object.Required) != 4 {
		t.Fatalf("plan object must be closed with all fields required: %+v", object)
	}
}

func TestInterpretOutputDecodesNoActionCandidateAndPlanDocuments(t *testing.T) {
	for name, output := range map[string]string{
		"no action": `{"version":"source.interpret.v1","summary":"Nothing to do.","interpretationDocument":{"candidate":null,"plan":null}}`,
		"legacy":    `{"version":"source.interpret.v1","summary":"Nothing to do.","interpretationDocument":{"candidate":null}}`,
		"candidate": `{"version":"source.interpret.v1","summary":"One.","interpretationDocument":{"candidate":{"version":1,"capability_id":"planner.item.create","arguments":{"title":"A"},"reason":"r"},"plan":null}}`,
		"plan":      `{"version":"source.interpret.v1","summary":"Two.","interpretationDocument":{"candidate":null,"plan":{"title":"T","summary":"S","reason":null,"operations":[{"sequence_no":0,"capability_id":"planner.item.create","arguments":{"title":"A"},"reason":null},{"sequence_no":1,"capability_id":"planner.item.create","arguments":{"title":"B"},"reason":null}]}}}`,
	} {
		if _, err := decodeInterpretOutput(json.RawMessage(output)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestCleanOutputDecodesStrictJSONTextAndLegacyObject(t *testing.T) {
	for name, output := range map[string]string{
		"strict JSON text": `{"version":"source.clean.v1","normalizedDocument":"{\"title\":\"A\",\"nested\":{\"ok\":true}}"}`,
		"legacy object":    `{"version":"source.clean.v1","normalizedDocument":{"title":"A","nested":{"ok":true}}}`,
	} {
		result, err := decodeCleanOutput(json.RawMessage(output))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if string(result.NormalizedDocument) != `{"nested":{"ok":true},"title":"A"}` {
			t.Fatalf("%s: normalized document = %s", name, result.NormalizedDocument)
		}
	}
}

// sourceStrictSchemaProblems checks the strict Structured Outputs subset used
// by the built-in OpenAI provider without coupling the test to field order or
// a full schema snapshot.
func sourceStrictSchemaProblems(path string, node map[string]any) []string {
	var problems []string
	if alternatives, ok := node["anyOf"].([]any); ok {
		for i, alternative := range alternatives {
			object, ok := alternative.(map[string]any)
			if !ok {
				problems = append(problems, fmt.Sprintf("%s.anyOf[%d]: not a schema object", path, i))
				continue
			}
			problems = append(problems, sourceStrictSchemaProblems(fmt.Sprintf("%s.anyOf[%d]", path, i), object)...)
		}
		return problems
	}
	typ, hasType := node["type"]
	if !hasType {
		return append(problems, path+": missing type")
	}
	isType := func(want string) bool {
		switch value := typ.(type) {
		case string:
			return value == want
		case []any:
			for _, item := range value {
				if item == want {
					return true
				}
			}
		}
		return false
	}
	if isType("object") {
		properties, ok := node["properties"].(map[string]any)
		if !ok {
			return append(problems, path+": open object without properties")
		}
		if node["additionalProperties"] != false {
			problems = append(problems, path+": additionalProperties must be false")
		}
		required := map[string]bool{}
		for _, name := range node["required"].([]any) {
			required[name.(string)] = true
		}
		names := make([]string, 0, len(properties))
		for name := range properties {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if !required[name] {
				problems = append(problems, path+"."+name+": not required")
			}
			problems = append(problems, sourceStrictSchemaProblems(path+"."+name, properties[name].(map[string]any))...)
		}
	}
	if isType("array") {
		items, ok := node["items"].(map[string]any)
		if !ok {
			return append(problems, path+": array without items")
		}
		problems = append(problems, sourceStrictSchemaProblems(path+"[]", items)...)
	}
	return problems
}

func TestSourceOutputSchemasAreStrictStructuredOutputCompatible(t *testing.T) {
	for name, raw := range map[string]json.RawMessage{
		"source.clean":     CleanOutputSchemaV1(),
		"source.interpret": InterpretOutputSchemaV1(),
	} {
		var schema map[string]any
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("%s schema is not valid JSON: %v", name, err)
		}
		if problems := sourceStrictSchemaProblems("$", schema); len(problems) > 0 {
			t.Fatalf("%s schema violates the strict subset:\n%s", name, strings.Join(problems, "\n"))
		}
	}
}
