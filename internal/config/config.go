// Package config loads the CLI configuration (Immich server origin and API key).
package config

import (
	"fmt"
	"net/url"

	"github.com/spf13/viper"
)

// Config holds the settings required to talk to an Immich server.
type Config struct {
	// Server is the origin of the Immich instance (e.g. https://immich.example.com/).
	// It must NOT include the /api base path; the client appends it.
	Server string
	// APIKey is sent as the x-api-key header on every request.
	APIKey string
}

// Load reads the YAML config file at path. The environment variables
// IMMICH_SERVER and IMMICH_API_KEY override the file values.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.BindEnv("server", "IMMICH_SERVER"); err != nil {
		return nil, fmt.Errorf("binding IMMICH_SERVER: %w", err)
	}
	if err := v.BindEnv("api_key", "IMMICH_API_KEY"); err != nil {
		return nil, fmt.Errorf("binding IMMICH_API_KEY: %w", err)
	}
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	cfg := &Config{
		Server: v.GetString("server"),
		APIKey: v.GetString("api_key"),
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config %q: %w", path, err)
	}
	return cfg, nil
}

func (c *Config) validate() error {
	if c.Server == "" {
		return fmt.Errorf("server is required")
	}
	u, err := url.Parse(c.Server)
	if err != nil {
		return fmt.Errorf("server is not a valid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("server must be an http(s) URL, got %q", c.Server)
	}
	if c.APIKey == "" {
		return fmt.Errorf("api_key is required")
	}
	return nil
}
