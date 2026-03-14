package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterMarketplace_WritesNewEntry(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir)

	err := r.RegisterMarketplace("donovanyohan/belayer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "plugins", "known_marketplaces.json"))
	if err != nil {
		t.Fatalf("reading marketplaces file: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parsing JSON: %v", err)
	}
	if _, ok := raw["belayer"]; !ok {
		t.Error("expected 'belayer' key in marketplaces")
	}
}

func TestRegisterMarketplace_IdempotentSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir)

	_ = r.RegisterMarketplace("donovanyohan/belayer")
	err := r.RegisterMarketplace("donovanyohan/belayer")
	if err != nil {
		t.Fatalf("second call should not error: %v", err)
	}
}

func TestInstallPlugin_WritesEntry(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir)

	err := r.InstallPlugin("harness", "2.1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "plugins", "installed_plugins.json"))
	if err != nil {
		t.Fatalf("reading installed file: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("parsing JSON: %v", err)
	}
	if _, ok := raw["harness@belayer"]; !ok {
		t.Error("expected 'harness@belayer' key")
	}
}

func TestInstallPlugin_IdempotentSkipsExisting(t *testing.T) {
	dir := t.TempDir()
	r := NewRegistry(dir)

	_ = r.InstallPlugin("harness", "2.1.0")
	err := r.InstallPlugin("harness", "2.1.0")
	if err != nil {
		t.Fatalf("second call should not error: %v", err)
	}
}

func TestRegisterMarketplace_CorruptJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	os.MkdirAll(pluginsDir, 0755)
	os.WriteFile(filepath.Join(pluginsDir, "known_marketplaces.json"), []byte("{not valid json"), 0644)

	r := NewRegistry(dir)
	err := r.RegisterMarketplace("donovanyohan/belayer")
	if err == nil {
		t.Fatal("expected error for corrupt JSON, got nil")
	}
}

func TestInstallPlugin_MergesWithExistingFile(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	os.MkdirAll(pluginsDir, 0755)

	existing := `{"other-plugin@other":[{"scope":"user","version":"1.0.0"}]}`
	os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte(existing), 0644)

	r := NewRegistry(dir)
	err := r.InstallPlugin("harness", "2.1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(pluginsDir, "installed_plugins.json"))
	var raw map[string]json.RawMessage
	json.Unmarshal(data, &raw)

	if _, ok := raw["harness@belayer"]; !ok {
		t.Error("expected 'harness@belayer' key")
	}
	if _, ok := raw["other-plugin@other"]; !ok {
		t.Error("existing plugin should be preserved")
	}
}

func TestRegisterMarketplace_MergesWithExistingFile(t *testing.T) {
	dir := t.TempDir()
	pluginsDir := filepath.Join(dir, "plugins")
	os.MkdirAll(pluginsDir, 0755)

	// Pre-existing marketplace
	existing := `{"other-marketplace":{"source":{"source":"github","repo":"someone/other"}}}`
	os.WriteFile(filepath.Join(pluginsDir, "known_marketplaces.json"), []byte(existing), 0644)

	r := NewRegistry(dir)
	err := r.RegisterMarketplace("donovanyohan/belayer")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(pluginsDir, "known_marketplaces.json"))
	var raw map[string]json.RawMessage
	json.Unmarshal(data, &raw)

	if _, ok := raw["belayer"]; !ok {
		t.Error("expected 'belayer' key")
	}
	if _, ok := raw["other-marketplace"]; !ok {
		t.Error("existing marketplace should be preserved")
	}
}
