package main

import (
	"strings"
	"testing"
	"time"
)

func TestRateTracker_PruneAndCheck(t *testing.T) {
	t.Run("under hourly limit", func(t *testing.T) {
		r := &rateTracker{}
		r.hourly = []time.Time{time.Now(), time.Now()}
		r.daily = []time.Time{time.Now(), time.Now()}
		if err := r.pruneAndCheck(5, 20); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("at hourly limit", func(t *testing.T) {
		r := &rateTracker{}
		now := time.Now()
		r.hourly = []time.Time{now, now, now, now, now}
		r.daily = []time.Time{now, now, now, now, now}
		err := r.pruneAndCheck(5, 20)
		if err == nil {
			t.Fatal("expected error at hourly limit")
		}
		if !strings.Contains(err.Error(), "hourly rate limit") {
			t.Errorf("error = %q, want hourly rate limit message", err.Error())
		}
	})

	t.Run("over hourly limit", func(t *testing.T) {
		r := &rateTracker{}
		now := time.Now()
		r.hourly = []time.Time{now, now, now, now, now, now}
		r.daily = []time.Time{now, now, now, now, now, now}
		err := r.pruneAndCheck(5, 20)
		if err == nil {
			t.Fatal("expected error over hourly limit")
		}
		if !strings.Contains(err.Error(), "hourly rate limit") {
			t.Errorf("error = %q, want hourly rate limit message", err.Error())
		}
	})

	t.Run("at daily limit", func(t *testing.T) {
		r := &rateTracker{}
		now := time.Now()
		for i := 0; i < 20; i++ {
			r.hourly = append(r.hourly, now.Add(-2*time.Hour)) // outside hourly window
			r.daily = append(r.daily, now.Add(-time.Duration(i)*time.Minute))
		}
		err := r.pruneAndCheck(5, 20)
		if err == nil {
			t.Fatal("expected error at daily limit")
		}
		if !strings.Contains(err.Error(), "daily rate limit") {
			t.Errorf("error = %q, want daily rate limit message", err.Error())
		}
	})

	t.Run("pruning removes old entries", func(t *testing.T) {
		r := &rateTracker{}
		now := time.Now()
		// Add entries from 2 hours ago (should be pruned from hourly)
		r.hourly = []time.Time{
			now.Add(-2 * time.Hour),
			now.Add(-2 * time.Hour),
			now.Add(-2 * time.Hour),
			now.Add(-2 * time.Hour),
			now.Add(-2 * time.Hour),
			now,
		}
		r.daily = []time.Time{now}
		if err := r.pruneAndCheck(5, 20); err != nil {
			t.Errorf("unexpected error after pruning: %v", err)
		}
		// After pruning, hourly should only have the recent entry
		if len(r.hourly) != 1 {
			t.Errorf("hourly entries = %d, want 1 after pruning", len(r.hourly))
		}
	})

	t.Run("pruning removes old daily entries", func(t *testing.T) {
		r := &rateTracker{}
		now := time.Now()
		// Add entries from 25 hours ago (should be pruned from daily)
		r.hourly = []time.Time{now}
		r.daily = []time.Time{
			now.Add(-25 * time.Hour),
			now.Add(-25 * time.Hour),
			now,
		}
		if err := r.pruneAndCheck(5, 20); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(r.daily) != 1 {
			t.Errorf("daily entries = %d, want 1 after pruning", len(r.daily))
		}
	})

	t.Run("zero limits disables checks", func(t *testing.T) {
		r := &rateTracker{}
		now := time.Now()
		for i := 0; i < 100; i++ {
			r.hourly = append(r.hourly, now)
			r.daily = append(r.daily, now)
		}
		if err := r.pruneAndCheck(0, 0); err != nil {
			t.Errorf("unexpected error with zero limits: %v", err)
		}
	})
}

func TestRateTracker_Record(t *testing.T) {
	r := &rateTracker{}
	r.record()
	if len(r.hourly) != 1 {
		t.Errorf("hourly = %d, want 1", len(r.hourly))
	}
	if len(r.daily) != 1 {
		t.Errorf("daily = %d, want 1", len(r.daily))
	}
	r.record()
	if len(r.hourly) != 2 {
		t.Errorf("hourly = %d, want 2", len(r.hourly))
	}
	if len(r.daily) != 2 {
		t.Errorf("daily = %d, want 2", len(r.daily))
	}
}

func TestPruneOlderThan(t *testing.T) {
	now := time.Now()
	times := []time.Time{
		now.Add(-3 * time.Hour),
		now.Add(-2 * time.Hour),
		now.Add(-30 * time.Minute),
		now,
	}
	cutoff := now.Add(-1 * time.Hour)
	result := pruneOlderThan(times, cutoff)
	if len(result) != 2 {
		t.Errorf("pruneOlderThan returned %d entries, want 2", len(result))
	}
}
