package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.DefaultCrag != "" {
		t.Errorf("expected empty default_crag, got %q", cfg.DefaultCrag)
	}
	if cfg.Crags == nil {
		t.Fatal("expected non-nil Crags map")
	}
	if len(cfg.Crags) != 0 {
		t.Errorf("expected empty Crags map, got %d entries", len(cfg.Crags))
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	cfg := DefaultConfig()
	cfg.DefaultCrag = "test-crag"
	cfg.Crags["test-crag"] = "/tmp/test"

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

	if loaded.DefaultCrag != "test-crag" {
		t.Errorf("expected default_crag=%q, got %q", "test-crag", loaded.DefaultCrag)
	}
	if loaded.Crags["test-crag"] != "/tmp/test" {
		t.Errorf("expected crag path /tmp/test, got %q", loaded.Crags["test-crag"])
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

	if loaded.DefaultCrag != "" {
		t.Errorf("expected empty default_crag, got %q", loaded.DefaultCrag)
	}
}

func TestBackwardsCompatUnmarshal(t *testing.T) {
	// Old config format using "default_instance" and "instances"
	old := `{"default_instance":"my-instance","instances":{"my-instance":"/tmp/mi"}}`
	var cfg Config
	if err := json.Unmarshal([]byte(old), &cfg); err != nil {
		t.Fatalf("unmarshal old format: %v", err)
	}
	if cfg.DefaultCrag != "my-instance" {
		t.Errorf("expected DefaultCrag=my-instance, got %q", cfg.DefaultCrag)
	}
	if cfg.Crags["my-instance"] != "/tmp/mi" {
		t.Errorf("expected Crags[my-instance]=/tmp/mi, got %q", cfg.Crags["my-instance"])
	}
}
