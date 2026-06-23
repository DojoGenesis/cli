package activity

// Tests for internal/activity/log.go.
//
// All tests are hermetic: t.Setenv("HOME", t.TempDir()) redirects
// config.DojoDir() — which calls os.UserHomeDir() — away from the real ~/.dojo.
// No network, no external processes, no mutations to the developer's home dir.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// isoHome sets HOME to a fresh temp directory and returns a cleanup function.
// Every test that exercises Append/Recent/Clear calls this first.
func isoHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

// ---------------------------------------------------------------------------
// Recent on a non-existent log — must return nil, nil (NOT an error).
// This test deliberately runs BEFORE any Append so the file never exists.
// ---------------------------------------------------------------------------

func TestRecentNonExistentFileReturnsNil(t *testing.T) {
	isoHome(t)

	entries, err := Recent(0)
	if err != nil {
		t.Fatalf("Recent on missing file returned unexpected error: %v", err)
	}
	if entries != nil {
		t.Errorf("Recent on missing file = %v; want nil", entries)
	}
}

// ---------------------------------------------------------------------------
// Round-trip: Append several entries, Recent(0) returns them ALL newest-first.
// ---------------------------------------------------------------------------

func TestRecentRoundTripNewestFirst(t *testing.T) {
	isoHome(t)

	seeds := []Entry{
		{Type: CommandRun, Summary: "first"},
		{Type: SkillInvoked, Summary: "second"},
		{Type: ArtifactSaved, Summary: "third"},
	}
	for _, e := range seeds {
		if err := Append(e); err != nil {
			t.Fatalf("Append(%q): %v", e.Summary, err)
		}
	}

	got, err := Recent(0)
	if err != nil {
		t.Fatalf("Recent(0): %v", err)
	}
	if len(got) != len(seeds) {
		t.Fatalf("Recent(0) returned %d entries; want %d", len(got), len(seeds))
	}

	// Expect newest-first: third, second, first.
	want := []string{"third", "second", "first"}
	for i, e := range got {
		if e.Summary != want[i] {
			t.Errorf("entry[%d].Summary = %q; want %q", i, e.Summary, want[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Recent(n) with n > 0 returns only the n newest entries.
// ---------------------------------------------------------------------------

func TestRecentTruncatesToN(t *testing.T) {
	isoHome(t)

	summaries := []string{"a", "b", "c", "d", "e"}
	for _, s := range summaries {
		if err := Append(Entry{Type: CommandRun, Summary: s}); err != nil {
			t.Fatalf("Append(%q): %v", s, err)
		}
	}

	cases := []struct {
		n        int
		wantLen  int
		wantFirst string // newest (last written)
	}{
		{n: 1, wantLen: 1, wantFirst: "e"},
		{n: 3, wantLen: 3, wantFirst: "e"},
		{n: 5, wantLen: 5, wantFirst: "e"},
		{n: 10, wantLen: 5, wantFirst: "e"}, // n > total → returns all
	}

	for _, tc := range cases {
		got, err := Recent(tc.n)
		if err != nil {
			t.Errorf("Recent(%d): %v", tc.n, err)
			continue
		}
		if len(got) != tc.wantLen {
			t.Errorf("Recent(%d) len = %d; want %d", tc.n, len(got), tc.wantLen)
			continue
		}
		if got[0].Summary != tc.wantFirst {
			t.Errorf("Recent(%d)[0].Summary = %q; want %q", tc.n, got[0].Summary, tc.wantFirst)
		}
	}
}

// ---------------------------------------------------------------------------
// Malformed lines are skipped, not fatal.
// Write one valid JSON line and one garbage line; only the valid entry parses.
// ---------------------------------------------------------------------------

func TestRecentSkipsMalformedLines(t *testing.T) {
	isoHome(t)

	// Write a valid entry first via Append.
	if err := Append(Entry{Type: SessionStarted, Summary: "valid"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Manually append a garbage line to the log file.
	logFile := LogPath()
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open log for garbage write: %v", err)
	}
	if _, err := f.WriteString("THIS IS NOT JSON\n"); err != nil {
		f.Close()
		t.Fatalf("write garbage line: %v", err)
	}
	f.Close()

	got, err := Recent(0)
	if err != nil {
		t.Fatalf("Recent(0): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Recent(0) = %d entries; want 1 (garbage line must be skipped)", len(got))
	}
	if got[0].Summary != "valid" {
		t.Errorf("entry.Summary = %q; want %q", got[0].Summary, "valid")
	}
}

// ---------------------------------------------------------------------------
// Blank lines are skipped (the len(line)==0 branch in the scanner).
// ---------------------------------------------------------------------------

func TestRecentSkipsBlankLines(t *testing.T) {
	isoHome(t)

	if err := Append(Entry{Type: ModelChanged, Summary: "entry"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Manually insert blank lines around the valid entry.
	logFile := LogPath()
	existing, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	patched := "\n" + string(existing) + "\n\n"
	if err := os.WriteFile(logFile, []byte(patched), 0600); err != nil {
		t.Fatalf("rewrite log with blanks: %v", err)
	}

	got, err := Recent(0)
	if err != nil {
		t.Fatalf("Recent(0): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Recent(0) = %d entries; want 1 (blank lines must be skipped)", len(got))
	}
	if got[0].Summary != "entry" {
		t.Errorf("entry.Summary = %q; want %q", got[0].Summary, "entry")
	}
}

// ---------------------------------------------------------------------------
// Append fills a zero Timestamp with time.Now().UTC().
// ---------------------------------------------------------------------------

func TestAppendFillsZeroTimestamp(t *testing.T) {
	isoHome(t)

	before := time.Now().UTC().Add(-time.Second) // small buffer for clock skew

	// Entry with zero Timestamp — Append must fill it.
	if err := Append(Entry{Type: AgentDispatched, Summary: "ts-fill"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	after := time.Now().UTC().Add(time.Second)

	got, err := Recent(1)
	if err != nil {
		t.Fatalf("Recent(1): %v", err)
	}
	if len(got) == 0 {
		t.Fatal("Recent(1) returned no entries")
	}
	ts := got[0].Timestamp
	if ts.IsZero() {
		t.Fatal("persisted Timestamp is zero; Append should have filled it")
	}
	if ts.Before(before) || ts.After(after) {
		t.Errorf("Timestamp %v is outside expected window [%v, %v]", ts, before, after)
	}
}

// ---------------------------------------------------------------------------
// Clear truncates an existing log to zero bytes; Recent returns nil/empty after.
// Clear on a non-existent file is a no-op returning nil.
// ---------------------------------------------------------------------------

func TestClearExistingLog(t *testing.T) {
	isoHome(t)

	if err := Append(Entry{Type: ProjectCreated, Summary: "to clear"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Sanity-check: entry is there.
	before, err := Recent(0)
	if err != nil {
		t.Fatalf("Recent before Clear: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("expected entry before Clear; got none")
	}

	if err := Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	// After Clear, log file exists but is zero bytes — Recent opens it and
	// finds no lines, so it returns nil (no entries parsed).
	after, err := Recent(0)
	if err != nil {
		t.Fatalf("Recent after Clear: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("Recent after Clear = %d entries; want 0", len(after))
	}
}

func TestClearNonExistentFileIsNoOp(t *testing.T) {
	isoHome(t)

	// Log file does not exist yet — Clear must return nil without creating it.
	if err := Clear(); err != nil {
		t.Fatalf("Clear on non-existent file returned error: %v", err)
	}

	// Confirm the file was NOT created as a side effect.
	if _, err := os.Stat(LogPath()); !os.IsNotExist(err) {
		t.Errorf("Clear on non-existent file created the file unexpectedly")
	}
}

// ---------------------------------------------------------------------------
// Log and LogWithDetails convenience wrappers — smoke-test that they persist.
// ---------------------------------------------------------------------------

func TestLogConvenienceWrapper(t *testing.T) {
	isoHome(t)

	Log(ErrorOccurred, "log-wrapper-test")

	got, err := Recent(1)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("Recent returned no entries after Log()")
	}
	if got[0].Summary != "log-wrapper-test" {
		t.Errorf("Summary = %q; want %q", got[0].Summary, "log-wrapper-test")
	}
	if got[0].Type != ErrorOccurred {
		t.Errorf("Type = %q; want %q", got[0].Type, ErrorOccurred)
	}
}

func TestLogWithDetailsConvenienceWrapper(t *testing.T) {
	isoHome(t)

	LogWithDetails(PhaseAdvanced, "phase-summary", "phase-details")

	got, err := Recent(1)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("Recent returned no entries after LogWithDetails()")
	}
	if got[0].Summary != "phase-summary" {
		t.Errorf("Summary = %q; want %q", got[0].Summary, "phase-summary")
	}
	if got[0].Details != "phase-details" {
		t.Errorf("Details = %q; want %q", got[0].Details, "phase-details")
	}
}

// ---------------------------------------------------------------------------
// LogPath derives from DojoDir, which must sit under the isolated HOME.
// ---------------------------------------------------------------------------

func TestLogPathUnderDojoDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	p := LogPath()
	expected := filepath.Join(tmp, ".dojo", "activity.log")
	if p != expected {
		t.Errorf("LogPath() = %q; want %q", p, expected)
	}
}
