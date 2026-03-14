package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/donovan-yohan/belayer/internal/config"
	"github.com/donovan-yohan/belayer/internal/defaults"
	"github.com/donovan-yohan/belayer/internal/plugins"
	"github.com/spf13/cobra"
)

const githubRepo = "donovanyohan/belayer"

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize belayer configuration",
		Long:  "Creates the ~/.belayer/ directory with config.json, default prompts, validation profiles, and belayer.toml. Registers the belayer marketplace and installs required Claude Code plugins.",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := config.EnsureDir()
			if err != nil {
				return fmt.Errorf("creating belayer directory: %w", err)
			}

			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if err := config.Save(cfg); err != nil {
				return fmt.Errorf("saving config: %w", err)
			}

			// Write default config files
			configDir := filepath.Join(dir, "config")
			if err := defaults.WriteToDir(configDir); err != nil {
				return fmt.Errorf("writing default config: %w", err)
			}

			// Register belayer marketplace and install plugins (non-fatal)
			if err := registerPlugins(cmd); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"Warning: plugin registration failed: %v\n"+
						"  The 'harness' and 'pr' Claude Code plugins were not installed.\n"+
						"  Re-run 'belayer init' after resolving the issue, or install plugins manually.\n", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Initialized belayer at %s\n", dir)
			return nil
		},
	}
}

type pluginSpec struct {
	name    string
	version string
}

func registerPlugins(cmd *cobra.Command) error {
	claudeDir, err := claudeConfigDir()
	if err != nil {
		return err
	}
	reg := plugins.NewRegistry(claudeDir)

	if err := reg.RegisterMarketplace(githubRepo); err != nil {
		return fmt.Errorf("registering marketplace: %w", err)
	}

	specs := []pluginSpec{
		{"harness", plugins.HarnessVersion},
		{"pr", plugins.PRVersion},
	}

	for _, p := range specs {
		if err := reg.InstallPlugin(p.name, p.version); err != nil {
			return fmt.Errorf("installing %s: %w", p.name, err)
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Registered belayer marketplace. Installed plugins: harness, pr\n")
	return nil
}

// claudeConfigDir returns the Claude Code config directory.
// Respects CLAUDE_CONFIG_DIR env var for testing.
func claudeConfigDir() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine Claude config directory: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}
