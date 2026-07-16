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
	Auth                AuthConfig                   `json:"auth,omitempty"`
	DispositionProfiles map[string]DispositionPreset `json:"disposition_profiles,omitempty"`
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

	// Environment overrides
	if v := os.Getenv("DOJO_GATEWAY_URL"); v != "" {
		cfg.Gateway.URL = v
	}
	if v := os.Getenv("DOJO_GATEWAY_TOKEN"); v != "" {
		cfg.Gateway.Token = v
	}
	if v := os.Getenv("DOJO_PLUGINS_PATH"); v != "" {
		cfg.Plugins.Path = v
	}
	if v := os.Getenv("DOJO_PROVIDER"); v != "" {
		cfg.Defaults.Provider = v
	}
	if v := os.Getenv("DOJO_DISPOSITION"); v != "" {
		cfg.Defaults.Disposition = v
	}
	if v := os.Getenv("DOJO_MODEL"); v != "" {
		cfg.Defaults.Model = v
	}
	if v := os.Getenv("DOJO_USER_ID"); v != "" {
		cfg.Auth.UserID = v
	}
	// DOJO_PROTOCOL_DISABLED: any non-empty value turns the protocol off. This
	// is the escape hatch that must work even when settings.json says enabled.
	if v := os.Getenv("DOJO_PROTOCOL_DISABLED"); v != "" {
		cfg.Protocol.Enabled = false
	}
	if v := os.Getenv("DOJO_PROTOCOL_PATH"); v != "" {
		cfg.Protocol.Path = v
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
func (c *Config) Save() error {
	data, err := json.MarshalIndent(c, "", "  ")
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
	}
}
