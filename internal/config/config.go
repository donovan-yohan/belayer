package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	dirName    = ".belayer"
	configFile = "config.json"
)

// Config represents the global belayer configuration.
type Config struct {
	DefaultInstance string            `json:"default_instance"`
	Instances       map[string]string `json:"instances"` // name -> path
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Instances: make(map[string]string),
	}
}

// Dir returns the path to the belayer config directory (~/.belayer/).
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, dirName), nil
}

// Path returns the full path to config.json.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFile), nil
}

// EnsureDir creates the ~/.belayer/ directory if it doesn't exist.
func EnsureDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating config directory: %w", err)
	}
	return dir, nil
}

// Load reads the config from disk. Returns DefaultConfig if the file doesn't exist.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}

// Save writes the config to disk, creating the directory if needed.
func Save(cfg *Config) error {
	if _, err := EnsureDir(); err != nil {
		return err
	}

	p, err := Path()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	if err := os.WriteFile(p, data, 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}
