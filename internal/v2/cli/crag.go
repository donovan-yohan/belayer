package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/donovan-yohan/belayer/internal/v2/config"
)

func newCragCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "crag",
		Short: "Manage named crags (pipeline directories)",
	}
	cmd.AddCommand(newCragInitCmd(), newCragListCmd())
	return cmd
}

func newCragInitCmd() *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Register the current directory as a named crag",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			if name == "" {
				name = filepath.Base(cwd)
			}
			if err := cfg.AddCrag(name, cwd); err != nil {
				return err
			}
			if err := config.Save(cfg); err != nil {
				return err
			}
			fmt.Printf("Registered crag %q at %s\n", name, cwd)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Crag name (default: directory name)")
	return cmd
}

func newCragListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all named crags",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if len(cfg.Crags) == 0 {
				fmt.Println("No crags registered.")
				fmt.Println("Register one with: belayer crag init --name <name>")
				return nil
			}
			for name, path := range cfg.Crags {
				fmt.Printf("  %-20s %s\n", name, path)
			}
			return nil
		},
	}
}

func newCdCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cd <crag-name>",
		Short: "Print the path to a named crag (for shell alias)",
		Long: `Print the directory path of a named crag. Use with a shell alias:
  bcd() { cd "$(belayer cd "$1")" }`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			path, err := cfg.ResolveCragPath(args[0])
			if err != nil {
				return err
			}
			fmt.Print(path)
			return nil
		},
	}
}
