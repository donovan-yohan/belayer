package cli

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	belayer "github.com/donovan-yohan/belayer"
	"github.com/donovan-yohan/belayer/internal/generatedtalent"
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
		Short:   "Manage local team identities and generated talent records",
	}
	cmd.AddCommand(newTeamListCmd(), newTeamAddCmd(), newTeamRemoveCmd(), newGeneratedTalentCmd())
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

func newGeneratedTalentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "generated",
		Aliases: []string{"generated-talents"},
		Short:   "Manage generated talent records in a crag-local pool",
	}
	cmd.AddCommand(newGeneratedTalentPersistCmd(), newGeneratedTalentListCmd(), newGeneratedTalentScaffoldCmd())
	return cmd
}

func newGeneratedTalentPersistCmd() *cobra.Command {
	var domain, role, lifecycle, status, sourceRequest, reason, note string
	var metadata []string
	var promotionEvidence []string
	var force bool
	cmd := &cobra.Command{
		Use:   "persist <crag> <id>",
		Short: "Persist compact generated talent metadata into a crag pool",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cragName, id := args[0], args[1]
			if err := validateName(cragName, "crag"); err != nil {
				return err
			}
			if err := validateName(id, "generated talent"); err != nil {
				return err
			}
			if err := validateRequiredFlag("domain", domain); err != nil {
				return err
			}
			if err := validateRequiredFlag("role", role); err != nil {
				return err
			}
			if err := validateRequiredFlag("source-request", sourceRequest); err != nil {
				return err
			}
			if err := validateRequiredFlag("reason", reason); err != nil {
				return err
			}
			if lifecycle == "" {
				lifecycle = "ephemeral"
			}
			if status == "" {
				status = "generated"
			}
			if err := validateTalentLifecycle(lifecycle); err != nil {
				return err
			}
			if err := validateGeneratedTalentStatus(status); err != nil {
				return err
			}
			parsedMetadata, err := parseKeyValueFlags(metadata)
			if err != nil {
				return err
			}
			if err := validateNonEmptyValues("promotion-evidence", promotionEvidence); err != nil {
				return err
			}
			if status == "promoted" && len(promotionEvidence) == 0 {
				return fmt.Errorf("--promotion-evidence is required when --status=promoted")
			}
			cragPath, err := cragDir(cragName)
			if err != nil {
				return err
			}
			if _, err := os.Stat(filepath.Join(cragPath, "crag.yaml")); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("crag %q does not exist", cragName)
				}
				return fmt.Errorf("stat crag.yaml: %w", err)
			}
			dir := filepath.Join(cragPath, "generated-talents", id)
			talentPath := filepath.Join(dir, "talent.yaml")
			now := time.Now().UTC().Format(time.RFC3339)
			record := generatedtalent.Record{
				SchemaVersion:     generatedtalent.SchemaVersion,
				ID:                id,
				Domain:            domain,
				Role:              role,
				Lifecycle:         lifecycle,
				Status:            status,
				SourceRequest:     sourceRequest,
				Reason:            reason,
				Metadata:          parsedMetadata,
				PromotionEvidence: promotionEvidence,
				CreatedAt:         now,
				UpdatedAt:         now,
			}
			if !force {
				if _, err := os.Stat(talentPath); err == nil {
					return fmt.Errorf("generated talent %q already exists in crag %q (use --force to overwrite)", id, cragName)
				} else if !os.IsNotExist(err) {
					return fmt.Errorf("stat generated talent: %w", err)
				}
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("mkdir generated talent: %w", err)
			}
			notesPath := filepath.Join(dir, "notes.md")
			if note != "" {
				if err := os.WriteFile(notesPath, []byte(note+"\n"), 0o644); err != nil {
					return fmt.Errorf("write generated talent notes: %w", err)
				}
			} else if force {
				if err := os.Remove(notesPath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("remove stale generated talent notes: %w", err)
				}
			}
			if err := generatedtalent.WriteRecord(talentPath, record); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Persisted generated talent %s in crag %s\n", id, cragName)
			return nil
		},
	}
	cmd.Flags().StringVar(&domain, "domain", "", "Prompt-visible domain metadata")
	cmd.Flags().StringVar(&role, "role", "", "Prompt-visible role metadata")
	cmd.Flags().StringVar(&lifecycle, "lifecycle", "ephemeral", "Runtime lifecycle: resident, resumable, or ephemeral")
	cmd.Flags().StringVar(&status, "status", "generated", "Generated talent status: generated, promoted, retired, or discarded")
	cmd.Flags().StringVar(&sourceRequest, "source-request", "", "Request, task, or turn identifier that caused generation")
	cmd.Flags().StringVar(&reason, "reason", "", "Why this generated talent was needed")
	cmd.Flags().StringArrayVar(&metadata, "metadata", nil, "Caller-provided key=value metadata; repeatable")
	cmd.Flags().StringArrayVar(&promotionEvidence, "promotion-evidence", nil, "Evidence path supporting promotion or reuse; repeatable")
	cmd.Flags().StringVar(&note, "note", "", "Optional human-readable notes.md content")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing generated talent record")
	_ = cmd.MarkFlagRequired("domain")
	_ = cmd.MarkFlagRequired("role")
	_ = cmd.MarkFlagRequired("source-request")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func newGeneratedTalentListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <crag>",
		Short: "List generated talent records for a crag",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cragName := args[0]
			if err := validateName(cragName, "crag"); err != nil {
				return err
			}
			dir, err := cragDir(cragName)
			if err != nil {
				return err
			}
			if _, err := os.Stat(filepath.Join(dir, "crag.yaml")); err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("crag %q does not exist", cragName)
				}
				return fmt.Errorf("stat crag.yaml: %w", err)
			}
			root := filepath.Join(dir, "generated-talents")
			entries, err := os.ReadDir(root)
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return fmt.Errorf("read generated talents: %w", err)
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			defer w.Flush()
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				path := filepath.Join(root, entry.Name(), "talent.yaml")
				record, err := generatedtalent.ReadRecord(path)
				if err != nil {
					if os.IsNotExist(err) {
						continue
					}
					return fmt.Errorf("read %s: %w", path, err)
				}
				if record.ID == "" {
					record.ID = entry.Name()
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", record.ID, record.Domain, record.Role, record.Lifecycle, record.Status)
			}
			return nil
		},
	}
}

func newGeneratedTalentScaffoldCmd() *cobra.Command {
	var target string
	var force bool
	cmd := &cobra.Command{
		Use:   "scaffold <crag> <id>",
		Short: "Scaffold a runnable project-local identity from generated talent metadata",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cragName, id := args[0], args[1]
			if err := validateName(cragName, "crag"); err != nil {
				return err
			}
			if err := validateName(id, "generated talent"); err != nil {
				return err
			}
			if target == "" {
				var err error
				target, err = os.Getwd()
				if err != nil {
					return err
				}
			}
			recordPath, err := generatedTalentRecordPath(cragName, id)
			if err != nil {
				return err
			}
			record, err := generatedtalent.ReadRecord(recordPath)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Errorf("generated talent %q does not exist in crag %q", id, cragName)
				}
				return err
			}
			identityDir, err := generatedtalent.ScaffoldIdentity(target, record, force)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Scaffolded generated talent %s at %s\n", id, identityDir)
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "Project directory (default cwd)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing scaffolded identity")
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

func generatedTalentRecordPath(cragName, id string) (string, error) {
	dir, err := generatedTalentDir(cragName, id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "talent.yaml"), nil
}

func generatedTalentDir(cragName, id string) (string, error) {
	dir, err := cragDir(cragName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "generated-talents", id), nil
}

func validateRequiredFlag(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("--%s is required", name)
	}
	return nil
}

func validateNonEmptyValues(name string, values []string) error {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("--%s values must be non-empty", name)
		}
	}
	return nil
}

func validateTalentLifecycle(lifecycle string) error {
	switch lifecycle {
	case "resident", "resumable", "ephemeral":
		return nil
	default:
		return fmt.Errorf("invalid lifecycle %q: must be resident, resumable, or ephemeral", lifecycle)
	}
}

func validateGeneratedTalentStatus(status string) error {
	switch status {
	case "generated", "promoted", "retired", "discarded":
		return nil
	default:
		return fmt.Errorf("invalid generated talent status %q: must be generated, promoted, retired, or discarded", status)
	}
}

func parseKeyValueFlags(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(values))
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		if !ok {
			return nil, fmt.Errorf("invalid metadata %q: use key=value", value)
		}
		key = strings.TrimSpace(key)
		if err := validateName(key, "metadata key"); err != nil {
			return nil, err
		}
		out[key] = strings.TrimSpace(val)
	}
	return out, nil
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
