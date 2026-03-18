package belayerconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_EmbeddedDefaults(t *testing.T) {
	cfg, err := Load("", "")
	require.NoError(t, err)
	assert.Equal(t, "claude", cfg.Agents.Provider)
	assert.Equal(t, "opus", cfg.Agents.LeadModel)
	assert.Equal(t, "sonnet", cfg.Agents.ReviewModel)
	assert.Equal(t, "dangerously-skip", cfg.Agents.Permissions)
	assert.Equal(t, 8, cfg.Execution.MaxLeads)
	assert.Equal(t, "5s", cfg.Execution.PollInterval)
	assert.Equal(t, "30m", cfg.Execution.StaleTimeout)
	assert.Equal(t, 3, cfg.Execution.MaxRetries)
	assert.True(t, cfg.Validation.Enabled)
	assert.True(t, cfg.Validation.AutoDetectProject)
	assert.Equal(t, "library", cfg.Validation.FallbackProfile)
	assert.Equal(t, "chrome-devtools", cfg.Validation.BrowserTool)
	assert.True(t, cfg.Anchor.Enabled)
	assert.Equal(t, 2, cfg.Anchor.MaxAttempts)
}

func TestLoadConfig_GlobalOverride(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "belayer.toml"), []byte("[execution]\nmax_leads = 4\n"), 0644)
	require.NoError(t, err)

	cfg, err := Load(dir, "")
	require.NoError(t, err)
	assert.Equal(t, 4, cfg.Execution.MaxLeads)
	assert.Equal(t, "claude", cfg.Agents.Provider) // still default
	assert.True(t, cfg.Validation.Enabled)          // still default
}

func TestLoadConfig_CragOverride(t *testing.T) {
	global := t.TempDir()
	crag := t.TempDir()

	err := os.WriteFile(filepath.Join(global, "belayer.toml"), []byte("[agents]\nprovider = \"codex\"\n"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(crag, "belayer.toml"), []byte("[agents]\nprovider = \"claude\"\n"), 0644)
	require.NoError(t, err)

	cfg, err := Load(global, crag)
	require.NoError(t, err)
	assert.Equal(t, "claude", cfg.Agents.Provider) // crag wins
}

func TestLoadConfig_CragOverridesGlobal(t *testing.T) {
	global := t.TempDir()
	crag := t.TempDir()

	err := os.WriteFile(filepath.Join(global, "belayer.toml"), []byte("[execution]\nmax_leads = 4\nmax_retries = 5\n"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(crag, "belayer.toml"), []byte("[execution]\nmax_leads = 2\n"), 0644)
	require.NoError(t, err)

	cfg, err := Load(global, crag)
	require.NoError(t, err)
	assert.Equal(t, 2, cfg.Execution.MaxLeads)   // crag wins
	assert.Equal(t, 5, cfg.Execution.MaxRetries)  // global override preserved
	assert.Equal(t, "5s", cfg.Execution.PollInterval) // embedded default preserved
}

func TestLoadConfig_MissingFileIgnored(t *testing.T) {
	cfg, err := Load("/nonexistent/path", "/also/nonexistent")
	require.NoError(t, err)
	assert.Equal(t, "claude", cfg.Agents.Provider) // falls back to embedded
}

func TestLoadProfile_EmbeddedDefault(t *testing.T) {
	profile, err := LoadProfile("", "", "frontend")
	require.NoError(t, err)
	assert.Contains(t, profile, "build")
}

func TestLoadProfile_Library(t *testing.T) {
	profile, err := LoadProfile("", "", "library")
	require.NoError(t, err)
	assert.Contains(t, profile, "build")
	assert.Contains(t, profile, "test_suite")
}

func TestLoadProfile_NotFound(t *testing.T) {
	_, err := LoadProfile("", "", "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestLoadConfig_EnvironmentDefaults(t *testing.T) {
	cfg, err := Load("", "")
	require.NoError(t, err)
	assert.Equal(t, "belayer", cfg.Environment.Command)
	assert.Equal(t, "env", cfg.Environment.Subcommand)
	assert.Equal(t, "", cfg.Environment.Snapshot)
}

func TestLoadConfig_TrackerAndReviewDefaults(t *testing.T) {
	cfg, err := Load("", "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Tracker.Provider != "github" {
		t.Errorf("Tracker.Provider = %q, want %q", cfg.Tracker.Provider, "github")
	}
	if cfg.Tracker.Label != "belayer" {
		t.Errorf("Tracker.Label = %q, want %q", cfg.Tracker.Label, "belayer")
	}
	if cfg.Review.CIFixAttempts != 2 {
		t.Errorf("Review.CIFixAttempts = %d, want 2", cfg.Review.CIFixAttempts)
	}
	if cfg.Review.PollInterval != "60s" {
		t.Errorf("Review.PollInterval = %q, want %q", cfg.Review.PollInterval, "60s")
	}
	if cfg.PR.StackThreshold != 1000 {
		t.Errorf("PR.StackThreshold = %d, want 1000", cfg.PR.StackThreshold)
	}
	if !cfg.PR.Draft {
		t.Error("PR.Draft should be true by default")
	}
}
