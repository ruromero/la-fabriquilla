package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ruromero/la-fabriquilla/review"
)

func TestFileStateStoreSaveAndLoad(t *testing.T) {
	store := NewFileStateStore(t.TempDir())
	ctx := context.Background()
	key := StateKey("acme", "widgets", 7)

	original := &State{
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 7,
		Phase:       "plan",
		Iteration:   1,
		IssueTitle:  "Add caching layer",
		IssueBody:   "We need a caching layer",
		PlanOutcome: "plan",
		PlanContent: "Implement LRU cache",
		Review: &ReviewState{
			Findings: []review.ReviewFinding{},
		},
		Files: []FileState{
			{Path: "cache/lru.go", Content: "package cache"},
		},
		StartedAt: time.Date(2026, 5, 30, 10, 0, 0, 0, time.UTC),
	}

	if err := store.Save(ctx, key, original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load(ctx, key)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	t.Run("scalar fields", func(t *testing.T) {
		if loaded.RepoOwner != original.RepoOwner {
			t.Errorf("RepoOwner = %q, want %q", loaded.RepoOwner, original.RepoOwner)
		}
		if loaded.RepoName != original.RepoName {
			t.Errorf("RepoName = %q, want %q", loaded.RepoName, original.RepoName)
		}
		if loaded.IssueNumber != original.IssueNumber {
			t.Errorf("IssueNumber = %d, want %d", loaded.IssueNumber, original.IssueNumber)
		}
		if loaded.Phase != original.Phase {
			t.Errorf("Phase = %q, want %q", loaded.Phase, original.Phase)
		}
		if loaded.PlanOutcome != original.PlanOutcome {
			t.Errorf("PlanOutcome = %q, want %q", loaded.PlanOutcome, original.PlanOutcome)
		}
	})

	t.Run("nested structs", func(t *testing.T) {
		if loaded.Review == nil {
			t.Fatal("Review is nil after round-trip")
		}
		if len(loaded.Review.Findings) != len(original.Review.Findings) {
			t.Errorf("Review.Findings count = %d, want %d", len(loaded.Review.Findings), len(original.Review.Findings))
		}
	})

	t.Run("slices", func(t *testing.T) {
		if len(loaded.Files) != 1 {
			t.Fatalf("Files count = %d, want 1", len(loaded.Files))
		}
		if loaded.Files[0].Path != "cache/lru.go" {
			t.Errorf("Files[0].Path = %q, want %q", loaded.Files[0].Path, "cache/lru.go")
		}
	})

	t.Run("UpdatedAt set", func(t *testing.T) {
		if loaded.UpdatedAt.IsZero() {
			t.Error("UpdatedAt should be set by Save")
		}
	})
}

func TestFileStateStoreLoadNonexistent(t *testing.T) {
	store := NewFileStateStore(t.TempDir())
	ctx := context.Background()

	_, err := store.Load(ctx, "does/not/exist")
	if err == nil {
		t.Fatal("expected error for nonexistent key")
	}
}

func TestFileStateStoreDelete(t *testing.T) {
	store := NewFileStateStore(t.TempDir())
	ctx := context.Background()
	key := StateKey("acme", "widgets", 1)

	state := &State{
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 1,
		Phase:       "init",
		StartedAt:   time.Now(),
	}

	if err := store.Save(ctx, key, state); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Verify file is gone.
	p := filepath.Join(store.BaseDir, key+".json")
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("state file should not exist after Delete, got err=%v", err)
	}

	// Load after delete should fail.
	if _, err := store.Load(ctx, key); err == nil {
		t.Fatal("Load after Delete should return error")
	}
}

func TestStateKey(t *testing.T) {
	t.Run("format", func(t *testing.T) {
		got := StateKey("ruromero", "la-fabriquilla", 42)
		want := "ruromero/la-fabriquilla/42"
		if got != want {
			t.Errorf("StateKey = %q, want %q", got, want)
		}
	})

	t.Run("different values", func(t *testing.T) {
		got := StateKey("acme", "widgets", 100)
		want := "acme/widgets/100"
		if got != want {
			t.Errorf("StateKey = %q, want %q", got, want)
		}
	})
}

func TestFileStateStoreConcurrent(t *testing.T) {
	store := NewFileStateStore(t.TempDir())
	ctx := context.Background()
	key := StateKey("acme", "widgets", 99)

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	// 10 concurrent writers.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(iter int) {
			defer wg.Done()
			s := &State{
				RepoOwner:   "acme",
				RepoName:    "widgets",
				IssueNumber: 99,
				Phase:       "plan",
				Iteration:   iter,
				StartedAt:   time.Now(),
			}
			if err := store.Save(ctx, key, s); err != nil {
				errs <- err
			}
		}(i)
	}

	// 10 concurrent readers (may see partial state or error, that's fine;
	// we just verify no panics or data corruption).
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := store.Load(ctx, key)
			if err != nil {
				// File might not exist yet or be mid-write; acceptable.
				return
			}
			if s.RepoOwner != "acme" {
				errs <- fmt.Errorf("RepoOwner = %q, want acme", s.RepoOwner)
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}
}
