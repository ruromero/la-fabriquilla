package eval

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestGoldenCasesValid(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("unable to determine test file path")
	}
	casesDir := filepath.Join(filepath.Dir(thisFile), "cases")

	cases, err := LoadTestCases(casesDir)
	if err != nil {
		t.Fatalf("LoadTestCases: %v", err)
	}

	if len(cases) < 18 {
		t.Fatalf("expected at least 18 cases, got %d", len(cases))
	}

	validPhases := map[string]bool{
		"planner":  true,
		"designer": true,
		"coder":    true,
		"reviewer": true,
		"arbiter":  true,
	}

	validAssertionTypes := map[string]bool{
		"outcome_equals":      true,
		"output_contains":     true,
		"output_not_contains": true,
		"file_count_gte":      true,
		"file_paths_include":  true,
		"severity_present":    true,
		"compiles":            true,
		"tests_pass":          true,
		"llm_judge":           true,
	}

	// Track names per phase for duplicate detection.
	namesByPhase := make(map[string]map[string]bool)

	for _, tc := range cases {
		t.Run(tc.Phase+"/"+tc.Name, func(t *testing.T) {
			if tc.Name == "" {
				t.Error("Name must not be empty")
			}
			if !validPhases[tc.Phase] {
				t.Errorf("invalid Phase %q", tc.Phase)
			}
			if len(tc.Assertions) == 0 {
				t.Error("must have at least one Assertion")
			}
			for _, a := range tc.Assertions {
				if !validAssertionTypes[a.Type] {
					t.Errorf("invalid assertion type %q", a.Type)
				}
			}
			if tc.PassThreshold == "" {
				t.Error("PassThreshold must not be empty")
			}
			_, _, err := ParseThreshold(tc.PassThreshold)
			if err != nil {
				t.Errorf("invalid PassThreshold %q: %v", tc.PassThreshold, err)
			}

			// Check for duplicate names within the same phase.
			if namesByPhase[tc.Phase] == nil {
				namesByPhase[tc.Phase] = make(map[string]bool)
			}
			if namesByPhase[tc.Phase][tc.Name] {
				t.Errorf("duplicate name %q in phase %q", tc.Name, tc.Phase)
			}
			namesByPhase[tc.Phase][tc.Name] = true
		})
	}
}
