package eval

import (
	"strings"
	"testing"
	"time"
)

func mkRecord(caseName, model string, ts time.Time, passes int) Record {
	return Record{
		Timestamp: ts, GitSHA: "abc1234", Case: caseName, Phase: "planner",
		Model: model, Runs: 10, Passes: passes, Threshold: 8,
		Pass: passes >= 8, WallTimeSecs: 12.5,
	}
}

func TestAppendAndLoadRecords(t *testing.T) {
	dir := t.TempDir()
	r1 := mkRecord("planner/a", "m1", time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC), 9)
	r2 := mkRecord("planner/a", "m2", time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC), 5)
	if err := AppendRecord(dir, r1); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := AppendRecord(dir, r2); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, err := LoadRecords(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (monthly files: 2026-06 and 2026-07)", len(got))
	}
}

func TestFormatComparisonUsesLatestPerCaseModel(t *testing.T) {
	recs := []Record{
		mkRecord("planner/a", "m1", time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), 2),
		mkRecord("planner/a", "m1", time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), 9), // latest m1
		mkRecord("planner/a", "m2", time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), 5),
	}
	out := FormatComparison(recs)
	if !strings.Contains(out, "9/10") {
		t.Errorf("latest m1 record (9/10) missing:\n%s", out)
	}
	if strings.Contains(out, "2/10") {
		t.Errorf("stale m1 record (2/10) should not appear:\n%s", out)
	}
	if !strings.Contains(out, "m2") || !strings.Contains(out, "5/10") {
		t.Errorf("m2 5/10 missing:\n%s", out)
	}
}
