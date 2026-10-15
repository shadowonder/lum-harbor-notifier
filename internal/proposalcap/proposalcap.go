// Package proposalcap defines the minimal, dependency-free data shapes shared
// between internal/assistant and the application-owned Proposal capability
// registry (app). It carries only generic metadata and a generic argument
// validator - never domain truth, transactions, or a Proposal/DB import.
//
// This is the mutation-side counterpart of internal/readcap.Descriptor: a
// shared shape, not a shared runtime. Execution/binding (lookup, invoke,
// create a durable Proposal) stays entirely in the app package and
// internal/proposal, exactly where domain mutation already happens.
package proposalcap

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ArgType is the closed set of primitive argument types a Proposal
// capability argument may declare. Only what current capabilities actually
// need is supported: no arrays of anything but strings, no nested objects,
// no union types.
type ArgType string

const (
	ArgString     ArgType = "string"
	ArgInteger    ArgType = "integer"
	ArgBoolean    ArgType = "boolean"
	ArgDate       ArgType = "date"     // YYYY-MM-DD
	ArgDatetime   ArgType = "datetime" // RFC3339, exact time + offset required
	ArgEnum       ArgType = "enum"
	ArgStringList ArgType = "string_list" // ordered []string - the one collection type the real Workflow contract needs
)

func (t ArgType) valid() bool {
	switch t {
	case ArgString, ArgInteger, ArgBoolean, ArgDate, ArgDatetime, ArgEnum, ArgStringList:
		return true
	default:
		return false
	}
}

// ArgumentSpec describes one argument of one Proposal capability.
type ArgumentSpec struct {
	Type        ArgType
	Required    bool
	Description string
	EnumValues  []string // required and only meaningful for ArgEnum
	Minimum     *int     // only meaningful for ArgInteger
	Maximum     *int     // only meaningful for ArgInteger
}

// Descriptor is the backend-owned, AI-facing metadata for one Proposal
// capability. It exposes only what the model needs to decide whether and how
// to call the capability - never domain internals, SQL, or repository
// concepts.
type Descriptor struct {
	ID      string
	Summary string
	UseWhen string
	// RequiresWorkItemContext is backend-owned metadata, not a permission
	// gate: it tells Prompt 2 whether this specific capability's arguments
	// depend on an authoritative base WorkItem being already loaded (for
	// example legacy Tracking operations that resolve titles against the
	// loaded Workflow snapshot). A capability with a stable target-ID
	// argument (work_item_id, tracked_item_id, ...) does NOT need this set,
	// because its handler authoritative-loads/revalidates that target itself
	// regardless of what page or selection the user currently has open.
	RequiresWorkItemContext bool
	// SourceInterpretEligible indicates whether this capability is eligible
	// to be offered to generic background source interpretation (source.interpret).
	// When false, the capability is available to the interactive Assistant but
	// strictly excluded from the source interpretation candidate contract.
	SourceInterpretEligible bool
	Arguments               map[string]ArgumentSpec
}

// Source is the narrow contract internal/assistant needs to render available
// Proposal capabilities and validate a model's selection - implemented by the
// application-owned capability registry.
type Source interface {
	ListDescriptors() []Descriptor
}

// Call is the generic model selection: a capability ID plus its raw
// arguments object. internal/assistant decodes exactly this shape and knows
// nothing about what any capability ID means.
type Call struct {
	CapabilityID string
	Arguments    json.RawMessage
}

// ValidateArguments checks raw JSON arguments against a capability's
// ArgumentSpec map: unknown keys, missing required keys, and per-type/range
// checks. It validates only shape and primitive bounds - never domain truth
// (entity existence, business invariants), which remains the handler's job.
func ValidateArguments(spec map[string]ArgumentSpec, raw json.RawMessage) (map[string]any, error) {
	values := map[string]any{}
	s := strings.TrimSpace(string(raw))
	if len(s) > 0 && s != "null" {
		dec := json.NewDecoder(strings.NewReader(s))
		if err := dec.Decode(&values); err != nil {
			return nil, fmt.Errorf("arguments must be a JSON object: %w", err)
		}
	}
	for key := range values {
		if _, ok := spec[key]; !ok {
			if key == "details" {
				if _, hasNotes := spec["notes"]; hasNotes {
					values["notes"] = values[key]
					delete(values, key)
					continue
				}
			} else if key == "notes" {
				if _, hasDetails := spec["details"]; hasDetails {
					values["details"] = values[key]
					delete(values, key)
					continue
				}
			} else if key == "dueDate" {
				if _, hasDueDate := spec["due_date"]; hasDueDate {
					values["due_date"] = values[key]
					delete(values, key)
					continue
				}
			} else if key == "dueTime" {
				if _, hasDueTime := spec["due_time"]; hasDueTime {
					values["due_time"] = values[key]
					delete(values, key)
					continue
				}
			} else if key == "due_at" {
				if _, hasDueTime := spec["due_time"]; hasDueTime {
					continue
				}
			} else if key == "work_item_id" {
				continue
			}
			return nil, fmt.Errorf("unknown argument %q", key)
		}
	}
	for name, arg := range spec {
		value, present := values[name]
		if !present || value == nil {
			if arg.Required {
				return nil, fmt.Errorf("missing required argument %q", name)
			}
			continue
		}
		if err := validateArgValue(values, name, arg, value); err != nil {
			return nil, err
		}
	}
	return values, nil
}

func validateArgValue(values map[string]any, name string, arg ArgumentSpec, value any) error {
	switch arg.Type {
	case ArgString, ArgEnum:
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("argument %q must be a string", name)
		}
		if arg.Type == ArgEnum {
			for _, allowed := range arg.EnumValues {
				if s == allowed {
					return nil
				}
			}
			return fmt.Errorf("argument %q must be one of %v", name, arg.EnumValues)
		}
		return nil
	case ArgBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("argument %q must be a boolean", name)
		}
		return nil
	case ArgInteger:
		f, ok := value.(float64)
		if !ok || f != float64(int(f)) {
			return fmt.Errorf("argument %q must be an integer", name)
		}
		n := int(f)
		if arg.Minimum != nil && n < *arg.Minimum {
			return fmt.Errorf("argument %q must be >= %d", name, *arg.Minimum)
		}
		if arg.Maximum != nil && n > *arg.Maximum {
			return fmt.Errorf("argument %q must be <= %d", name, *arg.Maximum)
		}
		return nil
	case ArgDate:
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("argument %q must be a date string", name)
		}
		if len(s) > 10 {
			if idx := strings.Index(s, "T"); idx > 0 {
				s = s[:idx]
				values[name] = s
			} else if idx := strings.Index(s, " "); idx > 0 {
				s = s[:idx]
				values[name] = s
			} else if t, err := time.Parse(time.RFC3339, s); err == nil {
				s = t.Format("2006-01-02")
				values[name] = s
			}
		}
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return fmt.Errorf("argument %q must be YYYY-MM-DD: %w", name, err)
		}
		return nil
	case ArgDatetime:
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("argument %q must be a datetime string", name)
		}
		if _, err := time.Parse(time.RFC3339, s); err != nil {
			// Resilient fallback: parse ISO-8601 without offset in system local time and format as RFC3339 with local offset
			if t, errLocal := time.ParseInLocation("2006-01-02T15:04:05", s, time.Local); errLocal == nil {
				values[name] = t.Format(time.RFC3339)
				return nil
			}
			if t, errLocal := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); errLocal == nil {
				values[name] = t.Format(time.RFC3339)
				return nil
			}
			return fmt.Errorf("argument %q must be RFC3339 with an exact offset: %w", name, err)
		}
		return nil
	case ArgStringList:
		list, ok := value.([]any)
		if !ok {
			return fmt.Errorf("argument %q must be a list of strings", name)
		}
		if len(list) == 0 {
			return fmt.Errorf("argument %q must not be empty", name)
		}
		for i, item := range list {
			switch v := item.(type) {
			case string:
			case float64:
				list[i] = fmt.Sprintf("%d", int(v))
			case int:
				list[i] = fmt.Sprintf("%d", v)
			default:
				return fmt.Errorf("argument %q must contain strings or integers", name)
			}
		}
		return nil
	default:
		return fmt.Errorf("argument %q has unsupported type %q", name, arg.Type)
	}
}

// AsString reads a validated, present string-typed argument.
func AsString(values map[string]any, name string) (string, bool) {
	v, ok := values[name]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// AsStringList reads a validated, present string_list-typed argument.
func AsStringList(values map[string]any, name string) ([]string, bool) {
	v, ok := values[name]
	if !ok {
		return nil, false
	}
	raw, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, s)
	}
	return out, true
}
