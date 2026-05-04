package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/daemon"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// pruneOrphan holds metadata about an orphaned belayer-* Hermes profile.
type pruneOrphan struct {
	ProfileName string
	ProfileDir  string
	CragSlug    string
	TalentName  string
	MemoryScope string
	DiskBytes   int64
}

// pruneSkipped holds metadata about a preserved (non-orphan) profile that was
// skipped because its memory scope is crag or talent.
type pruneSkipped struct {
	ProfileName string
	MemoryScope string
}

func newPruneCmd() *cobra.Command {
	var cragFilter string
	var dryRun, keepMemories, yes, includeScoped bool
	var dbPath string

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Remove orphaned blyr-* Hermes profiles",
		Long: `Remove orphaned blyr-* Hermes profiles.

An orphan is a profile directory that starts with "blyr-" but has no
matching row in the agent_runs store. This can happen when a session ends
abnormally or a profile is left behind after cleanup failures.

Profiles with memory.scope=crag or memory.scope=talent are intentionally
preserved across climbs and are skipped by default. Use --include-scoped
to include them in the prune target list.

By default the command lists orphans and prompts for confirmation before
removing anything. Use --dry-run to inspect without removing, or --yes for
non-interactive use in scripts.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve DB path.
			resolvedDB := dbPath
			if resolvedDB == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("prune: resolve home dir: %w", err)
				}
				resolvedDB = filepath.Join(home, ".belayer", "belayer.db")
			}

			// 1. Find orphans (and skipped preserved profiles).
			orphans, skipped, err := pruneListOrphansWithScope(resolvedDB, cragFilter, includeScoped)
			if err != nil {
				return fmt.Errorf("prune: find orphans: %w", err)
			}

			// 2. Print skipped preserved profiles.
			for _, s := range skipped {
				fmt.Fprintf(cmd.OutOrStdout(), "[skipped] %s — memory.scope=%s (preserved across climbs; use --include-scoped to remove)\n",
					s.ProfileName, s.MemoryScope)
			}

			if len(orphans) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No orphan profiles found.")
				return nil
			}

			// 3. Print list.
			printOrphanList(cmd.OutOrStdout(), orphans)

			// 4. If --dry-run, exit without removing.
			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "\n(dry-run) Would remove %d profile(s).\n", len(orphans))
				return nil
			}

			// 5. Prompt unless --yes.
			if !yes {
				if !isatty.IsTerminal(os.Stdin.Fd()) {
					return fmt.Errorf("stdin is not a terminal; use --yes to confirm non-interactively")
				}
				fmt.Fprintf(cmd.OutOrStdout(), "\nRemove %d orphan profile(s)? [y/N]: ", len(orphans))
				reader := bufio.NewReader(os.Stdin)
				answer, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("prune: read confirmation: %w", err)
				}
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}

			// 6. Remove each orphan (with optional memory archive).
			removed := 0
			archived := 0
			for _, o := range orphans {
				if keepMemories {
					if n, err := archiveMemorySnapshot(o); err != nil {
						// best-effort: warn but do not abort
						fmt.Fprintf(cmd.ErrOrStderr(), "warn: archive memories for %s: %v\n", o.ProfileName, err)
					} else if n > 0 {
						archived += n
					}
				}

				if err := daemon.TeardownProfile(o.ProfileName); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warn: teardown %s: %v\n", o.ProfileName, err)
					continue
				}
				removed++
			}

			// 7. Print summary.
			if keepMemories {
				fmt.Fprintf(cmd.OutOrStdout(), "Removed %d profile(s), archived %d memory snapshot(s).\n", removed, archived)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Removed %d profile(s).\n", removed)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cragFilter, "crag", "", "Filter to one crag slug")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be removed without removing")
	cmd.Flags().BoolVar(&keepMemories, "keep-memories", false, "Archive memories/ before delete")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip interactive confirmation")
	cmd.Flags().StringVar(&dbPath, "db", "", "SQLite database path (default ~/.belayer/belayer.db)")
	cmd.Flags().BoolVar(&includeScoped, "include-scoped", false, "Include crag/talent-scoped profiles in prune targets (override preservation safety)")
	return cmd
}

// pruneListOrphans finds all belayer-* profile directories that have no
// matching row in agent_runs. If cragFilter is non-empty, only profiles whose
// crag_slug matches are included.
//
// Kept for backward compatibility; delegates to pruneListOrphansWithScope with
// includeScoped=false (safe default: climb-scoped orphans only).
func pruneListOrphans(dbPath, cragFilter string) ([]pruneOrphan, error) {
	orphans, _, err := pruneListOrphansWithScope(dbPath, cragFilter, false)
	return orphans, err
}

// pruneListOrphansWithScope finds all belayer-* profile directories that have
// no matching row in agent_runs, separating them into orphans (to remove) and
// skipped (preserved across climbs). If includeScoped is true, crag/talent-scoped
// profiles are treated as orphans. If cragFilter is non-empty, only profiles
// whose crag_slug matches are included.
//
// NOTE: orphan detection logic is duplicated from doctor (Task 4.A) to avoid
// a merge conflict in parallel implementation. TODO: DRY into profile_health.go.
func pruneListOrphansWithScope(dbPath, cragFilter string, includeScoped bool) (orphans []pruneOrphan, skipped []pruneSkipped, _ error) {
	// Find profiles root.
	profilesRoot, err := daemon.ProfilesRoot()
	if err != nil {
		return nil, nil, fmt.Errorf("resolve profiles root: %w", err)
	}

	// Enumerate all belayer-* subdirectories.
	entries, err := os.ReadDir(profilesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read profiles dir %s: %w", profilesRoot, err)
	}

	var candidates []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Must be a belayer-managed fork (not the base "blyr" profile itself).
		if name == "blyr" || !strings.HasPrefix(name, "blyr-") {
			continue
		}
		candidates = append(candidates, name)
	}

	if len(candidates) == 0 {
		return nil, nil, nil
	}

	// Open the store read-only to query known profiles.
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open store %s: %w", dbPath, err)
	}
	defer st.Close()

	// Build a set of profile names referenced by any agent_runs row.
	knownProfiles, err := pruneKnownProfiles(st)
	if err != nil {
		return nil, nil, fmt.Errorf("query known profiles: %w", err)
	}

	// Identify orphans and preserved profiles.
	for _, name := range candidates {
		if knownProfiles[name] {
			continue
		}

		// Parse crag slug and metadata from .belayer-talent.yaml (best-effort).
		cragSlug, talentName, memoryScope := pruneReadMetadataWithScope(filepath.Join(profilesRoot, name))

		// Apply crag filter if set.
		if cragFilter != "" && cragSlug != cragFilter {
			continue
		}

		// Determine if this profile should be preserved (crag/talent-scoped) or pruned.
		isPreserved := !includeScoped && (memoryScope == "crag" || memoryScope == "talent")
		if isPreserved {
			skipped = append(skipped, pruneSkipped{
				ProfileName: name,
				MemoryScope: memoryScope,
			})
			continue
		}

		// Compute disk usage.
		size := pruneDirSize(filepath.Join(profilesRoot, name))

		orphans = append(orphans, pruneOrphan{
			ProfileName: name,
			ProfileDir:  filepath.Join(profilesRoot, name),
			CragSlug:    cragSlug,
			TalentName:  talentName,
			MemoryScope: memoryScope,
			DiskBytes:   size,
		})
	}

	return orphans, skipped, nil
}

// pruneKnownProfiles returns a set of profile names that appear in the
// agent_runs table. These profiles are considered "active" and must not be
// pruned even if they appear to be orphans.
func pruneKnownProfiles(st *store.Store) (map[string]bool, error) {
	db := st.DB()
	rows, err := db.Query(`SELECT DISTINCT profile FROM agent_runs WHERE profile != '' AND profile IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("query agent_runs profiles: %w", err)
	}
	defer rows.Close()

	known := make(map[string]bool)
	for rows.Next() {
		var profile string
		if err := rows.Scan(&profile); err != nil {
			return nil, fmt.Errorf("scan profile: %w", err)
		}
		if profile != "" {
			known[profile] = true
		}
	}
	return known, rows.Err()
}

// pruneReadMetadataWithScope reads crag_slug, talent_name, and memory_scope
// from a profile's .belayer-talent.yaml. Falls back to parsing the profile name
// for crag/talent and defaults memory_scope to "climb" (ephemeral) if missing.
func pruneReadMetadataWithScope(profileDir string) (cragSlug, talentName, memoryScope string) {
	data, err := os.ReadFile(filepath.Join(profileDir, ".belayer-talent.yaml"))
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "crag_slug:") {
				cragSlug = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "crag_slug:")), `"'`)
			}
			if strings.HasPrefix(line, "talent_name:") {
				talentName = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "talent_name:")), `"'`)
			}
			if strings.HasPrefix(line, "memory_scope:") {
				memoryScope = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "memory_scope:")), `"'`)
			}
		}
		if cragSlug != "" {
			if memoryScope == "" {
				memoryScope = "climb"
			}
			return
		}
	}

	// Fallback: parse from profile name "blyr-<crag>-<talent>".
	profileName := filepath.Base(profileDir)
	rest := strings.TrimPrefix(profileName, "blyr-")
	parts := strings.SplitN(rest, "-", 2)
	if len(parts) == 2 {
		cragSlug, talentName = parts[0], parts[1]
	} else {
		cragSlug = rest
	}
	if memoryScope == "" {
		memoryScope = "climb"
	}
	return
}

// pruneReadMetadata reads crag_slug and talent_name from a profile's
// .belayer-talent.yaml. Falls back to parsing the profile name if the file is
// missing or unreadable.
func pruneReadMetadata(profileDir string) (cragSlug, talentName string) {
	data, err := os.ReadFile(filepath.Join(profileDir, ".belayer-talent.yaml"))
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "crag_slug:") {
				cragSlug = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "crag_slug:")), `"'`)
			}
			if strings.HasPrefix(line, "talent_name:") {
				talentName = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "talent_name:")), `"'`)
			}
		}
		if cragSlug != "" {
			return
		}
	}

	// Fallback: parse from profile name "blyr-<crag>-<talent>".
	profileName := filepath.Base(profileDir)
	rest := strings.TrimPrefix(profileName, "blyr-")
	parts := strings.SplitN(rest, "-", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return rest, ""
}

// pruneDirSize returns the total byte count of all regular files under dir.
// Errors (e.g. permission denied) are silently ignored; we return whatever
// we could count.
func pruneDirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// printOrphanList prints the list of orphans grouped by crag to stdout.
func printOrphanList(out interface{ Write([]byte) (int, error) }, orphans []pruneOrphan) {
	// Group by crag.
	grouped := make(map[string][]pruneOrphan)
	var order []string
	seen := make(map[string]bool)
	for _, o := range orphans {
		key := o.CragSlug
		if key == "" {
			key = "(unknown)"
		}
		if !seen[key] {
			order = append(order, key)
			seen[key] = true
		}
		grouped[key] = append(grouped[key], o)
	}

	w := fmt.Sprintf // alias for brevity
	writeStr := func(s string) { _, _ = fmt.Fprint(out, s) }

	writeStr("Orphan profiles:\n")
	for _, crag := range order {
		writeStr(w("\n  crag: %s\n", crag))
		for _, o := range grouped[crag] {
			writeStr(w("    %-48s  %s\n", o.ProfileName, humanBytes(o.DiskBytes)))
		}
	}
}

// humanBytes formats byte counts as a human-readable string.
func humanBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// archiveMemorySnapshot copies MEMORY.md and USER.md from the profile's
// memories/ directory into ~/.belayer/crags/<crag>/evaluations/<talent>/.
// Returns the count of files successfully archived.
func archiveMemorySnapshot(o pruneOrphan) (int, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, fmt.Errorf("resolve home dir: %w", err)
	}

	cragSlug := o.CragSlug
	if cragSlug == "" {
		cragSlug = "unknown"
	}
	talentName := o.TalentName
	if talentName == "" {
		talentName = o.ProfileName
	}

	destDir := filepath.Join(home, ".belayer", "crags", cragSlug, "evaluations", talentName)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return 0, fmt.Errorf("mkdir %s: %w", destDir, err)
	}

	datePrefix := time.Now().UTC().Format("2006-01-02")
	memoriesDir := filepath.Join(o.ProfileDir, "memories")

	archived := 0
	for _, memFile := range []string{"MEMORY.md", "USER.md"} {
		src := filepath.Join(memoriesDir, memFile)
		data, err := os.ReadFile(src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return archived, fmt.Errorf("read %s: %w", src, err)
		}

		// Write as a JSON snapshot so evaluations/ consumers can parse metadata.
		snap := map[string]string{
			"profile":    o.ProfileName,
			"crag":       cragSlug,
			"talent":     talentName,
			"source":     memFile,
			"pruned_at":  time.Now().UTC().Format(time.RFC3339),
			"content":    string(data),
		}
		snapJSON, err := json.Marshal(snap)
		if err != nil {
			return archived, fmt.Errorf("marshal snapshot for %s: %w", memFile, err)
		}

		base := strings.TrimSuffix(memFile, ".md")
		destFile := filepath.Join(destDir, fmt.Sprintf("%s-%s-pruned.json", datePrefix, strings.ToLower(base)))
		if err := os.WriteFile(destFile, snapJSON, 0o644); err != nil {
			return archived, fmt.Errorf("write snapshot %s: %w", destFile, err)
		}
		archived++
	}

	return archived, nil
}
