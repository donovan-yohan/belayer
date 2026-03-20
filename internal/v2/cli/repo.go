package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/donovan-yohan/belayer/internal/v2/config"
)

func newRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Manage the global repo registry",
	}

	cmd.AddCommand(newRepoAddCmd(), newRepoListCmd(), newRepoRemoveCmd())
	return cmd
}

func newRepoAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <name> [path]",
		Short: "Register a repo (auto-detects path if omitted)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			var repoPath string
			if len(args) == 2 {
				repoPath = args[1]
			} else {
				detected, err := config.DetectRepoPath(name)
				if err != nil {
					return err
				}
				fmt.Printf("Auto-detected: %s\n", detected)
				repoPath = detected
			}

			if err := cfg.AddRepo(name, repoPath); err != nil {
				return err
			}
			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("Registered repo %q at %s\n", name, repoPath)
			return nil
		},
	}
}

func newRepoListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all registered repos",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if len(cfg.Repos) == 0 {
				fmt.Println("No repos registered.")
				fmt.Println("Add one with: belayer repo add <name> <path>")
				return nil
			}
			for name, entry := range cfg.Repos {
				fmt.Printf("  %-20s %s\n", name, entry.Path)
			}
			return nil
		},
	}
}

func newRepoRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a repo from the registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := cfg.RemoveRepo(args[0]); err != nil {
				return err
			}
			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("Removed repo %q\n", args[0])
			return nil
		},
	}
}
