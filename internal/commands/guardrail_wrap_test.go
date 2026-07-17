package commands

// guardrail_wrap_test.go — tests for the advisory guardrail wrap around
// Registry.Dispatch (see guardAdvise in commands.go). NEW file per the
// advancement-wave ownership rules; existing test files are not modified.
// TestMain in commands_test.go (same package) already isolates $HOME, so the
// activity log writes from successful dispatches stay out of the real ~/.dojo.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/DojoGenesis/cli/internal/config"
)

// guardTestSetup builds a bare Registry (no gateway, no real commands) with a
// single fake "boom" command whose failure is switchable per dispatch, plus a
// capture slice wired into the advicePrint seam.
func guardTestSetup(cfg *config.Config) (r *Registry, printed *[]string, fail *bool) {
	var lines []string
	f := true
	r = &Registry{
		cfg:  cfg,
		cmds: make(map[string]Command),
		advicePrint: func(msg string) {
			lines = append(lines, msg)
		},
	}
	r.cmds["boom"] = Command{
		Name:    "boom",
		Aliases: []string{"b"},
		Short:   "test command that fails on demand",
		Run: func(ctx context.Context, args []string) error {
			if f {
				return errors.New("kaboom")
			}
			return nil
		},
	}
	return r, &lines, &f
}

func warnMsg(key string, n int) string {
	return fmt.Sprintf("guardrail: %s has failed %d times consecutively with the same signature — consider a different approach", key, n)
}

func hardMsg(key string, n int) string {
	return fmt.Sprintf("guardrail: %s is stuck (%d consecutive identical failures) — stop and change strategy, or check config vs code (see debugging gate)", key, n)
}

// TestDispatchGuardrail_WarnHardAndReset drives the fake failing command
// through Dispatch and asserts warn fires exactly at WarnAfter, hard fires at
// and after HardAfter, and a success resets the streak.
func TestDispatchGuardrail_WarnHardAndReset(t *testing.T) {
	cfg := &config.Config{
		Guardrails: config.GuardrailsConfig{Enabled: true, WarnAfter: 2, HardAfter: 4},
	}
	key := "cmd:boom go"

	steps := []struct {
		name       string
		fail       bool
		wantAdvice string // "" = no advice line expected from this dispatch
	}{
		{"failure 1: below warn", true, ""},
		{"failure 2: warn fires at WarnAfter", true, warnMsg(key, 2)},
		{"failure 3: between warn and hard", true, ""},
		{"failure 4: hard fires at HardAfter", true, hardMsg(key, 4)},
		{"failure 5: hard keeps firing past HardAfter", true, hardMsg(key, 5)},
		{"success: resets, no advice", false, ""},
		{"failure 1 after reset: below warn", true, ""},
		{"failure 2 after reset: warn fires again", true, warnMsg(key, 2)},
	}

	r, printed, fail := guardTestSetup(cfg)
	for _, step := range steps {
		*fail = step.fail
		before := len(*printed)
		err := r.Dispatch(context.Background(), "boom go")
		if step.fail && err == nil {
			t.Fatalf("%s: Dispatch returned nil error; the command error must pass through unchanged", step.name)
		}
		if !step.fail && err != nil {
			t.Fatalf("%s: Dispatch returned %v; want nil", step.name, err)
		}
		got := (*printed)[before:]
		if step.wantAdvice == "" {
			if len(got) != 0 {
				t.Fatalf("%s: unexpected advice %q", step.name, got)
			}
			continue
		}
		if len(got) != 1 || got[0] != step.wantAdvice {
			t.Fatalf("%s: advice = %q; want exactly [%q]", step.name, got, step.wantAdvice)
		}
	}
}

// TestDispatchGuardrail_SubcommandKeysIndependent verifies that the first
// subcommand token is part of the key, so different subcommands hold
// independent streaks, and that a bare command keys without a trailing space.
func TestDispatchGuardrail_SubcommandKeysIndependent(t *testing.T) {
	cfg := &config.Config{
		Guardrails: config.GuardrailsConfig{Enabled: true, WarnAfter: 2, HardAfter: 4},
	}
	r, printed, _ := guardTestSetup(cfg) // fail stays true

	// Alternate subcommands: each key sees one failure at a time, so nothing
	// may fire until a single key reaches 2.
	for _, in := range []string{"boom alpha", "boom beta", "boom alpha"} {
		if err := r.Dispatch(context.Background(), in); err == nil {
			t.Fatalf("Dispatch(%q) = nil error; want failure", in)
		}
	}
	if len(*printed) != 1 || (*printed)[0] != warnMsg("cmd:boom alpha", 2) {
		t.Fatalf("advice = %q; want exactly [%q]", *printed, warnMsg("cmd:boom alpha", 2))
	}

	// Bare command (no args) keys as just "cmd:boom".
	r2, printed2, _ := guardTestSetup(cfg)
	r2.Dispatch(context.Background(), "boom")
	r2.Dispatch(context.Background(), "boom")
	if len(*printed2) != 1 || (*printed2)[0] != warnMsg("cmd:boom", 2) {
		t.Fatalf("bare-command advice = %q; want exactly [%q]", *printed2, warnMsg("cmd:boom", 2))
	}
}

// TestDispatchGuardrail_AliasSharesCanonicalKey verifies dispatching via an
// alias feeds the same streak as the canonical name (key uses cmd.Name).
func TestDispatchGuardrail_AliasSharesCanonicalKey(t *testing.T) {
	cfg := &config.Config{
		Guardrails: config.GuardrailsConfig{Enabled: true, WarnAfter: 2, HardAfter: 4},
	}
	r, printed, _ := guardTestSetup(cfg)

	r.Dispatch(context.Background(), "boom go") // canonical: streak 1
	r.Dispatch(context.Background(), "b go")    // alias: same key, streak 2 → warn
	if len(*printed) != 1 || (*printed)[0] != warnMsg("cmd:boom go", 2) {
		t.Fatalf("advice = %q; want exactly [%q] (alias must share the canonical key)", *printed, warnMsg("cmd:boom go", 2))
	}
}

// TestDispatchGuardrail_DisabledAndNilCfg verifies the disabled short-circuit
// and that a nil cfg is treated as disabled (no panic, no advice).
func TestDispatchGuardrail_DisabledAndNilCfg(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Config
	}{
		{"explicitly disabled", &config.Config{
			Guardrails: config.GuardrailsConfig{Enabled: false, WarnAfter: 1, HardAfter: 2},
		}},
		{"nil cfg", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, printed, _ := guardTestSetup(tc.cfg)
			for i := 0; i < 6; i++ {
				if err := r.Dispatch(context.Background(), "boom go"); err == nil {
					t.Fatal("Dispatch = nil error; want failure")
				}
			}
			if len(*printed) != 0 {
				t.Fatalf("advice = %q; want none", *printed)
			}
		})
	}
}

// TestDispatchGuardrail_UnknownCommandNotTracked verifies that an unknown
// command (which never reaches a handler) records nothing — the guardrail
// wraps handler outcomes, not typos.
func TestDispatchGuardrail_UnknownCommandNotTracked(t *testing.T) {
	cfg := &config.Config{
		Guardrails: config.GuardrailsConfig{Enabled: true, WarnAfter: 1, HardAfter: 3},
	}
	r, printed, _ := guardTestSetup(cfg)
	for i := 0; i < 3; i++ {
		if err := r.Dispatch(context.Background(), "nosuchcmd"); err == nil {
			t.Fatal("Dispatch(unknown) = nil error; want error")
		}
	}
	if len(*printed) != 0 {
		t.Fatalf("advice = %q; want none for unknown commands", *printed)
	}
}
