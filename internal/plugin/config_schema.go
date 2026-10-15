package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type ConfigFieldType string

const (
	ConfigString  ConfigFieldType = "string"
	ConfigNumber  ConfigFieldType = "number"
	ConfigBoolean ConfigFieldType = "boolean"
	ConfigEnum    ConfigFieldType = "enum"
	ConfigSecret  ConfigFieldType = "secret"
)

type ConfigVisibility string

const (
	VisibilityPrimary   ConfigVisibility = "PRIMARY"
	VisibilitySecondary ConfigVisibility = "SECONDARY"
	VisibilityExpert    ConfigVisibility = "EXPERT"
)

type PluginConfigSchema struct {
	Version int                 `json:"version"`
	Fields  []PluginConfigField `json:"fields,omitempty"`
}

type PluginConfigField struct {
	Key         string           `json:"key"`
	Label       string           `json:"label"`
	Description string           `json:"description,omitempty"`
	Type        ConfigFieldType  `json:"type"`
	Required    bool             `json:"required,omitempty"`
	Default     json.RawMessage  `json:"default,omitempty"`
	Options     []ConfigOption   `json:"options,omitempty"`
	Visibility  ConfigVisibility `json:"visibility,omitempty"`
}

type ConfigOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

func (s PluginConfigSchema) Validate() error {
	if s.Version != 1 {
		return fmt.Errorf("unsupported schema version %d", s.Version)
	}
	seen := make(map[string]struct{}, len(s.Fields))
	for _, field := range s.Fields {
		if strings.TrimSpace(field.Key) == "" {
			return fmt.Errorf("field key must not be empty")
		}
		if _, exists := seen[field.Key]; exists {
			return fmt.Errorf("duplicate field key %q", field.Key)
		}
		seen[field.Key] = struct{}{}
		if strings.TrimSpace(field.Label) == "" {
			return fmt.Errorf("field %q label must not be empty", field.Key)
		}
		switch field.Type {
		case ConfigString, ConfigNumber, ConfigBoolean, ConfigSecret:
			if len(field.Options) != 0 {
				return fmt.Errorf("field %q options are only valid for enum fields", field.Key)
			}
		case ConfigEnum:
			if len(field.Options) == 0 {
				return fmt.Errorf("field %q enum options must not be empty", field.Key)
			}
			values := make(map[string]struct{}, len(field.Options))
			for _, option := range field.Options {
				if strings.TrimSpace(option.Label) == "" || strings.TrimSpace(option.Value) == "" {
					return fmt.Errorf("field %q enum option label/value must not be empty", field.Key)
				}
				if _, exists := values[option.Value]; exists {
					return fmt.Errorf("field %q has duplicate enum option", field.Key)
				}
				values[option.Value] = struct{}{}
			}
		default:
			return fmt.Errorf("field %q has unsupported type %q", field.Key, field.Type)
		}
		if field.Visibility != "" && field.Visibility != VisibilityPrimary && field.Visibility != VisibilitySecondary && field.Visibility != VisibilityExpert {
			return fmt.Errorf("field %q has unsupported visibility %q", field.Key, field.Visibility)
		}
		if field.Type == ConfigSecret && len(field.Default) > 0 {
			return fmt.Errorf("field %q secret fields cannot define a manifest default", field.Key)
		}
		if len(field.Default) > 0 {
			if err := validateConfigValue(field, field.Default); err != nil {
				return fmt.Errorf("field %q default: %v", field.Key, err)
			}
		}
	}
	return nil
}

func (s PluginConfigSchema) ValidateDocument(document json.RawMessage) error {
	if len(document) == 0 {
		document = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.UseNumber()
	var values map[string]json.RawMessage
	if err := decoder.Decode(&values); err != nil {
		return fmt.Errorf("configuration must be a JSON object")
	}
	if err := ensureDecoderEOF(decoder); err != nil {
		return fmt.Errorf("configuration must contain exactly one JSON object")
	}
	if values == nil {
		return fmt.Errorf("configuration must be a JSON object")
	}
	fields := make(map[string]PluginConfigField, len(s.Fields))
	for _, field := range s.Fields {
		fields[field.Key] = field
		if field.Required {
			if _, exists := values[field.Key]; !exists {
				return fmt.Errorf("required field %q is missing", field.Key)
			}
		}
	}
	for key, value := range values {
		field, exists := fields[key]
		if !exists {
			return fmt.Errorf("unknown field %q", key)
		}
		if err := validateConfigValue(field, value); err != nil {
			return fmt.Errorf("field %q: %v", key, err)
		}
	}
	return nil
}

func validateConfigValue(field PluginConfigField, raw json.RawMessage) error {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("value must not be null")
	}
	switch field.Type {
	case ConfigString, ConfigSecret, ConfigEnum:
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("value must be a string")
		}
		if field.Type == ConfigEnum {
			for _, option := range field.Options {
				if option.Value == value {
					return nil
				}
			}
			return fmt.Errorf("value is not an allowed option")
		}
	case ConfigNumber:
		var value json.Number
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("value must be a number")
		}
		if err := ensureDecoderEOF(decoder); err != nil {
			return fmt.Errorf("value must be a number")
		}
	case ConfigBoolean:
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return fmt.Errorf("value must be a boolean")
		}
	}
	return nil
}
