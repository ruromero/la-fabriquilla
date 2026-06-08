package mcp

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
)

var shellMetachars = []string{";", "&&", "||", "|", "`", "$(", "\n", "\x00"}

// ValidateToolName checks that name is non-empty, present in the allowed
// set, and free of shell metacharacters.
func ValidateToolName(name string, allowed map[string]bool) error {
	if name == "" {
		return fmt.Errorf("empty tool name")
	}
	for _, mc := range shellMetachars {
		if strings.Contains(name, mc) {
			return fmt.Errorf("tool name contains shell metacharacter: %q", mc)
		}
	}
	if !allowed[name] {
		return fmt.Errorf("unknown tool: %s", name)
	}
	return nil
}

// ValidatePathArg checks a path string for shell injection characters,
// path traversal, and absolute paths.
func ValidatePathArg(path string) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	for _, mc := range shellMetachars {
		if strings.Contains(path, mc) {
			return fmt.Errorf("shell metacharacter in path: %q", mc)
		}
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("absolute path not allowed: %s", path)
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed: %s", path)
	}
	lower := strings.ToLower(path)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%2e") {
		return fmt.Errorf("URL-encoded path component not allowed: %s", path)
	}
	if strings.Contains(path, `\`) {
		return fmt.Errorf("backslash in path not allowed: %s", path)
	}
	return nil
}

// ValidateToolArgs checks args against a JSON schema subset: required
// fields and property type checking. The schema is expected to be an
// unmarshaled JSON object with optional "required" and "properties" keys.
func ValidateToolArgs(args map[string]any, schema map[string]any) error {
	if schema == nil {
		return nil
	}

	required, _ := toStringSlice(schema["required"])
	for _, field := range required {
		if args == nil {
			return fmt.Errorf("required field missing: %s", field)
		}
		if _, ok := args[field]; !ok {
			return fmt.Errorf("required field missing: %s", field)
		}
	}

	props, _ := schema["properties"].(map[string]any)
	if props == nil || args == nil {
		return nil
	}
	for key, val := range args {
		propDef, ok := props[key]
		if !ok {
			continue
		}
		propMap, ok := propDef.(map[string]any)
		if !ok {
			continue
		}
		typeName, ok := propMap["type"].(string)
		if !ok {
			continue
		}
		if err := checkType(key, val, typeName); err != nil {
			return err
		}
	}
	return nil
}

func checkType(field string, val any, expected string) error {
	switch expected {
	case "string":
		if _, ok := val.(string); !ok {
			return fmt.Errorf("field %q: expected type string, got %T", field, val)
		}
	case "number":
		if _, ok := val.(float64); !ok {
			return fmt.Errorf("field %q: expected type number, got %T", field, val)
		}
	case "integer":
		f, ok := val.(float64)
		if !ok {
			return fmt.Errorf("field %q: expected type integer, got %T", field, val)
		}
		if f != math.Trunc(f) {
			return fmt.Errorf("field %q: expected integer, got float %v", field, f)
		}
	case "boolean":
		if _, ok := val.(bool); !ok {
			return fmt.Errorf("field %q: expected type boolean, got %T", field, val)
		}
	case "object":
		if _, ok := val.(map[string]any); !ok {
			return fmt.Errorf("field %q: expected type object, got %T", field, val)
		}
	case "array":
		if _, ok := val.([]any); !ok {
			return fmt.Errorf("field %q: expected type array, got %T", field, val)
		}
	}
	return nil
}

func toStringSlice(v any) ([]string, bool) {
	switch arr := v.(type) {
	case []string:
		return arr, true
	case []any:
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			result = append(result, s)
		}
		return result, true
	default:
		return nil, false
	}
}
