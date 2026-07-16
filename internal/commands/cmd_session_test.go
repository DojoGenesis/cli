package commands

// cmd_session_test.go — unit tests for W5-RESUME: /session ls,
// /session resume <id>, and the pure resume-by-id warning logic they share.
//
// commands_test.go's TestMain already points $HOME at a shared temp dir for
// the whole test binary, and its existing /session tests (TestSessionNew,
// TestSessionResume, TestSessionResumeNoPrior) deliberately tolerate
// order-dependent state.json content ("no panic" rather than exact
// assertions). The tests below make precise assertions about history
// contents and ordering, so each one gets its own isolated $HOME via
// t.Setenv — same pattern internal/state/state_test.go uses, and safe here
// because none of these call t.Parallel().

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/DojoGenesis/cli/internal/state"
)

// overrideSessionStateDir points ~/.dojo/state.json at a fresh temp dir for
// the duration of one test. t.Setenv restores the previous $HOME
// automatically on cleanup.
func overrideSessionStateDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	return filepath.Join(tmp, ".dojo", "state.json")
}

// ─── sessionResumeWarning (pure logic — no disk I/O) ────────────────────────

func TestSessionResumeWarningMalformed(t *testing.T) {
	cases := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"whitespace-only", "   "},
		{"too-short", "ab"},
		{"shell-fragment", "$(rm -rf /)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sessionResumeWarning(tc.id, nil)
			if got == "" {
				t.Errorf("sessionResumeWarning(%q, nil) = \"\"; want a malformed-id warning", tc.id)
			}
		})
	}
}

func TestSessionResumeWarningNotFound(t *testing.T) {
	hist := []state.SessionEntry{
		{ID: "dojo-cli-20260101-000000", SavedAt: "2026-01-01T00:00:00Z"},
		{ID: "dojo-cli-20260102-000000", SavedAt: "2026-01-02T00:00:00Z"},
	}
	got := sessionResumeWarning("dojo-cli-99999999-999999", hist)
	want := `"dojo-cli-99999999-999999" not found in local session history`
	if got != want {
		t.Errorf("sessionResumeWarning() = %q; want %q", got, want)
	}
}

func TestSessionResumeWarningFound(t *testing.T) {
	hist := []state.SessionEntry{
		{ID: "dojo-cli-20260101-000000", SavedAt: "2026-01-01T00:00:00Z"},
		{ID: "dojo-cli-20260102-000000", SavedAt: "2026-01-02T00:00:00Z"},
	}
	got := sessionResumeWarning("dojo-cli-20260102-000000", hist)
	if got != "" {
		t.Errorf("sessionResumeWarning() = %q; want \"\" for an id present in history", got)
	}
}

func TestSessionResumeWarningEmptyHistory(t *testing.T) {
	got := sessionResumeWarning("dojo-cli-20260102-000000", nil)
	if got == "" {
		t.Error("sessionResumeWarning() = \"\"; want a not-found warning against empty/nil history")
	}
}

// ─── sessionResumeByID (I/O path: warn-but-never-block) ─────────────────────

// TestSessionResumeByIDSwitchesEvenWhenUnknown verifies /session resume <id>
// never blocks — it warns but still switches — for an id absent from history.
func TestSessionResumeByIDSwitchesEvenWhenUnknown(t *testing.T) {
	overrideSessionStateDir(t)
	state.SaveSession("dojo-cli-known-session")

	session := "dojo-cli-known-session"
	if err := sessionResumeByID(&session, "dojo-cli-unknown-session"); err != nil {
		t.Fatalf("sessionResumeByID() error = %v; want nil (never blocks)", err)
	}
	if session != "dojo-cli-unknown-session" {
		t.Errorf("session = %q; want dojo-cli-unknown-session (switched despite not being in history)", session)
	}
}

// TestSessionResumeByIDKnownID verifies resuming an id that IS in history
// switches cleanly and persists as the new LastSessionID (so a subsequent
// bare --resume picks up exactly this session, not whatever was active
// before the resume-by-id call).
func TestSessionResumeByIDKnownID(t *testing.T) {
	overrideSessionStateDir(t)
	state.SaveSession("dojo-cli-first")
	state.SaveSession("dojo-cli-second")

	session := "dojo-cli-second"
	if err := sessionResumeByID(&session, "dojo-cli-first"); err != nil {
		t.Fatalf("sessionResumeByID() error = %v; want nil", err)
	}
	if session != "dojo-cli-first" {
		t.Errorf("session = %q; want dojo-cli-first", session)
	}

	st, err := state.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if st.LastSessionID != "dojo-cli-first" {
		t.Errorf("LastSessionID = %q; want dojo-cli-first", st.LastSessionID)
	}
}

// TestSessionResumeByIDEmptyState verifies resume-by-id still works (warns,
// switches, never errors) against a completely fresh workspace with no
// state.json at all.
func TestSessionResumeByIDEmptyState(t *testing.T) {
	overrideSessionStateDir(t)

	session := "dojo-cli-current"
	if err := sessionResumeByID(&session, "dojo-cli-brand-new"); err != nil {
		t.Fatalf("sessionResumeByID() error = %v; want nil", err)
	}
	if session != "dojo-cli-brand-new" {
		t.Errorf("session = %q; want dojo-cli-brand-new", session)
	}
}

// ─── sessionResumeLast (bare `/session resume`, unchanged behavior) ────────

// TestSessionResumeLastNoPriorSession verifies bare /session resume still
// errors when there's nothing to resume — same contract as before history
// existed.
func TestSessionResumeLastNoPriorSession(t *testing.T) {
	overrideSessionStateDir(t)

	session := "dojo-cli-current"
	if err := sessionResumeLast(&session); err == nil {
		t.Fatal("sessionResumeLast() error = nil; want an error when no prior session exists")
	}
}

// TestSessionResumeLastRestoresLastSessionID verifies bare /session resume
// restores whatever LastSessionID currently holds.
func TestSessionResumeLastRestoresLastSessionID(t *testing.T) {
	overrideSessionStateDir(t)
	state.SaveSession("dojo-cli-prior")

	session := "dojo-cli-current"
	if err := sessionResumeLast(&session); err != nil {
		t.Fatalf("sessionResumeLast() error = %v; want nil", err)
	}
	if session != "dojo-cli-prior" {
		t.Errorf("session = %q; want dojo-cli-prior", session)
	}
}

// ─── sessionLs ───────────────────────────────────────────────────────────────

// TestSessionLsEmptyHistory verifies /session ls doesn't error on a fresh
// workspace with no recorded sessions.
func TestSessionLsEmptyHistory(t *testing.T) {
	overrideSessionStateDir(t)

	if err := sessionLs("dojo-cli-current"); err != nil {
		t.Fatalf("sessionLs() error = %v; want nil for empty history", err)
	}
}

// TestSessionLsPopulatedHistory verifies /session ls doesn't error once
// sessions have been recorded, including when one of them is the active one.
func TestSessionLsPopulatedHistory(t *testing.T) {
	overrideSessionStateDir(t)
	state.SaveSession("dojo-cli-one")
	state.SaveSession("dojo-cli-two")

	if err := sessionLs("dojo-cli-two"); err != nil {
		t.Fatalf("sessionLs() error = %v; want nil", err)
	}
}

// ─── /session ls and /session resume <id> via full dispatch ────────────────
//
// testRegistry (defined in commands_test.go, same package) builds a
// Registry with gw=nil; /session's subcommands are purely client-side so
// they're safe to dispatch without a gateway, matching that file's existing
// "sessionCmd integration" tests.

// TestDispatchSessionLs verifies `/session ls` reaches sessionLs through the
// full command-dispatch path, not just the extracted helper directly.
func TestDispatchSessionLs(t *testing.T) {
	overrideSessionStateDir(t)
	r, session := testRegistry()
	state.SaveSession(*session)

	if err := r.Dispatch(context.Background(), "session ls"); err != nil {
		t.Fatalf("Dispatch(session ls) error = %v; want nil", err)
	}
}

// TestDispatchSessionResumeByID verifies `/session resume <id>` reaches
// sessionResumeByID through the full command-dispatch path and updates the
// Registry's session pointer.
func TestDispatchSessionResumeByID(t *testing.T) {
	overrideSessionStateDir(t)
	r, session := testRegistry()
	state.SaveSession("dojo-cli-target")

	if err := r.Dispatch(context.Background(), "session resume dojo-cli-target"); err != nil {
		t.Fatalf("Dispatch(session resume <id>) error = %v; want nil", err)
	}
	if *session != "dojo-cli-target" {
		t.Errorf("session = %q; want dojo-cli-target", *session)
	}
}

// TestDispatchSessionResumeBareStillWorks verifies bare `/session resume`
// (no id) still reaches sessionResumeLast — the pre-existing behavior — not
// the new resume-by-id path, when dispatched through the switch in
// sessionCmd's Run closure.
func TestDispatchSessionResumeBareStillWorks(t *testing.T) {
	overrideSessionStateDir(t)
	r, session := testRegistry()
	state.SaveSession("dojo-cli-last-active")

	if err := r.Dispatch(context.Background(), "session resume"); err != nil {
		t.Fatalf("Dispatch(session resume) error = %v; want nil", err)
	}
	if *session != "dojo-cli-last-active" {
		t.Errorf("session = %q; want dojo-cli-last-active", *session)
	}
}
