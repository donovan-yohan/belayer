package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/donovan-yohan/belayer/frameworks"
	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	var frameworkFlag string
	var force bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Scaffold a belayer framework into the current repo",
		Long: `Install a belayer framework into .belayer/ of the current repository.

Frameworks provide pipeline.yaml and node runner scripts that define
how belayer executes pipeline nodes.

Examples:
  belayer setup --framework claude-tmux           # built-in framework
  belayer setup --framework ./my-custom-framework # local path`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if frameworkFlag == "" {
				return fmt.Errorf("--framework is required")
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}

			targetDir := filepath.Join(cwd, ".belayer")
			if err := os.MkdirAll(targetDir, 0o755); err != nil {
				return fmt.Errorf("create .belayer directory: %w", err)
			}

			if err := frameworks.Install(frameworkFlag, targetDir, force); err != nil {
				return err
			}

			if err := frameworks.EnsureInternalDir(cwd); err != nil {
				return fmt.Errorf("create .internal directory: %w", err)
			}

			fmt.Printf("Framework %q installed to .belayer/\n", frameworkFlag)
			fmt.Println("Customize .belayer/pipeline.yaml, then run: belayer climb")
			return nil
		},
	}

	cmd.Flags().StringVar(&frameworkFlag, "framework", "", "Framework name (built-in) or path (local directory)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing .belayer/pipeline.yaml")

	return cmd
}
