package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInitCmd_RegistersPlugins(t *testing.T) {
	// Set up temp dirs for claude config
	claudeDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", claudeDir)

	// Redirect belayer home to temp dir
	belayerHome := t.TempDir()
	t.Setenv("HOME", belayerHome)

	cmd := newInitCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Verify marketplace was registered
	mpData, err := os.ReadFile(filepath.Join(claudeDir, "plugins", "known_marketplaces.json"))
	if err != nil {
		t.Fatalf("marketplace file not created: %v", err)
	}
	var mp map[string]json.RawMessage
	if err := json.Unmarshal(mpData, &mp); err != nil {
		t.Fatalf("parsing marketplaces: %v", err)
	}
	if _, ok := mp["belayer"]; !ok {
		t.Error("belayer marketplace not registered")
	}

	// Verify plugins were installed
	ipData, err := os.ReadFile(filepath.Join(claudeDir, "plugins", "installed_plugins.json"))
	if err != nil {
		t.Fatalf("installed plugins file not created: %v", err)
	}
	var ip map[string]json.RawMessage
	if err := json.Unmarshal(ipData, &ip); err != nil {
		t.Fatalf("parsing installed plugins: %v", err)
	}
	if _, ok := ip["harness@belayer"]; !ok {
		t.Error("harness plugin not installed")
	}
	if _, ok := ip["pr@belayer"]; !ok {
		t.Error("pr plugin not installed")
	}
}
