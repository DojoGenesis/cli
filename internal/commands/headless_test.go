package commands

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DojoGenesis/cli/internal/client"
	"github.com/DojoGenesis/cli/internal/config"
)

// newHeadlessTestRegistry builds a Registry the way the one-shot path does, with
// a client pointed at a dead URL (the cases here never reach the network). HOME
// is isolated package-wide by TestMain, so config.Load returns safe defaults.
func newHeadlessTestRegistry(t *testing.T) *Registry {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	gw := client.New(cfg.Gateway.URL, cfg.Gateway.Token, cfg.Gateway.Timeout)
	session := ""
	return New(cfg, gw, nil, &session)
}

// An unknown command returns a well-formed error envelope: ok=false, no data,
// error.code="unknown_command" — the signal a headless caller maps to exit 2.
func TestRunHeadlessUnknownCommand(t *testing.T) {
	reg := newHeadlessTestRegistry(t)
	env := reg.RunHeadless(context.Background(), "definitelynotacommand")

	if env.OK {
		t.Fatal("unknown command reported ok=true")
	}
	if env.Command != "definitelynotacommand" {
		t.Fatalf("command = %q", env.Command)
	}
	if env.Data != nil {
		t.Fatalf("error envelope carried data: %v", env.Data)
	}
	if env.Error == nil || env.Error.Code != "unknown_command" {
		t.Fatalf("error = %+v, want code unknown_command", env.Error)
	}

	// And the whole envelope round-trips through JSON (what an agent parses).
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back["ok"] != false {
		t.Fatalf("round-trip ok = %v", back["ok"])
	}
}

// Dispatch still surfaces the sentinel so the human/plain path can map exit
// codes with errors.Is (RunHeadless and main.go share this classification).
func TestDispatchUnknownIsSentinel(t *testing.T) {
	reg := newHeadlessTestRegistry(t)
	err := reg.Dispatch(context.Background(), "definitelynotacommand")
	if !errors.Is(err, ErrUnknownCommand) {
		t.Fatalf("err = %v, want wrapped ErrUnknownCommand", err)
	}
}

// Commands() is the discovery source of truth: non-empty, sorted, and every
// entry names a registered command.
func TestCommandsDiscovery(t *testing.T) {
	reg := newHeadlessTestRegistry(t)
	cmds := reg.Commands()
	if len(cmds) == 0 {
		t.Fatal("Commands() returned nothing")
	}
	for i := 1; i < len(cmds); i++ {
		if cmds[i-1].Name > cmds[i].Name {
			t.Fatalf("Commands() not sorted at %d: %q > %q", i, cmds[i-1].Name, cmds[i].Name)
		}
	}
	for _, c := range cmds {
		if c.Name == "" {
			t.Fatal("Commands() entry with empty name")
		}
		if _, ok := reg.cmds[c.Name]; !ok {
			t.Fatalf("Commands() lists %q which is not registered", c.Name)
		}
	}
}
