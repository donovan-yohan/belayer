package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func newOrgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "Manage local Belayer organizations",
	}
	cmd.AddCommand(newOrgListCmd(), newOrgInitCmd(), newOrgLinkCmd())
	return cmd
}

func newOrgListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List local Belayer organizations",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := orgRoot()
			if err != nil {
				return err
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return fmt.Errorf("read org root: %w", err)
			}
			var names []string
			for _, entry := range entries {
				if entry.IsDir() {
					names = append(names, entry.Name())
				}
			}
			sort.Strings(names)
			for _, name := range names {
				fmt.Fprintln(cmd.OutOrStdout(), name)
			}
			return nil
		},
	}
}

func newOrgInitCmd() *cobra.Command {
	var kind, description string
	cmd := &cobra.Command{
		Use:   "init <name>",
		Short: "Create a local Belayer organization skeleton",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validateName(name, "org"); err != nil {
				return err
			}
			if kind == "" {
				kind = "custom"
			}
			root, err := orgRoot()
			if err != nil {
				return err
			}
			dir := filepath.Join(root, name)
			for _, sub := range []string{"", "teams", "sops", "gates", "evaluations", "promotions", "generated-talents"} {
				if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
					return fmt.Errorf("mkdir org skeleton: %w", err)
				}
			}
			if kind == "story" {
				if err := os.MkdirAll(filepath.Join(dir, "world-state"), 0o755); err != nil {
					return fmt.Errorf("mkdir world-state: %w", err)
				}
			}
			orgYAML := fmt.Sprintf("schema_version: \"belayer-org/v1\"\nname: %s\nkind: %s\n", name, kind)
			if description != "" {
				orgYAML += fmt.Sprintf("description: %q\n", description)
			}
			orgYAML += "catalog_categories: []\n"
			orgPath := filepath.Join(dir, "org.yaml")
			created, err := writeIfMissing(orgPath, []byte(orgYAML))
			if err != nil {
				return err
			}
			if created {
				fmt.Fprintf(cmd.OutOrStdout(), "Created org %s at %s\n", name, dir)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Org %s already exists at %s\n", name, dir)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "custom", "Org kind: development, story, research, or custom")
	cmd.Flags().StringVar(&description, "description", "", "Human-readable org summary")
	return cmd
}

func newOrgLinkCmd() *cobra.Command {
	var target string
	cmd := &cobra.Command{
		Use:   "link <name>",
		Short: "Link the current project to a local Belayer organization",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := validateName(name, "org"); err != nil {
				return err
			}
			orgDir, err := orgDir(name)
			if err != nil {
				return err
			}
			if _, err := os.Stat(filepath.Join(orgDir, "org.yaml")); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("org %q does not exist at %s", name, orgDir)
				}
				return fmt.Errorf("stat org.yaml: %w", err)
			}
			if target == "" {
				target, err = os.Getwd()
				if err != nil {
					return err
				}
			}
			configPath := filepath.Join(target, ".belayer", "config.yaml")
			if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
				return fmt.Errorf("mkdir .belayer: %w", err)
			}
			raw, err := os.ReadFile(configPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("read config: %w", err)
			}
			updated := setOrgLinkBlock(raw, name, "")
			if err := os.WriteFile(configPath, updated, 0o644); err != nil {
				return fmt.Errorf("write config: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Linked project to org %s\n", name)
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "Project directory (default cwd)")
	return cmd
}

func belayerHome() (string, error) {
	if home := os.Getenv("BELAYER_HOME"); home != "" {
		return home, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Join(userHome, ".belayer"), nil
}

func orgRoot() (string, error) {
	home, err := belayerHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "orgs"), nil
}

func orgDir(name string) (string, error) {
	root, err := orgRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, name), nil
}

func setOrgLinkBlock(raw []byte, name, explicitPath string) []byte {
	block := "org:\n  name: " + name + "\n"
	if explicitPath != "" {
		block += "  path: " + explicitPath + "\n"
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return []byte(block)
	}
	lines := strings.Split(string(raw), "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "org:" && len(line) == len(strings.TrimLeft(line, " \t")) {
			start = i
			break
		}
	}
	if start == -1 {
		out := append([]byte{}, raw...)
		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, '\n')
		}
		out = append(out, '\n')
		out = append(out, []byte(block)...)
		return out
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) == len(strings.TrimLeft(line, " \t")) && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			end = i
			break
		}
	}
	replacement := strings.Split(strings.TrimSuffix(block, "\n"), "\n")
	next := append([]string{}, lines[:start]...)
	next = append(next, replacement...)
	next = append(next, lines[end:]...)
	out := strings.Join(next, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return []byte(out)
}
