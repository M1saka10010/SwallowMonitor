package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigDefaultsRetentionDays(t *testing.T) {
	cfg, err := loadConfig(writeConfig(t, "listen: \":9999\"\n"))
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.RetentionDays == nil || *cfg.RetentionDays != 7 {
		t.Fatalf("default RetentionDays = %v, want 7", cfg.RetentionDays)
	}
}

func TestLoadConfigExplicitZeroKeepsNoPrune(t *testing.T) {
	cfg, err := loadConfig(writeConfig(t, "retentionDays: 0\n"))
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if cfg.RetentionDays == nil || *cfg.RetentionDays != 0 {
		t.Fatalf("explicit zero RetentionDays = %v, want 0", cfg.RetentionDays)
	}
}
