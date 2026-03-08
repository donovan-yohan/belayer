package cli

import (
	"fmt"
	"path/filepath"

	"github.com/donovan-yohan/belayer/internal/config"
	"github.com/donovan-yohan/belayer/internal/defaults"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize belayer configuration",
		Long:  "Creates the ~/.belayer/ directory and default config.json if they don't exist.",
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

			fmt.Fprintf(cmd.OutOrStdout(), "Initialized belayer at %s\n", dir)
			return nil
		},
	}
}
