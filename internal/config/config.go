package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config is the dojo CLI configuration, loaded from ~/.dojo/settings.json.
type Config struct {
	Gateway  GatewayConfig  `json:"gateway"`
	Plugins  PluginsConfig  `json:"plugins"`
	Defaults DefaultsConfig `json:"defaults"`
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

	return cfg, nil
}

// SettingsPath returns the config file path (exported for /settings command).
func SettingsPath() string {
	return settingsPath()
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
			URL:     "http://localhost:7340",
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
