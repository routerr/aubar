package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	cfg := DefaultSettings()
	if err := cfg.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Refresh.GlobalIntervalSeconds != cfg.Refresh.GlobalIntervalSeconds {
		t.Fatalf("interval mismatch")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("settings file missing: %v", err)
	}
}

func TestValidateRejectsInvalid(t *testing.T) {
	cfg := DefaultSettings()
	cfg.Refresh.GlobalIntervalSeconds = 1
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation error")
	}
}
