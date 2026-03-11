package belayerconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/donovan-yohan/belayer/internal/defaults"
)

// Config holds the full belayer configuration.
type Config struct {
	Agents     AgentsConfig     `toml:"agents"`
	Execution  ExecutionConfig  `toml:"execution"`
	Validation ValidationConfig `toml:"validation"`
	Anchor     AnchorConfig     `toml:"anchor"`
}

// AgentsConfig configures the AI agent provider and models.
type AgentsConfig struct {
	Provider    string `toml:"provider"`
	LeadModel   string `toml:"lead_model"`
	ReviewModel string `toml:"review_model"`
	Permissions string `toml:"permissions"`
}

// ExecutionConfig controls lead execution parameters.
type ExecutionConfig struct {
	MaxLeads     int    `toml:"max_leads"`
	PollInterval string `toml:"poll_interval"`
	StaleTimeout string `toml:"stale_timeout"`
	MaxRetries   int    `toml:"max_retries"`
}

// ValidationConfig controls post-execution validation.
type ValidationConfig struct {
	Enabled           bool   `toml:"enabled"`
	AutoDetectProject bool   `toml:"auto_detect_project"`
	FallbackProfile   string `toml:"fallback_profile"`
	BrowserTool       string `toml:"browser_tool"`
}

// AnchorConfig controls the anchor (validation) loop.
type AnchorConfig struct {
	Enabled     bool `toml:"enabled"`
	MaxAttempts int  `toml:"max_attempts"`
}

// Load resolves configuration with precedence: crag > global > embedded defaults.
func Load(globalDir, cragDir string) (*Config, error) {
	var cfg Config

	// 1. Start with embedded defaults.
	embeddedData, err := defaults.FS.ReadFile("belayer.toml")
	if err != nil {
		return nil, fmt.Errorf("reading embedded defaults: %w", err)
	}
	if _, err := toml.Decode(string(embeddedData), &cfg); err != nil {
		return nil, fmt.Errorf("decoding embedded defaults: %w", err)
	}

	// 2. Overlay global config if present.
	if globalDir != "" {
		if err := overlayFile(filepath.Join(globalDir, "belayer.toml"), &cfg); err != nil {
			return nil, fmt.Errorf("loading global config: %w", err)
		}
	}

	// 3. Overlay crag config if present.
	if cragDir != "" {
		if err := overlayFile(filepath.Join(cragDir, "belayer.toml"), &cfg); err != nil {
			return nil, fmt.Errorf("loading crag config: %w", err)
		}
	}

	return &cfg, nil
}

// overlayFile reads a TOML file and decodes it into cfg, overwriting any fields present in the file.
func overlayFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if _, err := toml.Decode(string(data), cfg); err != nil {
		return fmt.Errorf("decoding %s: %w", path, err)
	}
	return nil
}

// LoadProfile reads a validation profile with resolution: crag > global > embedded.
func LoadProfile(globalDir, cragDir, name string) (string, error) {
	return resolveFile(globalDir, cragDir, "profiles", name+".toml")
}

// resolveFile checks crag, then global, then embedded FS for a file.
func resolveFile(globalDir, cragDir, subdir, filename string) (string, error) {
	rel := filepath.Join(subdir, filename)

	// 1. Crag directory.
	if cragDir != "" {
		data, err := os.ReadFile(filepath.Join(cragDir, rel))
		if err == nil {
			return string(data), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
	}

	// 2. Global directory.
	if globalDir != "" {
		data, err := os.ReadFile(filepath.Join(globalDir, rel))
		if err == nil {
			return string(data), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
	}

	// 3. Embedded defaults.
	data, err := defaults.FS.ReadFile(rel)
	if err != nil {
		return "", fmt.Errorf("file %s not found in any config layer: %w", rel, err)
	}
	return string(data), nil
}
