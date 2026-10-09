package config

import (
	"strings"
	"testing"
)

func TestLoadDefaultsAreExplicitMockSettings(t *testing.T) {
	t.Setenv("MIGRATION_MODE", "")
	t.Setenv("CHECKPOINT_PROVIDER", "")
	t.Setenv("TRANSFER_PROVIDER", "")
	cfg := Load()
	if cfg.MigrationMode != "mock" || cfg.CheckpointProvider != "mock" || cfg.TransferProvider != "mock" {
		t.Fatalf("unexpected defaults: mode=%q checkpoint=%q transfer=%q", cfg.MigrationMode, cfg.CheckpointProvider, cfg.TransferProvider)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should validate: %v", err)
	}
}

func TestValidateRejectsUnsupportedAndUnsafeModes(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Config)
		want string
	}{
		{"statefulset", func(c *Config) { c.MigrationMode = "statefulset" }, "unsupported"},
		{"unknown provider", func(c *Config) { c.CheckpointProvider = "file" }, "unsupported CHECKPOINT_PROVIDER"},
		{"mock mode with real provider", func(c *Config) { c.CheckpointProvider = "kubelet" }, "requires CHECKPOINT_PROVIDER=mock"},
		{"invalid timeout", func(c *Config) { c.TransferTimeout = 0 }, "timeouts must be positive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Load()
			tt.edit(cfg)
			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}
