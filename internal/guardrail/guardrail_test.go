package guardrail

import (
	"fmt"
	"strings"
	"testing"
)

func TestRecord_ResetOnSuccess(t *testing.T) {
	tr := New(3, 5, true)

	tr.Record("k", true)
	tr.Record("k", true)
	if adv := tr.Record("k", false); adv.Level != None || adv.Msg != "" {
		t.Fatalf("Record(success) = %+v; want {None \"\"}", adv)
	}

	// Counter must have reset to 0: two more failures should NOT reach
	// warnAfter (3) yet.
	tr.Record("k", true)
	adv := tr.Record("k", true)
	if adv.Level != None {
		t.Fatalf("after reset, 2nd failure Level = %v; want None (not yet at warnAfter)", adv.Level)
	}
}

func TestRecord_WarnFiresExactlyOnceAtThreshold(t *testing.T) {
	tr := New(3, 5, true)

	var warnCount int
	var levels []Level
	for i := 0; i < 5; i++ {
		adv := tr.Record("k", true)
		levels = append(levels, adv.Level)
		if adv.Level == Warn {
			warnCount++
			want := fmt.Sprintf("guardrail: k has failed %d times consecutively with the same signature — consider a different approach", i+1)
			if adv.Msg != want {
				t.Fatalf("Warn Msg = %q; want %q", adv.Msg, want)
			}
		}
	}
	if warnCount != 1 {
		t.Fatalf("warnCount over 5 consecutive failures = %d; want exactly 1 (levels=%v)", warnCount, levels)
	}
	if levels[2] != Warn { // 3rd consecutive failure (0-indexed: i==2)
		t.Fatalf("levels[2] (3rd failure) = %v; want Warn", levels[2])
	}
}

func TestRecord_HardFiresAtAndAfterHardAfter(t *testing.T) {
	tr := New(3, 5, true)

	for i := 0; i < 4; i++ {
		adv := tr.Record("k", true)
		if adv.Level == Hard {
			t.Fatalf("failure #%d prematurely returned Hard", i+1)
		}
	}

	adv := tr.Record("k", true) // 5th consecutive failure == hardAfter
	if adv.Level != Hard {
		t.Fatalf("5th consecutive failure Level = %v; want Hard", adv.Level)
	}
	want := "guardrail: k is stuck (5 consecutive identical failures) — stop and change strategy, or check config vs code (see debugging gate)"
	if adv.Msg != want {
		t.Fatalf("Hard Msg = %q; want %q", adv.Msg, want)
	}

	// A truly stuck loop keeps being told: 6th, 7th failures stay Hard.
	for i, wantCount := range []int{6, 7} {
		adv := tr.Record("k", true)
		if adv.Level != Hard {
			t.Fatalf("failure #%d Level = %v; want Hard (past hardAfter)", wantCount, adv.Level)
		}
		want := fmt.Sprintf("guardrail: k is stuck (%d consecutive identical failures) — stop and change strategy, or check config vs code (see debugging gate)", wantCount)
		if adv.Msg != want {
			t.Fatalf("failure #%d (i=%d) Msg = %q; want %q", wantCount, i, adv.Msg, want)
		}
	}
}

func TestRecord_Disabled(t *testing.T) {
	// warnAfter=1, hardAfter=3 would fire immediately if enabled -- proves
	// the disabled short-circuit, not just a threshold never reached.
	tr := New(1, 3, false)

	for i := 0; i < 5; i++ {
		adv := tr.Record("k", true)
		if adv.Level != None || adv.Msg != "" {
			t.Fatalf("disabled tracker Record #%d = %+v; want {None \"\"}", i, adv)
		}
	}
}

func TestNew_Clamps(t *testing.T) {
	tests := []struct {
		name          string
		warnAfter     int
		hardAfter     int
		wantWarnAfter int
		wantHardAfter int
	}{
		{"warnAfter zero", 0, 5, 3, 5},
		{"warnAfter negative", -1, 5, 3, 5},
		{"hardAfter equal warnAfter", 3, 3, 3, 5},
		{"hardAfter less than warnAfter", 4, 2, 4, 6},
		{"both invalid", 0, 0, 3, 5},
		{"already valid, untouched", 2, 4, 2, 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr := New(tc.warnAfter, tc.hardAfter, true)
			if tr.warnAfter != tc.wantWarnAfter {
				t.Errorf("warnAfter = %d; want %d", tr.warnAfter, tc.wantWarnAfter)
			}
			if tr.hardAfter != tc.wantHardAfter {
				t.Errorf("hardAfter = %d; want %d", tr.hardAfter, tc.wantHardAfter)
			}
		})
	}
}

func TestSignature(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"lowercases", "Connection REFUSED", "connection refused"},
		{"collapses whitespace", "conn   refused\n\tretry", "conn refused retry"},
		{"truncates to first 80 bytes", strings.Repeat("a", 100), strings.Repeat("a", 80)},
		{"empty string", "", ""},
		{"already short and clean", "conn refused", "conn refused"},
		{"leading and trailing whitespace", "  conn refused  ", "conn refused"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Signature(tc.in)
			if got != tc.want {
				t.Errorf("Signature(%q) = %q; want %q", tc.in, got, tc.want)
			}
			if len(got) > 80 {
				t.Errorf("Signature(%q) len = %d; want <= 80", tc.in, len(got))
			}
		})
	}
}

// TestRecord_Concurrency runs many t.Parallel goroutines hammering Record
// against a shared Tracker (some sharing keys, to exercise the mutex on
// the same map entries). It makes no assertion on final counts -- ordering
// across goroutines is nondeterministic by design -- its job is to prove
// no data race and no panic. Run locally with the race detector:
//
//	go test ./internal/guardrail -count=1 -race
func TestRecord_Concurrency(t *testing.T) {
	tr := New(3, 5, true)
	keys := []string{"a", "b", "c", "d"}

	for g := 0; g < 20; g++ {
		g := g
		t.Run(fmt.Sprintf("worker-%d", g), func(t *testing.T) {
			t.Parallel()
			key := keys[g%len(keys)]
			for i := 0; i < 200; i++ {
				_ = tr.Record(key, i%2 == 0)
			}
			_ = Signature("concurrent CALL failing\tagain")
		})
	}
}
