// Package guardrail implements an advisory circuit breaker for the human
// REPL. It counts consecutive failures per caller-defined key and
// escalates a ready-to-print one-liner from silence, to a warning, to a
// "you're stuck" hard-stop message. It never blocks or refuses an action —
// Dojo's guardrail only gets louder, matching the advisory contract on
// internal/config.GuardrailsConfig (Enabled/WarnAfter/HardAfter, default
// enabled/3/5).
//
// Inspiration: Hermes' tool_loop_guardrails, which warn/hard-stop a tool
// loop after N exact_failure / same_tool_failure / idempotent_no_progress
// outcomes against the same key — a stuck-loop circuit breaker. Dojo's
// Tracker is the counting primitive underneath that idea, scoped down for
// a human-in-the-loop REPL: callers decide what a "key" is (a command
// string, a tool+error signature, ...) and what "failed" means at their
// call site; the tracker only ever advises, never blocks.
package guardrail

import (
	"fmt"
	"strings"
	"sync"
)

// Level is the escalation tier returned by Tracker.Record.
type Level int

const (
	// None means no advice: the tracker is disabled, the outcome was a
	// success (which resets the key), or the streak hasn't reached
	// WarnAfter yet.
	None Level = iota
	// Warn means the key has failed exactly WarnAfter times in a row.
	Warn
	// Hard means the key has failed HardAfter or more times in a row —
	// a stuck loop. Returned every time the count is at or past
	// HardAfter, so a truly stuck loop keeps being told.
	Hard
)

// Advice is the result of one Tracker.Record call: an escalation Level and
// a ready-to-print message. Msg is empty when Level == None.
type Advice struct {
	Level Level
	Msg   string // ready-to-print one-liner; empty when Level == None
}

// Tracker counts CONSECUTIVE failures per key. A success for a key resets
// that key's counter to zero. Keys are caller-defined, e.g.
// "cmd:/code test" or "tool:web_fetch|conn refused" (see Signature for
// building the error-derived half of such a key). Safe for concurrent use.
type Tracker struct {
	mu        sync.Mutex
	counts    map[string]int
	warnAfter int
	hardAfter int
	enabled   bool
}

// New builds a Tracker. Sanity clamps guard against a malformed config
// producing a tracker that never fires or fires nonsensically:
// warnAfter < 1 becomes 3; hardAfter <= warnAfter becomes warnAfter + 2 —
// the same 3/5 shape internal/config.GuardrailsConfig defaults to.
func New(warnAfter, hardAfter int, enabled bool) *Tracker {
	if warnAfter < 1 {
		warnAfter = 3
	}
	if hardAfter <= warnAfter {
		hardAfter = warnAfter + 2
	}
	return &Tracker{
		counts:    make(map[string]int),
		warnAfter: warnAfter,
		hardAfter: hardAfter,
		enabled:   enabled,
	}
}

// Record registers one outcome for key and returns advice. Rules:
//
//   - disabled:     always Advice{Level: None}.
//   - failed=false: resets key's counter, returns None.
//   - failed=true:  increments key's counter; at exactly warnAfter
//     returns Warn; at and after hardAfter returns Hard (every time, so a
//     truly stuck loop keeps being told); strictly between warnAfter and
//     hardAfter, returns None.
func (t *Tracker) Record(key string, failed bool) Advice {
	if !t.enabled {
		return Advice{Level: None}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if !failed {
		delete(t.counts, key)
		return Advice{Level: None}
	}

	t.counts[key]++
	n := t.counts[key]

	switch {
	case n >= t.hardAfter:
		return Advice{
			Level: Hard,
			Msg:   fmt.Sprintf("guardrail: %s is stuck (%d consecutive identical failures) — stop and change strategy, or check config vs code (see debugging gate)", key, n),
		}
	case n == t.warnAfter:
		return Advice{
			Level: Warn,
			Msg:   fmt.Sprintf("guardrail: %s has failed %d times consecutively with the same signature — consider a different approach", key, n),
		}
	default:
		return Advice{Level: None}
	}
}

// Signature normalizes an error string into a stable short key component:
// lowercase, whitespace runs collapsed to a single space, truncated to the
// first 80 bytes. Helper for callers building keys, e.g.
// "tool:web_fetch|" + Signature(err.Error()).
func Signature(err string) string {
	s := strings.ToLower(err)
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}
