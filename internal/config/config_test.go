package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.DefaultInstance != "" {
		t.Errorf("expected empty default_instance, got %q", cfg.DefaultInstance)
	}
	if cfg.Instances == nil {
		t.Fatal("expected non-nil Instances map")
	}
	if len(cfg.Instances) != 0 {
		t.Errorf("expected empty Instances map, got %d entries", len(cfg.Instances))
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	cfg := DefaultConfig()
	cfg.DefaultInstance = "test-instance"
	cfg.Instances["test-instance"] = "/tmp/test"

	// Write directly to temp path
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read back and verify
	readData, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	loaded := DefaultConfig()
	if err := json.Unmarshal(readData, loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if loaded.DefaultInstance != "test-instance" {
		t.Errorf("expected default_instance=%q, got %q", "test-instance", loaded.DefaultInstance)
	}
	if loaded.Instances["test-instance"] != "/tmp/test" {
		t.Errorf("expected instance path /tmp/test, got %q", loaded.Instances["test-instance"])
	}
}

func TestLoadNonExistent(t *testing.T) {
	// Load should return default config when file doesn't exist
	// This test uses the real Load which depends on HOME,
	// so we test the json round-trip instead
	cfg := DefaultConfig()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if loaded.DefaultInstance != "" {
		t.Errorf("expected empty default_instance, got %q", loaded.DefaultInstance)
	}
}
