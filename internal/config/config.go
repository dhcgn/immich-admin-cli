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
	// Tools holds paths to optional external binaries used by client
	// workflows for local processing (e.g. ImageMagick for --resize).
	Tools Tools
	// ClipProbe holds the connection settings for the external
	// immich-clip-probe service used by `client-workflow find-similar`.
	// Optional globally — only find-similar requires it.
	ClipProbe ClipProbe
}

// ClipProbe holds the connection settings for the external visual-similarity
// service (https://github.com/dhcgn/immich-clip-probe/), which runs next to
// Immich. Server is the service origin (e.g. https://clip-probe.example.com/);
// the client appends /v1/similar. Token is the service's own API_TOKEN, not
// an Immich API key.
type ClipProbe struct {
	Server string
	Token  string
}

// Tools holds explicit paths to external executables used by client
// workflows. Any field left empty falls back to a PATH lookup at the point
// of use (see e.g. workflows.ResolveImageMagickPath) — nothing here is
// required unless the corresponding feature is actually used.
type Tools struct {
	// ImageMagickPath is the path to the ImageMagick executable ("magick"
	// for v7+, or "convert" for legacy v6) used by
	// `client-workflow download-album --resize`.
	ImageMagickPath string
	// FFmpegPath is the path to the ffmpeg executable used by
	// `client-workflow download-album --resize-video-preset`.
	FFmpegPath string
}

// Load reads the YAML config file at path. The environment variables
// IMMICH_SERVER, IMMICH_API_KEY, IMMICH_IMAGEMAGICK_PATH,
// IMMICH_FFMPEG_PATH, IMMICH_CLIP_PROBE_SERVER, and IMMICH_CLIP_PROBE_TOKEN
// override the file values.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.BindEnv("server", "IMMICH_SERVER"); err != nil {
		return nil, fmt.Errorf("binding IMMICH_SERVER: %w", err)
	}
	if err := v.BindEnv("api_key", "IMMICH_API_KEY"); err != nil {
		return nil, fmt.Errorf("binding IMMICH_API_KEY: %w", err)
	}
	if err := v.BindEnv("tools.imagemagick_path", "IMMICH_IMAGEMAGICK_PATH"); err != nil {
		return nil, fmt.Errorf("binding IMMICH_IMAGEMAGICK_PATH: %w", err)
	}
	if err := v.BindEnv("tools.ffmpeg_path", "IMMICH_FFMPEG_PATH"); err != nil {
		return nil, fmt.Errorf("binding IMMICH_FFMPEG_PATH: %w", err)
	}
	if err := v.BindEnv("clip_probe.server", "IMMICH_CLIP_PROBE_SERVER"); err != nil {
		return nil, fmt.Errorf("binding IMMICH_CLIP_PROBE_SERVER: %w", err)
	}
	if err := v.BindEnv("clip_probe.token", "IMMICH_CLIP_PROBE_TOKEN"); err != nil {
		return nil, fmt.Errorf("binding IMMICH_CLIP_PROBE_TOKEN: %w", err)
	}
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("reading config file %q: %w", path, err)
	}

	cfg := &Config{
		Server: v.GetString("server"),
		APIKey: v.GetString("api_key"),
		Tools: Tools{
			ImageMagickPath: v.GetString("tools.imagemagick_path"),
			FFmpegPath:      v.GetString("tools.ffmpeg_path"),
		},
		ClipProbe: ClipProbe{
			Server: v.GetString("clip_probe.server"),
			Token:  v.GetString("clip_probe.token"),
		},
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

// ClipProbeRepoURL is the setup reference for the external visual-similarity
// service. This is the first command that depends on a third-party service
// running next to Immich, so the URL is surfaced in help and errors.
const ClipProbeRepoURL = "https://github.com/dhcgn/immich-clip-probe/"

// ValidateClipProbe reports whether the clip-probe settings are usable by
// `client-workflow find-similar`. It is opt-in (not part of validate) so
// existing configs without a clip_probe section keep working.
func (c *Config) ValidateClipProbe() error {
	if c.ClipProbe.Server == "" || c.ClipProbe.Token == "" {
		return fmt.Errorf("clip-probe not configured: set clip_probe.server + clip_probe.token in config (or IMMICH_CLIP_PROBE_SERVER / IMMICH_CLIP_PROBE_TOKEN env). Setup: %s", ClipProbeRepoURL)
	}
	u, err := url.Parse(c.ClipProbe.Server)
	if err != nil {
		return fmt.Errorf("clip_probe.server is not a valid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("clip_probe.server must be an http(s) URL, got %q. Setup: %s", c.ClipProbe.Server, ClipProbeRepoURL)
	}
	return nil
}
