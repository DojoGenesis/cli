package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DojoGenesis/cli/internal/ioutilx"
)

// DefaultGatewayURL is the fallback gateway address used by both config defaults
// and the bootstrap initializer so the value is never duplicated.
const DefaultGatewayURL = "http://localhost:7340"

// Config is the dojo CLI configuration, loaded from ~/.dojo/settings.json.
type Config struct {
	Gateway             GatewayConfig                `json:"gateway"`
	Plugins             PluginsConfig                `json:"plugins"`
	Defaults            DefaultsConfig               `json:"defaults"`
	Protocol            ProtocolConfig               `json:"protocol"`
	Verify              VerifyConfig                 `json:"verify"`
	Auth                AuthConfig                   `json:"auth,omitempty"`
	Permissions         PermissionsConfig            `json:"permissions"`
	Delegation          DelegationConfig             `json:"delegation"`
	Guardrails          GuardrailsConfig             `json:"guardrails"`
	Skills              SkillsConfig                 `json:"skills"`
	DispositionProfiles map[string]DispositionPreset `json:"disposition_profiles,omitempty"`

	// envOverrides records which fields Load() populated from an environment
	// variable (DOJO_GATEWAY_URL, DOJO_PROTOCOL_DISABLED, etc.), and what each
	// field held immediately before that override was applied. Unexported so
	// encoding/json never serializes it — Save() reads it to keep env-transient
	// overrides out of settings.json. See envOverride and Save() for why this
	// exists and noteEnvOverride() for how it's populated.
	envOverrides []envOverride
}

// ProtocolConfig controls the workspace "genius protocol" carried onto every
// chat/agent turn. Enabled defaults to TRUE (set in defaults(), merged over by
// any settings.json value): because defaults() pre-seeds true and json.Unmarshal
// only overwrites keys that are present, a settings.json that omits the whole
// block — or omits just "enabled" — keeps the protocol on. Path optionally
// points at an explicit override doc; empty means "resolve ./DOJO.md, then
// ~/.dojo/DOJO.md, then the embedded default".
type ProtocolConfig struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path,omitempty"`
}

// VerifyConfig controls the opt-in "done means verified" gate that runs after a
// successful /agent dispatch or /run in the interactive REPL. AfterAgent
// defaults to FALSE — the loop is strictly opt-in — so a settings.json that
// omits the whole block (or omits just "after_agent") leaves it off: unlike
// ProtocolConfig.Enabled, the intended default here IS the zero value, so
// defaults() need not pre-seed it. DOJO_VERIFY_AFTER_AGENT (any non-empty
// value) flips it on for a single run, resolved in Load like the other env
// knobs.
type VerifyConfig struct {
	AfterAgent bool `json:"after_agent"`
}

type AuthConfig struct {
	UserID string `json:"user_id,omitempty"`
}

type GatewayConfig struct {
	URL     string `json:"url"`
	Timeout string `json:"timeout"`
	Token   string `json:"token"` // optional bearer token
}

type PluginsConfig struct {
	Path string `json:"path"`
}

type DefaultsConfig struct {
	Provider    string `json:"provider"`
	Disposition string `json:"disposition"`
	Model       string `json:"model"`
}

// PermissionsConfig controls the CLI's action-permission gate: which command
// patterns require an interactive prompt before running. Mode selects the
// gating strategy — "default" prompts per the harness's normal per-command
// rules, "allowlist" skips the prompt only for patterns listed in Allowed,
// "yolo" skips every prompt. Mode defaults to "default" (pre-seeded in
// defaults()); Allowed holds glob-style command patterns, e.g. "code.undo",
// "plugin.install", "craft.*". See --yolo in cmd/dojo/main.go, which sets
// Mode to "yolo" in memory for the run only and never persists it via
// Save() — the same transient-override discipline as the env-overridden
// fields below (see envOverride and Save()), just triggered by a flag
// instead of an environment variable.
type PermissionsConfig struct {
	Mode    string   `json:"mode"`
	Allowed []string `json:"allowed"`
}

// DelegationConfig controls the default model used for agent dispatch (the
// cheap lane for sub-agent work) — distinct from Defaults.Model, which is
// the interactive chat default. Model defaults to "" (unset): an empty
// value means dispatch call sites fall back to their own default rather
// than a config-pinned one.
type DelegationConfig struct {
	Model string `json:"model"`
}

// GuardrailsConfig controls the repeated-failure nudge (warn, then hard
// stop) surfaced during long-running command loops. Enabled is a plain bool
// — not *bool — so it can reuse the exact "pre-seed true in defaults(), let
// json.Unmarshal only overwrite keys actually present" merge trick already
// documented on ProtocolConfig, rather than introduce a second optional-bool
// convention in this file (a *bool field literally named Enabled would also
// collide with an Enabled() accessor method — Go forbids a type having both
// a field and a method of the same name). A settings.json that omits the
// whole block, or omits just "enabled", keeps guardrails on; only an
// explicit "enabled": false turns them off. WarnAfter/HardAfter default to
// 3/5, also pre-seeded in defaults().
type GuardrailsConfig struct {
	Enabled   bool `json:"enabled"`
	WarnAfter int  `json:"warn_after"`
	HardAfter int  `json:"hard_after"`
}

// SkillsConfig controls skill discovery beyond the gateway-hosted CAS skill
// set. ExternalDirs lists additional local directories to scan for
// SKILL.md-bearing skill folders, defaulting to [".claude/skills"]
// (pre-seeded in defaults()). This is a different knob from DOJO_SKILLS_PATH
// (see internal/commands/cmd_workflow.go's `/skill package-all`), which
// names a single directory to push to the gateway CAS, not a discovery
// search path — no config section existed for skills before this one.
type SkillsConfig struct {
	ExternalDirs []string `json:"external_dirs"`
}

// envOverride records one Config field that Load() populated from an
// environment variable: get/set read and write the field, preEnv is the
// value the field held immediately before the override was applied (from
// settings.json or defaults()), and envVal is the value the override wrote.
//
// Save() compares get(c) against envVal to tell "still env-controlled"
// (nothing has touched the field since Load(), so persist preEnv instead)
// apart from "explicitly changed since Load()" (e.g. /model set mutating
// Defaults.Model directly — a real edit, so persist the new value as-is).
// See Save() for the full rationale: this is what stops a one-off env var
// like DOJO_PROTOCOL_DISABLED=1 from getting permanently baked into
// settings.json the next time anything calls Save().
type envOverride struct {
	get    func(*Config) any
	set    func(*Config, any)
	preEnv any
	envVal any
}

// noteEnvOverride applies newVal to a Config field via set, after
// snapshotting the field's current (pre-override) value via get, and
// records both on cfg.envOverrides for Save() to consult later. Call this
// instead of assigning the field directly whenever Load() applies an env
// var override — see envOverride and Save().
func noteEnvOverride[T comparable](cfg *Config, get func(*Config) T, set func(*Config, T), newVal T) {
	pre := get(cfg)
	set(cfg, newVal)
	cfg.envOverrides = append(cfg.envOverrides, envOverride{
		get:    func(c *Config) any { return get(c) },
		set:    func(c *Config, v any) { set(c, v.(T)) },
		preEnv: pre,
		envVal: newVal,
	})
}

// Load reads ~/.dojo/settings.json, applying environment variable overrides.
// Missing file is not an error — defaults are returned.
func Load() (*Config, error) {
	cfg := defaults()

	path := settingsPath()
	data, err := os.ReadFile(path)
	if err == nil {
		if jsonErr := json.Unmarshal(data, cfg); jsonErr != nil {
			return nil, jsonErr
		}
	}

	// Environment overrides. Each is applied via noteEnvOverride (not a plain
	// field assignment) so Save() can keep these transient, run-scoped values
	// out of settings.json instead of silently baking them in permanently —
	// see envOverride and Save().
	if v := os.Getenv("DOJO_GATEWAY_URL"); v != "" {
		noteEnvOverride(cfg,
			func(c *Config) string { return c.Gateway.URL },
			func(c *Config, v string) { c.Gateway.URL = v },
			v)
	}
	if v := os.Getenv("DOJO_GATEWAY_TOKEN"); v != "" {
		noteEnvOverride(cfg,
			func(c *Config) string { return c.Gateway.Token },
			func(c *Config, v string) { c.Gateway.Token = v },
			v)
	}
	if v := os.Getenv("DOJO_PLUGINS_PATH"); v != "" {
		noteEnvOverride(cfg,
			func(c *Config) string { return c.Plugins.Path },
			func(c *Config, v string) { c.Plugins.Path = v },
			v)
	}
	if v := os.Getenv("DOJO_PROVIDER"); v != "" {
		noteEnvOverride(cfg,
			func(c *Config) string { return c.Defaults.Provider },
			func(c *Config, v string) { c.Defaults.Provider = v },
			v)
	}
	if v := os.Getenv("DOJO_DISPOSITION"); v != "" {
		noteEnvOverride(cfg,
			func(c *Config) string { return c.Defaults.Disposition },
			func(c *Config, v string) { c.Defaults.Disposition = v },
			v)
	}
	if v := os.Getenv("DOJO_MODEL"); v != "" {
		noteEnvOverride(cfg,
			func(c *Config) string { return c.Defaults.Model },
			func(c *Config, v string) { c.Defaults.Model = v },
			v)
	}
	if v := os.Getenv("DOJO_USER_ID"); v != "" {
		noteEnvOverride(cfg,
			func(c *Config) string { return c.Auth.UserID },
			func(c *Config, v string) { c.Auth.UserID = v },
			v)
	}
	// DOJO_PROTOCOL_DISABLED: any non-empty value turns the protocol off. This
	// is the escape hatch that must work even when settings.json says enabled.
	if v := os.Getenv("DOJO_PROTOCOL_DISABLED"); v != "" {
		noteEnvOverride(cfg,
			func(c *Config) bool { return c.Protocol.Enabled },
			func(c *Config, v bool) { c.Protocol.Enabled = v },
			false)
	}
	if v := os.Getenv("DOJO_PROTOCOL_PATH"); v != "" {
		noteEnvOverride(cfg,
			func(c *Config) string { return c.Protocol.Path },
			func(c *Config, v string) { c.Protocol.Path = v },
			v)
	}
	// DOJO_VERIFY_AFTER_AGENT: any non-empty value opts into the post-agent
	// verify loop for this run. Tracked via noteEnvOverride (not a plain
	// assignment) so a one-off export never bakes into settings.json — same
	// transient-override contract as DOJO_PROTOCOL_DISABLED; see Save().
	if v := os.Getenv("DOJO_VERIFY_AFTER_AGENT"); v != "" {
		noteEnvOverride(cfg,
			func(c *Config) bool { return c.Verify.AfterAgent },
			func(c *Config, v bool) { c.Verify.AfterAgent = v },
			true)
	}
	// DOJO_PERMISSIONS_MODE / DOJO_DELEGATION_MODEL: same transient,
	// run-scoped override contract as the knobs above — tracked via
	// noteEnvOverride so an unrelated Save() (e.g. /model set) never bakes
	// either one into settings.json. Guardrails and Skills.ExternalDirs
	// deliberately have no env override (not requested).
	if v := os.Getenv("DOJO_PERMISSIONS_MODE"); v != "" {
		noteEnvOverride(cfg,
			func(c *Config) string { return c.Permissions.Mode },
			func(c *Config, v string) { c.Permissions.Mode = v },
			v)
	}
	if v := os.Getenv("DOJO_DELEGATION_MODEL"); v != "" {
		noteEnvOverride(cfg,
			func(c *Config) string { return c.Delegation.Model },
			func(c *Config, v string) { c.Delegation.Model = v },
			v)
	}

	// Gateway settings are load-bearing for connectivity — a malformed URL
	// or timeout is a genuine configuration error, so it still stops
	// startup (fixing it requires hand-editing settings.json regardless).
	if err := cfg.validateGateway(); err != nil {
		return nil, err
	}

	// A saved disposition must NEVER brick startup — see validateDisposition
	// for why Validate() alone isn't enough here. If it doesn't check out,
	// try the fuller merged set (builtins + config profiles + file-based
	// presets under ~/.dojo/dispositions/*.json — the same set /disposition
	// ls shows) before giving up; only degrade to the default if the name is
	// unknown there too. This closes the brick: setting a listed file-preset
	// name used to save fine via /disposition set, then fail right here on
	// the very next startup because Validate() only knows about builtins +
	// DispositionProfiles, not the file-based presets /disposition ls reads.
	if err := cfg.validateDisposition(); err != nil {
		if !IsKnownDisposition(cfg.Defaults.Disposition, cfg.DispositionProfiles) {
			fmt.Fprintf(os.Stderr, "dojo: warning: %s — falling back to default disposition %q\n", err, DefaultDisposition)
			cfg.Defaults.Disposition = DefaultDisposition
		}
	}

	return cfg, nil
}

// DefaultDisposition is the disposition Load() falls back to when the saved
// value can't be resolved against any known preset. Named so defaults() and
// the degrade path in Load() can't drift apart.
const DefaultDisposition = "balanced"

// validDispositions lists the allowed values for Defaults.Disposition.
var validDispositions = map[string]bool{
	"":            true,
	"focused":     true,
	"balanced":    true,
	"exploratory": true,
	"deliberate":  true,
}

// IsKnownDisposition reports whether name resolves to something real: a
// builtin, a config-resident profile in configProfiles, or a file-based
// preset under DispositionsDir(). This is the same merged set /disposition
// ls displays (LoadDispositionPresets + MergeConfigProfiles) — it is the
// single source of truth callers should use before trusting a disposition
// name. Validate() alone only knows about builtins + configProfiles and
// deliberately skips the disk read that checking file-based presets needs.
//
// A file-read error while checking presets is treated as "not known" so
// callers (Load, in particular) degrade safely instead of propagating an
// unrelated I/O error.
func IsKnownDisposition(name string, configProfiles map[string]DispositionPreset) bool {
	if validDispositions[name] {
		return true
	}
	if _, ok := configProfiles[name]; ok {
		return true
	}
	filePresets, err := LoadDispositionPresets()
	if err != nil {
		return false
	}
	for _, p := range filePresets {
		if p.Name == name {
			return true
		}
	}
	return false
}

// Validate checks that the config values are well-formed.
// It returns a descriptive error for the first invalid field found.
func (c *Config) Validate() error {
	if err := c.validateGateway(); err != nil {
		return err
	}
	if err := c.validateDisposition(); err != nil {
		return err
	}
	if err := c.validateProtocol(); err != nil {
		return err
	}
	if err := c.validateVerify(); err != nil {
		return err
	}
	if err := c.validatePermissions(); err != nil {
		return err
	}
	return nil
}

// validateProtocol is intentionally lenient: the protocol has no fail-hard
// conditions. A missing or unreadable override Path degrades to the embedded
// default at build-context time (see protocol.BuildSystemContext) rather than
// bricking startup — mirroring the disposition degrade philosophy. Kept as a
// hook so future hard constraints have a home without re-touching Validate.
func (c *Config) validateProtocol() error {
	return nil
}

// validateVerify has no fail-hard conditions: AfterAgent is a bool that is
// valid either way, and a missing block is a valid (disabled) config. Kept as a
// hook — symmetric with validateProtocol — so any future verify constraint has
// a home without re-touching Validate.
func (c *Config) validateVerify() error {
	return nil
}

// validateGateway checks that Gateway.URL and Gateway.Timeout are well-formed.
func (c *Config) validateGateway() error {
	// Gateway URL must be parseable.
	if c.Gateway.URL != "" {
		if _, err := url.ParseRequestURI(c.Gateway.URL); err != nil {
			return fmt.Errorf("invalid gateway.url %q: %w", c.Gateway.URL, err)
		}
	}

	// Gateway timeout must be a valid Go duration.
	if c.Gateway.Timeout != "" {
		if _, err := time.ParseDuration(c.Gateway.Timeout); err != nil {
			return fmt.Errorf("invalid gateway.timeout %q: %w", c.Gateway.Timeout, err)
		}
	}
	return nil
}

// validateDisposition checks that Defaults.Disposition is a builtin or a
// known config-resident custom profile (or empty). It does NOT check
// file-based presets under DispositionsDir() — that needs a disk read, which
// this function intentionally avoids so Validate() stays a pure, fast check
// that's safe to call from anywhere (including tests with no ~/.dojo on
// disk). Callers that need the fuller picture before giving up — namely
// Load(), which must never brick startup over a disposition name — should
// use IsKnownDisposition instead.
func (c *Config) validateDisposition() error {
	if !validDispositions[c.Defaults.Disposition] {
		if _, ok := c.DispositionProfiles[c.Defaults.Disposition]; !ok {
			return fmt.Errorf(
				"invalid defaults.disposition %q: must be one of focused, balanced, exploratory, deliberate, or a custom profile",
				c.Defaults.Disposition,
			)
		}
	}
	return nil
}

// validPermissionsModes lists the allowed values for Permissions.Mode. ""
// is valid (mirrors validDispositions) and means "unset" — Load()/defaults()
// normalize it to "default" before any real use.
var validPermissionsModes = map[string]bool{
	"":          true,
	"default":   true,
	"allowlist": true,
	"yolo":      true,
}

// validatePermissions checks that Permissions.Mode is one of the known
// gating strategies (or empty).
func (c *Config) validatePermissions() error {
	if !validPermissionsModes[c.Permissions.Mode] {
		return fmt.Errorf(
			"invalid permissions.mode %q: must be one of default, allowlist, yolo",
			c.Permissions.Mode,
		)
	}
	return nil
}

// EffectiveString returns a human-readable dump of the active config
// showing each setting as a key=value line.
func (c *Config) EffectiveString() string {
	var b strings.Builder
	fmt.Fprintf(&b, "gateway.url = %s\n", c.Gateway.URL)
	fmt.Fprintf(&b, "gateway.timeout = %s\n", c.Gateway.Timeout)
	if c.Gateway.Token != "" {
		fmt.Fprintf(&b, "gateway.token = %s\n", maskToken(c.Gateway.Token))
	} else {
		fmt.Fprintf(&b, "gateway.token = (not set)\n")
	}
	fmt.Fprintf(&b, "defaults.provider = %s\n", defaultIfEmpty(c.Defaults.Provider, "(not set)"))
	fmt.Fprintf(&b, "defaults.model = %s\n", defaultIfEmpty(c.Defaults.Model, "(not set)"))
	fmt.Fprintf(&b, "defaults.disposition = %s\n", defaultIfEmpty(c.Defaults.Disposition, "(not set)"))
	fmt.Fprintf(&b, "plugins.path = %s\n", c.Plugins.Path)
	fmt.Fprintf(&b, "protocol.enabled = %t\n", c.Protocol.Enabled)
	fmt.Fprintf(&b, "protocol.path = %s\n", defaultIfEmpty(c.Protocol.Path, "(embedded default)"))
	fmt.Fprintf(&b, "verify.after_agent = %t\n", c.Verify.AfterAgent)
	fmt.Fprintf(&b, "auth.user_id = %s\n", defaultIfEmpty(c.Auth.UserID, "(not set)"))
	return b.String()
}

func maskToken(t string) string {
	if len(t) <= 8 {
		return "****"
	}
	return t[:4] + "****" + t[len(t)-4:]
}

func defaultIfEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// SettingsPath returns the config file path (exported for /settings command).
func SettingsPath() string {
	return settingsPath()
}

// Save writes the current config to ~/.dojo/settings.json atomically.
//
// Fields Load() populated from an environment variable (DOJO_GATEWAY_URL,
// DOJO_PROTOCOL_DISABLED, ...) are transient by design — they override
// settings.json for the current run, not rewrite it. Without this guard, any
// such env var combined with any command that saves config (/model set,
// /disposition set) would silently and permanently bake the override into
// settings.json. This actually happened: a run with DOJO_PROTOCOL_DISABLED=1
// that also triggered a save wrote protocol.enabled:false to disk, disabling
// the protocol on every later run even with the env var unset.
//
// So for each field Load() env-overrode (tracked in c.envOverrides), Save()
// checks whether the field still holds the exact value the override wrote:
// if so, nothing has touched it since Load(), and the pre-override value
// (from the file, or the default) is written instead of the transient one.
// If the field has since been explicitly changed by application code (e.g.
// /model set mutating Defaults.Model directly), that's a real user edit and
// is kept as-is — this only strips values that are still purely env-sourced.
func (c *Config) Save() error {
	out := *c
	for _, ov := range c.envOverrides {
		if ov.get(c) == ov.envVal {
			ov.set(&out, ov.preEnv)
		}
	}

	data, err := json.MarshalIndent(&out, "", "  ")
	if err != nil {
		return err
	}
	return ioutilx.AtomicWriteFile(settingsPath(), data, 0600)
}

// DojoDir returns ~/.dojo
func DojoDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".dojo")
}

// MCPConfigPath returns ~/.dojo/mcp.json.
func MCPConfigPath() string {
	return filepath.Join(DojoDir(), "mcp.json")
}

// DispositionsDir returns ~/.dojo/dispositions/.
func DispositionsDir() string {
	return filepath.Join(DojoDir(), "dispositions")
}

func settingsPath() string {
	return filepath.Join(DojoDir(), "settings.json")
}

func defaults() *Config {
	home, _ := os.UserHomeDir()
	return &Config{
		Gateway: GatewayConfig{
			URL:     DefaultGatewayURL,
			Timeout: "60s",
		},
		Plugins: PluginsConfig{
			Path: filepath.Join(home, ".dojo", "plugins"),
		},
		Defaults: DefaultsConfig{
			Provider:    "",
			Disposition: DefaultDisposition,
			Model:       "",
		},
		// Protocol on by default. Pre-seeding true here is what makes the
		// "absent block / absent enabled key stays enabled" merge behavior work
		// (see ProtocolConfig doc) — a settings.json must say enabled:false, or
		// DOJO_PROTOCOL_DISABLED must be set, to turn it off.
		Protocol: ProtocolConfig{
			Enabled: true,
		},
		// Verify is opt-in and OFF by default: the zero value (AfterAgent:false)
		// is the intended default, so an omitted block keeps the post-agent
		// verify loop disabled. Stated explicitly for symmetry and to document
		// intent at the defaults site; DOJO_VERIFY_AFTER_AGENT flips it on per
		// run (see Load).
		Verify: VerifyConfig{
			AfterAgent: false,
		},
		// Permissions.Mode defaults to "default" — normal per-command prompt
		// rules apply until a settings.json or DOJO_PERMISSIONS_MODE (or
		// --yolo, in-memory only) says otherwise.
		Permissions: PermissionsConfig{
			Mode: "default",
		},
		// Delegation.Model defaults to "" (unset), stated explicitly for
		// symmetry with DefaultsConfig above.
		Delegation: DelegationConfig{
			Model: "",
		},
		// Guardrails on by default, same pre-seed-true trick as
		// Protocol.Enabled above (see GuardrailsConfig doc). WarnAfter/
		// HardAfter need pre-seeding too since their zero value (0) is not
		// the intended default.
		Guardrails: GuardrailsConfig{
			Enabled:   true,
			WarnAfter: 3,
			HardAfter: 5,
		},
		// Skills.ExternalDirs defaults to the conventional Claude Code
		// project skills directory.
		Skills: SkillsConfig{
			ExternalDirs: []string{".claude/skills"},
		},
	}
}
