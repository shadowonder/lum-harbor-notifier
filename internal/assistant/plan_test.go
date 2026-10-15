package assistant

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
)

func planEnvelope(operations string) string {
	return `{"version":1,"kind":"MUTATION_CANDIDATE","text":"One plan.","candidate":null,"plan":{"title":"Plan","summary":"Does two things.","reason":"Requested together.","operations":[` + operations + `]}}`
}

func planOp(seq int, capability, args string) string {
	quoted, _ := json.Marshal(args)
	return fmt.Sprintf(`{"sequence_no":%d,"capability_id":%q,"arguments":%s,"reason":null}`, seq, capability, quoted)
}

func TestDecodePrompt2ResultPlanAndCandidate(t *testing.T) {
	singular, err := DecodePrompt2Result(json.RawMessage(`{"version":1,"kind":"MUTATION_CANDIDATE","text":"One.","candidate":{"version":1,"capability_id":"cap.one","arguments":"{\"title\":\"A\"}","reason":null},"plan":null}`))
	if err != nil || singular.Candidate == nil || singular.Plan != nil || string(singular.Candidate.Arguments) != `{"title":"A"}` {
		t.Fatalf("singular candidate: %+v %v", singular, err)
	}
	legacy, err := DecodePrompt2Result(json.RawMessage(`{"version":1,"kind":"MUTATION_CANDIDATE","text":"One.","candidate":{"version":1,"capability_id":"cap.one","arguments":"{}","reason":null}}`))
	if err != nil || legacy.Candidate == nil {
		t.Fatalf("an output without a plan field keeps the singular contract: %+v %v", legacy, err)
	}

	plan, err := DecodePrompt2Result(json.RawMessage(planEnvelope(planOp(0, "cap.one", `{"title":"A"}`) + "," + planOp(1, "cap.two", `{"ref":"$op[0].work_item_id"}`))))
	if err != nil || plan.Plan == nil || plan.Candidate != nil {
		t.Fatalf("plan: %+v %v", plan, err)
	}
	if plan.Plan.Title != "Plan" || *plan.Plan.Summary != "Does two things." || *plan.Plan.Reason != "Requested together." || len(plan.Plan.Operations) != 2 {
		t.Fatalf("plan metadata: %+v", plan.Plan)
	}
	if plan.Plan.Operations[1].CapabilityID != "cap.two" || string(plan.Plan.Operations[1].Arguments) != `{"ref":"$op[0].work_item_id"}` {
		t.Fatalf("operation arguments must be decoded verbatim from the JSON string: %+v", plan.Plan.Operations[1])
	}

	ops := make([]string, 25)
	for i := range ops {
		ops[i] = planOp(i, "cap.one", `{}`)
	}
	near, err := DecodePrompt2Result(json.RawMessage(planEnvelope(strings.Join(ops[:20], ","))))
	if err != nil || len(near.Plan.Operations) != 20 {
		t.Fatalf("20 operations decode: %v", err)
	}
	// The decoder validates shape only; the 20-operation limit is enforced by
	// the application's preflight so the user gets a precise refusal.
	over, err := DecodePrompt2Result(json.RawMessage(planEnvelope(strings.Join(ops, ","))))
	if err != nil || len(over.Plan.Operations) != 25 {
		t.Fatalf("25 operations reach preflight untruncated: %v", err)
	}

	answer, err := DecodePrompt2Result(json.RawMessage(`{"version":1,"kind":"ANSWER","text":"Hi.","candidate":null,"plan":null}`))
	if err != nil || answer.Kind != KindAnswer || answer.Plan != nil || answer.Candidate != nil {
		t.Fatalf("answer: %+v %v", answer, err)
	}
}

func TestDecodePrompt2ResultRejectsMalformedPlans(t *testing.T) {
	both := `{"version":1,"kind":"MUTATION_CANDIDATE","text":"x","candidate":{"version":1,"capability_id":"cap.one","arguments":"{}","reason":null},"plan":{"title":"P","summary":"S","reason":null,"operations":[` + planOp(0, "cap.one", `{}`) + `]}}`
	for name, output := range map[string]string{
		"candidate and plan together":  both,
		"neither candidate nor plan":   `{"version":1,"kind":"MUTATION_CANDIDATE","text":"x","candidate":null,"plan":null}`,
		"plan on an answer":            `{"version":1,"kind":"ANSWER","text":"x","candidate":null,"plan":{"title":"P","summary":"S","reason":null,"operations":[]}}`,
		"plan on unsupported":          `{"version":1,"kind":"UNSUPPORTED","text":"x","candidate":null,"plan":{"title":"P","summary":"S","reason":null,"operations":[]}}`,
		"empty operations":             planEnvelope(""),
		"missing title":                `{"version":1,"kind":"MUTATION_CANDIDATE","text":"x","candidate":null,"plan":{"title":" ","summary":"S","reason":null,"operations":[` + planOp(0, "cap.one", `{}`) + `]}}`,
		"sequence gap":                 planEnvelope(planOp(0, "cap.one", `{}`) + "," + planOp(2, "cap.two", `{}`)),
		"sequence out of order":        planEnvelope(planOp(1, "cap.one", `{}`) + "," + planOp(0, "cap.two", `{}`)),
		"blank capability":             planEnvelope(planOp(0, " ", `{}`)),
		"arguments not an object":      planEnvelope(planOp(0, "cap.one", `["a"]`)),
		"arguments not a JSON string":  planEnvelope(`{"sequence_no":0,"capability_id":"cap.one","arguments":"\"unterminated","reason":null}`),
		"unknown operation property":   planEnvelope(`{"sequence_no":0,"capability_id":"cap.one","arguments":"{}","reason":null,"execute":true}`),
		"unknown plan property":        `{"version":1,"kind":"MUTATION_CANDIDATE","text":"x","candidate":null,"plan":{"title":"P","summary":"S","reason":null,"operations":[` + planOp(0, "cap.one", `{}`) + `],"autoApply":true}}`,
		"unknown top-level property":   `{"version":1,"kind":"ANSWER","text":"x","candidate":null,"plan":null,"plans":[]}`,
		"operation reason is too long": planEnvelope(`{"sequence_no":0,"capability_id":"cap.one","arguments":"{}","reason":"` + strings.Repeat("r", 501) + `"}`),
	} {
		if _, err := DecodePrompt2Result(json.RawMessage(output)); !errors.Is(err, ErrMalformedResult) {
			t.Errorf("%s: want ErrMalformedResult, got %v", name, err)
		}
	}
}

// strictSchemaProblems checks the OpenAI strict Structured Outputs subset the
// project relies on: every object declares additionalProperties:false and
// requires every declared property; every property has an explicit type (or
// is an anyOf of typed alternatives); arrays declare items; no unconstrained
// open object appears.
func strictSchemaProblems(path string, node map[string]any) []string {
	var problems []string
	if alternatives, ok := node["anyOf"].([]any); ok {
		for i, alternative := range alternatives {
			problems = append(problems, strictSchemaProblems(fmt.Sprintf("%s.anyOf[%d]", path, i), alternative.(map[string]any))...)
		}
		return problems
	}
	typ, hasType := node["type"]
	if !hasType {
		return append(problems, path+": missing type")
	}
	isType := func(want string) bool {
		switch v := typ.(type) {
		case string:
			return v == want
		case []any:
			for _, item := range v {
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
			problems = append(problems, strictSchemaProblems(path+"."+name, properties[name].(map[string]any))...)
		}
	}
	if isType("array") {
		items, ok := node["items"].(map[string]any)
		if !ok {
			return append(problems, path+": array without items")
		}
		problems = append(problems, strictSchemaProblems(path+"[]", items)...)
	}
	return problems
}

func TestPrompt2SchemaIsStrictStructuredOutputCompatible(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(prompt2Schema, &schema); err != nil {
		t.Fatalf("Prompt 2 schema is not valid JSON: %v", err)
	}
	if problems := strictSchemaProblems("$", schema); len(problems) > 0 {
		t.Fatalf("Prompt 2 schema violates the strict subset:\n%s", strings.Join(problems, "\n"))
	}
	plan := schema["properties"].(map[string]any)["plan"].(map[string]any)["anyOf"].([]any)
	if plan[0].(map[string]any)["type"] != "null" {
		t.Fatal("plan must be explicitly nullable")
	}
	operation := plan[1].(map[string]any)["properties"].(map[string]any)["operations"].(map[string]any)["items"].(map[string]any)
	if operation["properties"].(map[string]any)["arguments"].(map[string]any)["type"] != "string" {
		t.Fatal("plan operation arguments must stay a JSON string, like candidate arguments")
	}
	// The checker itself must catch the violations it guards against.
	for name, bad := range map[string]string{
		"open object":     `{"type":"object","properties":{"a":{"type":"object"}},"required":["a"],"additionalProperties":false}`,
		"optional":        `{"type":"object","properties":{"a":{"type":"string"}},"required":[],"additionalProperties":false}`,
		"extra allowed":   `{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}`,
		"untyped":         `{"type":"object","properties":{"a":{"const":1}},"required":["a"],"additionalProperties":false}`,
		"untyped array":   `{"type":"object","properties":{"a":{"type":"array"}},"required":["a"],"additionalProperties":false}`,
		"open array item": `{"type":"object","properties":{"a":{"type":"array","items":{"type":"object"}}},"required":["a"],"additionalProperties":false}`,
	} {
		var node map[string]any
		_ = json.Unmarshal([]byte(bad), &node)
		if len(strictSchemaProblems("$", node)) == 0 {
			t.Errorf("checker missed %s", name)
		}
	}
}

func TestPrompt2RequestCarriesPlanContract(t *testing.T) {
	request, err := BuildPrompt2Request("req", "Create two streams", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(request.OutputSchema) != string(prompt2Schema) {
		t.Fatal("request must carry the plan-capable schema")
	}
	system := request.Messages[0].Content
	for _, marker := range []string{
		"propose them together as ONE plan",
		"at most 20 operations",
		`"$op[N].<output>"`,
		"EARLIER operation",
		"never put a reference inside a title",
		"each Workflow created in one plan must have its own distinct name",
		"never set both \"candidate\" and \"plan\"",
	} {
		if !strings.Contains(system, marker) {
			t.Errorf("Prompt 2 system prompt is missing %q", marker)
		}
	}
}
