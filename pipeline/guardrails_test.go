package pipeline

import "testing"

func TestCheckCostBudget(t *testing.T) {
	t.Run("under budget", func(t *testing.T) {
		state := &State{TotalPromptTokens: 500, TotalCompTokens: 300}
		if err := CheckCostBudget(state, 1000); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("over budget", func(t *testing.T) {
		state := &State{TotalPromptTokens: 600, TotalCompTokens: 500}
		err := CheckCostBudget(state, 1000)
		if err == nil {
			t.Fatal("expected error when over budget")
		}
		want := "token budget exceeded: 1100 used, limit 1000"
		if err.Error() != want {
			t.Errorf("error = %q, want %q", err.Error(), want)
		}
	})

	t.Run("exactly at budget", func(t *testing.T) {
		state := &State{TotalPromptTokens: 500, TotalCompTokens: 500}
		if err := CheckCostBudget(state, 1000); err != nil {
			t.Errorf("unexpected error at exact budget: %v", err)
		}
	})

	t.Run("zero budget disables check", func(t *testing.T) {
		state := &State{TotalPromptTokens: 999999, TotalCompTokens: 999999}
		if err := CheckCostBudget(state, 0); err != nil {
			t.Errorf("unexpected error with zero budget: %v", err)
		}
	})

	t.Run("negative budget disables check", func(t *testing.T) {
		state := &State{TotalPromptTokens: 999999, TotalCompTokens: 999999}
		if err := CheckCostBudget(state, -1); err != nil {
			t.Errorf("unexpected error with negative budget: %v", err)
		}
	})
}

func TestCheckPRScope(t *testing.T) {
	t.Run("within limits", func(t *testing.T) {
		files := []FileState{
			{Path: "a.go", Content: "line1\nline2"},
			{Path: "b.go", Content: "line1"},
		}
		if err := CheckPRScope(files, 5, 100); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("too many files", func(t *testing.T) {
		files := []FileState{
			{Path: "a.go", Content: "x"},
			{Path: "b.go", Content: "x"},
			{Path: "c.go", Content: "x"},
		}
		err := CheckPRScope(files, 2, 0)
		if err == nil {
			t.Fatal("expected error for too many files")
		}
		want := "too many files changed: 3, limit 2"
		if err.Error() != want {
			t.Errorf("error = %q, want %q", err.Error(), want)
		}
	})

	t.Run("too many lines", func(t *testing.T) {
		files := []FileState{
			{Path: "a.go", Content: "1\n2\n3\n4\n5"},
			{Path: "b.go", Content: "1\n2\n3\n4\n5\n6"},
		}
		err := CheckPRScope(files, 0, 10)
		if err == nil {
			t.Fatal("expected error for too many lines")
		}
		// a.go = 5 lines, b.go = 6 lines => 11 total
		want := "PR too large: 11 lines, limit 10"
		if err.Error() != want {
			t.Errorf("error = %q, want %q", err.Error(), want)
		}
	})

	t.Run("zero limits disables check", func(t *testing.T) {
		files := []FileState{
			{Path: "a.go", Content: "x"},
			{Path: "b.go", Content: "x"},
		}
		if err := CheckPRScope(files, 0, 0); err != nil {
			t.Errorf("unexpected error with zero limits: %v", err)
		}
	})

	t.Run("empty file list", func(t *testing.T) {
		if err := CheckPRScope(nil, 5, 100); err != nil {
			t.Errorf("unexpected error for empty list: %v", err)
		}
	})
}

func TestCountLines(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		if n := countLines(""); n != 0 {
			t.Errorf("countLines(\"\") = %d, want 0", n)
		}
	})

	t.Run("single line no newline", func(t *testing.T) {
		if n := countLines("hello"); n != 1 {
			t.Errorf("countLines(\"hello\") = %d, want 1", n)
		}
	})

	t.Run("single line with newline", func(t *testing.T) {
		if n := countLines("hello\n"); n != 2 {
			t.Errorf("countLines(\"hello\\n\") = %d, want 2", n)
		}
	})

	t.Run("multiple lines", func(t *testing.T) {
		if n := countLines("a\nb\nc"); n != 3 {
			t.Errorf("countLines(\"a\\nb\\nc\") = %d, want 3", n)
		}
	})

	t.Run("multiple lines with trailing newline", func(t *testing.T) {
		if n := countLines("a\nb\nc\n"); n != 4 {
			t.Errorf("countLines(\"a\\nb\\nc\\n\") = %d, want 4", n)
		}
	})
}
