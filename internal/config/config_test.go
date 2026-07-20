package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatal(err)
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadFillsDefaults(t *testing.T) {
	path := writeTemp(t, `{"listen": "127.0.0.1:9999"}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:9999" {
		t.Errorf("Listen = %q", cfg.Listen)
	}
	if cfg.DefaultRoute != "socks" || len(cfg.Rules) == 0 || len(cfg.Upstreams) == 0 {
		t.Errorf("defaults not applied: %+v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := writeTemp(t, `{"listne": "typo"}`)
	if _, err := Load(path); err == nil {
		t.Error("expected error for unknown field")
	}
}

func TestValidateErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"unknown rule type", func(c *Config) { c.Rules[0].Type = "geoip" }},
		{"unknown rule route", func(c *Config) { c.Rules[0].Route = "nope" }},
		{"unknown default route", func(c *Config) { c.DefaultRoute = "nope" }},
		{"reserved upstream name", func(c *Config) { c.Upstreams["direct"] = Upstream{Type: "direct"} }},
		{"socks5 without address", func(c *Config) { c.Upstreams["socks"] = Upstream{Type: "socks5"} }},
		{"bad dial timeout", func(c *Config) { c.DialTimeout = "soon" }},
		{"unknown upstream type", func(c *Config) { c.Upstreams["socks"] = Upstream{Type: "http", Address: "x"} }},
	}
	for _, tt := range tests {
		cfg := Default()
		tt.mutate(cfg)
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: expected validation error", tt.name)
		}
	}
}
