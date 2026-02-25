package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaults_StaleThreshold(t *testing.T) {
	cfg := defaults()
	if cfg.StaleThreshold != 15*time.Minute {
		t.Errorf("default StaleThreshold = %v, want 15m", cfg.StaleThreshold)
	}
}

func TestDefaults_DebugFalse(t *testing.T) {
	cfg := defaults()
	if cfg.Debug {
		t.Error("default Debug should be false")
	}
}

func TestApplyConfigValue_StaleThreshold(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected time.Duration
	}{
		{"valid minutes", "30", 30 * time.Minute},
		{"invalid string", "abc", 15 * time.Minute},
		{"zero", "0", 15 * time.Minute},
		{"negative", "-5", 15 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaults()
			applyConfigValue(cfg, "STALE_THRESHOLD", tt.value)
			if cfg.StaleThreshold != tt.expected {
				t.Errorf("StaleThreshold = %v, want %v", cfg.StaleThreshold, tt.expected)
			}
		})
	}
}

func TestApplyConfigValue_Debug(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		{"enabled with 1", "1", true},
		{"enabled with true", "true", true},
		{"disabled with 0", "0", false},
		{"disabled with false", "false", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := defaults()
			applyConfigValue(cfg, "DEBUG", tt.value)
			if cfg.Debug != tt.expected {
				t.Errorf("Debug = %v, want %v", cfg.Debug, tt.expected)
			}
		})
	}
}

func TestConfigFile_StaleThreshold(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config")
	if err := os.WriteFile(configPath, []byte("STALE_THRESHOLD=30\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := defaults()
	if err := loadFromFile(cfg, configPath); err != nil {
		t.Fatalf("loadFromFile failed: %v", err)
	}

	if cfg.StaleThreshold != 30*time.Minute {
		t.Errorf("StaleThreshold = %v, want 30m", cfg.StaleThreshold)
	}
}

func TestConfigFile_Debug(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config")
	if err := os.WriteFile(configPath, []byte("DEBUG=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := defaults()
	if err := loadFromFile(cfg, configPath); err != nil {
		t.Fatalf("loadFromFile failed: %v", err)
	}

	if !cfg.Debug {
		t.Error("expected Debug to be true")
	}
}

func TestEnvOverride_StaleThreshold(t *testing.T) {
	cfg := defaults()

	t.Setenv("CLAUDE_MATRIX_STALE_THRESHOLD", "45")
	applyEnvOverrides(cfg)

	if cfg.StaleThreshold != 45*time.Minute {
		t.Errorf("StaleThreshold = %v, want 45m", cfg.StaleThreshold)
	}
}

func TestEnvOverride_StaleThresholdInvalid(t *testing.T) {
	cfg := defaults()

	t.Setenv("CLAUDE_MATRIX_STALE_THRESHOLD", "abc")
	applyEnvOverrides(cfg)

	if cfg.StaleThreshold != 15*time.Minute {
		t.Errorf("StaleThreshold = %v, want 15m (default)", cfg.StaleThreshold)
	}
}

func TestEnvOverride_Debug(t *testing.T) {
	cfg := defaults()

	t.Setenv("CLAUDE_MATRIX_DEBUG", "1")
	applyEnvOverrides(cfg)

	if !cfg.Debug {
		t.Error("expected Debug to be true")
	}
}

func TestEnvOverride_BeatsConfigFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config")
	if err := os.WriteFile(configPath, []byte("STALE_THRESHOLD=30\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := defaults()
	if err := loadFromFile(cfg, configPath); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CLAUDE_MATRIX_STALE_THRESHOLD", "45")
	applyEnvOverrides(cfg)

	if cfg.StaleThreshold != 45*time.Minute {
		t.Errorf("StaleThreshold = %v, want 45m (env var should beat config)", cfg.StaleThreshold)
	}
}
