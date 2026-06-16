package eval

import (
	"testing"
)

func TestParseJudgeResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantOK  bool
		wantErr bool
	}{
		{
			name:   "plain json",
			input:  `{"pass": true, "explanation": "all good"}`,
			wantOK: true,
		},
		{
			name:   "json in markdown fence",
			input:  "```json\n{\"pass\": false, \"explanation\": \"missing function\"}\n```",
			wantOK: false,
		},
		{
			name:   "json with surrounding text",
			input:  "Here is my evaluation:\n{\"pass\": true, \"explanation\": \"criteria met\"}\nDone.",
			wantOK: true,
		},
		{
			name:   "plain fence no lang",
			input:  "```\n{\"pass\": true, \"explanation\": \"ok\"}\n```",
			wantOK: true,
		},
		{
			name:    "no json",
			input:   "This output looks fine to me.",
			wantErr: true,
		},
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid json",
			input:   `{"pass": maybe}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := ParseJudgeResponse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Pass != tt.wantOK {
				t.Errorf("pass = %v, want %v", resp.Pass, tt.wantOK)
			}
			if resp.Explanation == "" {
				t.Error("explanation should not be empty")
			}
		})
	}
}

func TestBuildJudgePrompt(t *testing.T) {
	tc := TestCase{
		Name:  "test-case",
		Phase: "coder",
		Inputs: map[string]string{
			"plan":        "Add a Foo function",
			"conventions": "Use stdlib only",
		},
	}
	output := "func Foo() { return 42 }"
	directive := "1. Defines function Foo\n2. Uses only stdlib"

	sys, usr := BuildJudgePrompt(tc, output, directive)

	if sys == "" {
		t.Error("system prompt should not be empty")
	}
	for _, want := range []string{"Foo function", "func Foo()", "Defines function Foo", "Uses only stdlib"} {
		if !contains(usr, want) {
			t.Errorf("user prompt missing %q", want)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
