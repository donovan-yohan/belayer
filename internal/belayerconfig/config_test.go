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

func TestLoadConfig_InstanceOverride(t *testing.T) {
	global := t.TempDir()
	instance := t.TempDir()

	err := os.WriteFile(filepath.Join(global, "belayer.toml"), []byte("[agents]\nprovider = \"codex\"\n"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(instance, "belayer.toml"), []byte("[agents]\nprovider = \"claude\"\n"), 0644)
	require.NoError(t, err)

	cfg, err := Load(global, instance)
	require.NoError(t, err)
	assert.Equal(t, "claude", cfg.Agents.Provider) // instance wins
}

func TestLoadConfig_InstanceOverridesGlobal(t *testing.T) {
	global := t.TempDir()
	instance := t.TempDir()

	err := os.WriteFile(filepath.Join(global, "belayer.toml"), []byte("[execution]\nmax_leads = 4\nmax_retries = 5\n"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(instance, "belayer.toml"), []byte("[execution]\nmax_leads = 2\n"), 0644)
	require.NoError(t, err)

	cfg, err := Load(global, instance)
	require.NoError(t, err)
	assert.Equal(t, 2, cfg.Execution.MaxLeads)   // instance wins
	assert.Equal(t, 5, cfg.Execution.MaxRetries)  // global override preserved
	assert.Equal(t, "5s", cfg.Execution.PollInterval) // embedded default preserved
}

func TestLoadConfig_MissingFileIgnored(t *testing.T) {
	cfg, err := Load("/nonexistent/path", "/also/nonexistent")
	require.NoError(t, err)
	assert.Equal(t, "claude", cfg.Agents.Provider) // falls back to embedded
}

func TestLoadPrompt_EmbeddedDefault(t *testing.T) {
	prompt, err := LoadPrompt("", "", "lead")
	require.NoError(t, err)
	assert.Contains(t, prompt, "{{.Spec}}")
	assert.Contains(t, prompt, "{{.GoalID}}")
}

func TestLoadPrompt_InstanceOverride(t *testing.T) {
	instance := t.TempDir()
	promptDir := filepath.Join(instance, "prompts")
	require.NoError(t, os.MkdirAll(promptDir, 0755))

	custom := "Custom prompt for {{.GoalID}}"
	require.NoError(t, os.WriteFile(filepath.Join(promptDir, "lead.md"), []byte(custom), 0644))

	prompt, err := LoadPrompt("", instance, "lead")
	require.NoError(t, err)
	assert.Equal(t, custom, prompt)
}

func TestLoadPrompt_GlobalOverride(t *testing.T) {
	global := t.TempDir()
	promptDir := filepath.Join(global, "prompts")
	require.NoError(t, os.MkdirAll(promptDir, 0755))

	custom := "Global custom prompt"
	require.NoError(t, os.WriteFile(filepath.Join(promptDir, "lead.md"), []byte(custom), 0644))

	prompt, err := LoadPrompt(global, "", "lead")
	require.NoError(t, err)
	assert.Equal(t, custom, prompt)
}

func TestLoadPrompt_InstanceWinsOverGlobal(t *testing.T) {
	global := t.TempDir()
	instance := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(global, "prompts"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(instance, "prompts"), 0755))

	require.NoError(t, os.WriteFile(filepath.Join(global, "prompts", "lead.md"), []byte("global"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(instance, "prompts", "lead.md"), []byte("instance"), 0644))

	prompt, err := LoadPrompt(global, instance, "lead")
	require.NoError(t, err)
	assert.Equal(t, "instance", prompt)
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

func TestLoadPrompt_NotFound(t *testing.T) {
	_, err := LoadPrompt("", "", "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestLoadProfile_NotFound(t *testing.T) {
	_, err := LoadProfile("", "", "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
