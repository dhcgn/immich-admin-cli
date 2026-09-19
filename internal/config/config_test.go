package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadWithoutClipProbe(t *testing.T) {
	p := writeConfig(t, "server: https://immich.example.com/\napi_key: k\n")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ClipProbe.Server != "" || cfg.ClipProbe.Token != "" {
		t.Errorf("expected empty clip_probe, got %+v", cfg.ClipProbe)
	}
	if err := cfg.ValidateClipProbe(); err == nil {
		t.Error("expected ValidateClipProbe to fail without clip_probe")
	} else if !strings.Contains(err.Error(), ClipProbeRepoURL) {
		t.Errorf("missing-config error must link the repo, got: %v", err)
	}
}

func TestLoadWithClipProbeAndEnvOverride(t *testing.T) {
	p := writeConfig(t, "server: https://immich.example.com/\napi_key: k\nclip_probe:\n  server: https://clip-probe.example.com/\n  token: file-token\n")
	t.Setenv("IMMICH_CLIP_PROBE_TOKEN", "env-token")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ClipProbe.Server != "https://clip-probe.example.com/" || cfg.ClipProbe.Token != "env-token" {
		t.Errorf("unexpected clip_probe: %+v", cfg.ClipProbe)
	}
	if err := cfg.ValidateClipProbe(); err != nil {
		t.Errorf("ValidateClipProbe: %v", err)
	}
}

func TestValidateClipProbeBadURL(t *testing.T) {
	cfg := &Config{Server: "https://immich.example.com/", APIKey: "k", ClipProbe: ClipProbe{Server: "ftp://x", Token: "t"}}
	if err := cfg.ValidateClipProbe(); err == nil {
		t.Error("expected error for non-http(s) clip_probe.server")
	} else if !strings.Contains(err.Error(), ClipProbeRepoURL) {
		t.Errorf("bad-URL error must link the repo, got: %v", err)
	}
}
