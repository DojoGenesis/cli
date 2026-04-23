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
	Gateway             GatewayConfig                 `json:"gateway"`
	Plugins             PluginsConfig                 `json:"plugins"`
	Defaults            DefaultsConfig                `json:"defaults"`
	Auth                AuthConfig                    `json:"auth,omitempty"`
	DispositionProfiles map[string]DispositionPreset  `json:"disposition_profiles,omitempty"`
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

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validDispositions lists the allowed values for Defaults.Disposition.
var validDispositions = map[string]bool{
	"":            true,
	"focused":     true,
	"balanced":    true,
	"exploratory": true,
	"deliberate":  true,
}

// Validate checks that the config values are well-formed.
// It returns a descriptive error for the first invalid field found.
func (c *Config) Validate() error {
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

	// Disposition must be a builtin OR a known custom profile (or empty).
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
			Disposition: "balanced",
			Model:       "",
		},
	}
}
