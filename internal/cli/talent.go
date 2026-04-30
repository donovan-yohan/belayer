package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	belayer "github.com/donovan-yohan/belayer"
	"github.com/spf13/cobra"
)

type copySummary struct {
	written int
	skipped int
}

func newTeamCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "team",
		Aliases: []string{"teams", "talent"},
		Short:   "List, add, and remove local team identities",
	}
	cmd.AddCommand(newTeamListCmd(), newTeamAddCmd(), newTeamRemoveCmd())
	return cmd
}

func newTeamListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List local team catalog categories and identities",
		RunE: func(cmd *cobra.Command, args []string) error {
			cats, err := talentCategories()
			if err != nil {
				return err
			}
			for _, cat := range cats {
				talents, err := talentNames(cat)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", cat)
				for _, talent := range talents {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", talent)
				}
			}
			return nil
		},
	}
}

func newTeamAddCmd() *cobra.Command {
	var target string
	var force bool
	cmd := &cobra.Command{
		Use:     "add <category|category/team>",
		Aliases: []string{"install"},
		Short:   "Add local team catalog identities into .belayer/agents",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			category, talent, err := parseTalentRef(args[0])
			if err != nil {
				return err
			}
			if _, err := talentNames(category); err != nil {
				return err
			}
			if target == "" {
				target, err = os.Getwd()
				if err != nil {
					return err
				}
			}
			dst := filepath.Join(target, ".belayer", "agents")
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", dst, err)
			}

			var summary copySummary
			if talent != "" {
				summary, err = installOneTalent(category, talent, dst, force)
			} else {
				summary, err = installTalentCategory(category, dst, force)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Added %s: written=%d skipped=%d\n", args[0], summary.written, summary.skipped)
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "Project directory (default cwd)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing installed files")
	return cmd
}

func newTeamRemoveCmd() *cobra.Command {
	var target string
	cmd := &cobra.Command{
		Use:   "remove <category|category/team>",
		Short: "Remove local team identities from .belayer/agents",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			category, talent, err := parseTalentRef(args[0])
			if err != nil {
				return err
			}
			if _, err := talentNames(category); err != nil {
				return err
			}
			if target == "" {
				target, err = os.Getwd()
				if err != nil {
					return err
				}
			}
			dst := filepath.Join(target, ".belayer", "agents")

			var summary removeSummary
			if talent != "" {
				summary, err = removeOneTeam(category, talent, dst)
			} else {
				summary, err = removeTeamCategory(category, dst)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s: removed=%d skipped=%d\n", args[0], summary.removed, summary.skipped)
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "Project directory (default cwd)")
	return cmd
}

func parseTalentRef(ref string) (string, string, error) {
	parts := strings.Split(ref, "/")
	if len(parts) == 1 {
		if err := validateName(parts[0], "category"); err != nil {
			return "", "", err
		}
		return parts[0], "", nil
	}
	if len(parts) == 2 {
		if err := validateName(parts[0], "category"); err != nil {
			return "", "", err
		}
		if err := validateName(parts[1], "team"); err != nil {
			return "", "", err
		}
		return parts[0], parts[1], nil
	}
	return "", "", fmt.Errorf("invalid team reference %q (use category or category/team)", ref)
}

func validateName(name, label string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid %s %q", label, name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid %s %q: path separators are not allowed", label, name)
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("invalid %s %q: only letters, digits, dash, underscore, and dot are allowed", label, name)
	}
	return nil
}

func talentCategories() ([]string, error) {
	entries, err := belayer.TalentCatalog.ReadDir("examples/talent-catalog")
	if err != nil {
		return nil, fmt.Errorf("read team catalog: %w", err)
	}
	var cats []string
	for _, entry := range entries {
		if entry.IsDir() {
			cats = append(cats, entry.Name())
		}
	}
	sort.Strings(cats)
	return cats, nil
}

func talentNames(category string) ([]string, error) {
	if err := validateName(category, "category"); err != nil {
		return nil, err
	}
	if category == "development" {
		names, err := identityDirs(belayer.DefaultAgents, "agents")
		if err != nil {
			return nil, err
		}
		return names, nil
	}
	root := "examples/talent-catalog/" + category
	if _, err := fs.Stat(belayer.TalentCatalog, root); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("unknown team category %q", category)
		}
		return nil, fmt.Errorf("stat team category %q: %w", category, err)
	}
	names, err := identityDirs(belayer.TalentCatalog, root)
	if err != nil {
		return nil, err
	}
	return names, nil
}

func identityDirs(source fs.FS, root string) ([]string, error) {
	entries, err := fs.ReadDir(source, root)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", root, err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentPath := filepath.ToSlash(filepath.Join(root, entry.Name(), "agent.yaml"))
		if _, err := fs.Stat(source, agentPath); err == nil {
			names = append(names, entry.Name())
		} else if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat %s: %w", agentPath, err)
		}
	}
	sort.Strings(names)
	return names, nil
}

func installTalentCategory(category, dst string, force bool) (copySummary, error) {
	names, err := talentNames(category)
	if err != nil {
		return copySummary{}, err
	}
	var total copySummary
	for _, name := range names {
		summary, err := installOneTalent(category, name, dst, force)
		if err != nil {
			return copySummary{}, err
		}
		total.written += summary.written
		total.skipped += summary.skipped
	}
	return total, nil
}

func installOneTalent(category, name, dst string, force bool) (copySummary, error) {
	names, err := talentNames(category)
	if err != nil {
		return copySummary{}, err
	}
	found := false
	for _, n := range names {
		if n == name {
			found = true
			break
		}
	}
	if !found {
		return copySummary{}, fmt.Errorf("unknown team %q in category %q", name, category)
	}
	if category == "development" {
		return copyFSTree(belayer.DefaultAgents, "agents/"+name, filepath.Join(dst, name), force)
	}
	return copyFSTree(belayer.TalentCatalog, "examples/talent-catalog/"+category+"/"+name, filepath.Join(dst, name), force)
}

type removeSummary struct {
	removed int
	skipped int
}

func removeTeamCategory(category, dst string) (removeSummary, error) {
	names, err := talentNames(category)
	if err != nil {
		return removeSummary{}, err
	}
	var total removeSummary
	for _, name := range names {
		summary, err := removeOneTeam(category, name, dst)
		if err != nil {
			return removeSummary{}, err
		}
		total.removed += summary.removed
		total.skipped += summary.skipped
	}
	return total, nil
}

func removeOneTeam(category, name, dst string) (removeSummary, error) {
	names, err := talentNames(category)
	if err != nil {
		return removeSummary{}, err
	}
	found := false
	for _, n := range names {
		if n == name {
			found = true
			break
		}
	}
	if !found {
		return removeSummary{}, fmt.Errorf("unknown team %q in category %q", name, category)
	}

	path := filepath.Join(dst, name)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return removeSummary{skipped: 1}, nil
		}
		return removeSummary{}, fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.RemoveAll(path); err != nil {
		return removeSummary{}, fmt.Errorf("remove %s: %w", path, err)
	}
	return removeSummary{removed: 1}, nil
}

func copyFSTree(source fs.FS, root, dst string, force bool) (copySummary, error) {
	var summary copySummary
	err := fs.WalkDir(source, root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("rel %s: %w", path, err)
		}
		if rel == "." {
			if d.IsDir() {
				return os.MkdirAll(dst, 0o755)
			}
			return nil
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		if !force {
			if _, err := os.Stat(out); err == nil {
				summary.skipped++
				return nil
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("stat %s: %w", out, err)
			}
		}
		data, err := fs.ReadFile(source, path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(out), err)
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", out, err)
		}
		summary.written++
		return nil
	})
	if err != nil {
		return copySummary{}, err
	}
	return summary, nil
}
