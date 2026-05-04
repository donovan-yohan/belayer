package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/donovan-yohan/belayer/internal/daemon"
)

func newUninstallCmd() *cobra.Command {
	var crag string
	var yes, keepMemories bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove belayer Hermes profiles and state (per-crag or global)",
		Long: `Remove belayer Hermes profiles and workspace state.

Per-crag mode (--crag <slug>):
  Removes all blyr-<slug>-* profiles and ~/.belayer/crags/<slug>/ workspace
  state. Preserves the base blyr profile, other crags, and the talent catalog.

Global mode (no --crag):
  Removes ALL blyr-* profiles (including the base blyr profile) and the
  entire ~/.belayer/ directory. Other Hermes profiles (default, etc.) are left
  intact. ~/.hermes/auth.json is preserved.

Both modes prompt for confirmation unless --yes is given.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if crag != "" {
				return uninstallCrag(cmd, crag, yes, keepMemories)
			}
			return uninstallGlobal(cmd, yes)
		},
	}
	cmd.Flags().StringVar(&crag, "crag", "", "Per-crag uninstall (removes only that crag's profiles and workspace state)")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&keepMemories, "keep-memories", false, "Archive memories/ before delete (per-crag only)")
	return cmd
}

// validateCragSlug rejects slugs that would allow path traversal or are
// invalid as Hermes profile name segments. The slug must be non-empty,
// contain no path separator characters, and must not contain "..".
func validateCragSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("crag slug must not be empty")
	}
	if strings.Contains(slug, "/") || strings.Contains(slug, "\\") {
		return fmt.Errorf("crag slug %q must not contain path separators", slug)
	}
	if strings.Contains(slug, "..") {
		return fmt.Errorf("crag slug %q must not contain \"..\"", slug)
	}
	// Reuse Hermes profile name validation: the slug becomes a segment of
	// "blyr-<slug>-<talent>" so it must meet the same character constraints.
	if err := daemon.ValidateProfileName("blyr-" + slug + "-x"); err != nil {
		return fmt.Errorf("crag slug %q is invalid: only lowercase alphanumeric, hyphens, and underscores are allowed", slug)
	}
	return nil
}

// stdinIsTTY returns true when os.Stdin is a character device (interactive
// terminal). Used to guard interactive confirmation prompts.
func stdinIsTTY() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// confirmPrompt writes prompt to out, reads a line from in, and returns true
// iff the user entered "y" or "Y". Any other input (including empty) returns
// false.
func confirmPrompt(out io.Writer, in io.Reader, prompt string) bool {
	fmt.Fprint(out, prompt)
	scanner := bufio.NewScanner(in)
	if scanner.Scan() {
		answer := strings.TrimSpace(scanner.Text())
		return strings.EqualFold(answer, "y")
	}
	return false
}

// listBelayerProfiles returns all profile names under the profiles root that
// start with "blyr-". The base "blyr" profile is never included.
func listBelayerProfiles(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "blyr-") {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// listCragProfiles returns only profile names that start with "blyr-<slug>-".
func listCragProfiles(root, slug string) ([]string, error) {
	all, err := listBelayerProfiles(root)
	if err != nil {
		return nil, err
	}
	prefix := "blyr-" + slug + "-"
	var names []string
	for _, n := range all {
		if strings.HasPrefix(n, prefix) {
			names = append(names, n)
		}
	}
	return names, nil
}

// ── Per-crag uninstall (Task 4.C) ────────────────────────────────────────────

func uninstallCrag(cmd *cobra.Command, slug string, yes, keepMemories bool) error {
	if err := validateCragSlug(slug); err != nil {
		return err
	}

	profilesRoot, err := daemon.ProfilesRoot()
	if err != nil {
		return fmt.Errorf("resolve profiles root: %w", err)
	}

	profiles, err := listCragProfiles(profilesRoot, slug)
	if err != nil {
		return err
	}

	belayerDir, err := belayerHome()
	if err != nil {
		return fmt.Errorf("resolve belayer home: %w", err)
	}
	cragWS := filepath.Join(belayerDir, "crags", slug)
	cragWSExists := false
	if _, statErr := os.Stat(cragWS); statErr == nil {
		cragWSExists = true
	}

	out := cmd.OutOrStdout()

	if len(profiles) == 0 && !cragWSExists {
		fmt.Fprintf(out, "Nothing to remove for crag %q (no profiles or workspace state found).\n", slug)
		return nil
	}

	// Print summary.
	fmt.Fprintf(out, "Will remove for crag %q:\n", slug)
	for _, p := range profiles {
		fmt.Fprintf(out, "  profile: %s\n", p)
	}
	if cragWSExists {
		fmt.Fprintf(out, "  workspace: %s\n", cragWS)
	}

	if !yes {
		if !stdinIsTTY() {
			return fmt.Errorf("stdin is not a TTY; re-run with --yes to confirm")
		}
		msg := fmt.Sprintf("Remove %d profile(s) + crag dir for %q? [y/N]: ", len(profiles), slug)
		if !confirmPrompt(out, cmd.InOrStdin(), msg) {
			fmt.Fprintln(out, "Aborted.")
			return nil
		}
	}

	// Archive memories before deletion if requested.
	if keepMemories && len(profiles) > 0 {
		archiveRoot := filepath.Join(belayerDir, "uninstall-archive",
			fmt.Sprintf("%s-%s", slug, time.Now().UTC().Format("20060102T150405Z")))
		if err := archiveProfileMemories(profiles, profilesRoot, archiveRoot); err != nil {
			return fmt.Errorf("archive memories: %w", err)
		}
		fmt.Fprintf(out, "Archived memories to %s\n", archiveRoot)
	}

	// Tear down each profile.
	removed := 0
	for _, p := range profiles {
		if err := daemon.TeardownProfile(p); err != nil {
			return fmt.Errorf("teardown profile %s: %w", p, err)
		}
		removed++
	}

	// Remove crag workspace.
	if cragWSExists {
		if err := os.RemoveAll(cragWS); err != nil {
			return fmt.Errorf("remove crag workspace %s: %w", cragWS, err)
		}
	}

	fmt.Fprintf(out, "Removed %d profile(s)", removed)
	if cragWSExists {
		fmt.Fprintf(out, " + crag workspace")
	}
	fmt.Fprintf(out, " for crag %q.\n", slug)
	return nil
}

// archiveProfileMemories copies MEMORY.md and USER.md from each profile's
// memories/ directory into archiveRoot/<talentName>/<date>-uninstall.json.
func archiveProfileMemories(profiles []string, profilesRoot, archiveRoot string) error {
	if err := os.MkdirAll(archiveRoot, 0o755); err != nil {
		return fmt.Errorf("mkdir archive root: %w", err)
	}
	date := time.Now().UTC().Format("2006-01-02")
	for _, p := range profiles {
		memoriesDir := filepath.Join(profilesRoot, p, "memories")
		// Extract a human-readable talent segment from the profile name for the
		// archive subdirectory name. We use everything after "blyr-<crag>-".
		// E.g. "blyr-software-co-supervisor" → we strip the "blyr-" prefix
		// and find the remaining path.
		talentSegment := p // fallback: use full profile name
		parts := strings.SplitN(p, "-", 3)
		if len(parts) == 3 {
			talentSegment = parts[2]
		}

		talentArchiveDir := filepath.Join(archiveRoot, talentSegment)
		if err := os.MkdirAll(talentArchiveDir, 0o755); err != nil {
			return fmt.Errorf("mkdir talent archive dir: %w", err)
		}

		payload := map[string]string{
			"profile": p,
			"date":    date,
		}

		for _, fname := range []string{"MEMORY.md", "USER.md"} {
			src := filepath.Join(memoriesDir, fname)
			data, err := os.ReadFile(src)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return fmt.Errorf("read %s from %s: %w", fname, p, err)
			}
			payload[fname] = string(data)
		}

		out, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal archive for %s: %w", p, err)
		}

		archivePath := filepath.Join(talentArchiveDir, date+"-uninstall.json")
		if err := os.WriteFile(archivePath, out, 0o644); err != nil {
			return fmt.Errorf("write archive for %s: %w", p, err)
		}
	}
	return nil
}

// ── Global uninstall (Task 4.D) ───────────────────────────────────────────────

func uninstallGlobal(cmd *cobra.Command, yes bool) error {
	profilesRoot, err := daemon.ProfilesRoot()
	if err != nil {
		return fmt.Errorf("resolve profiles root: %w", err)
	}

	// Collect all blyr-* profiles (excludes "blyr" base profile).
	forkProfiles, err := listBelayerProfiles(profilesRoot)
	if err != nil {
		return err
	}

	// Check whether the base "blyr" profile exists.
	baseProfileDir := filepath.Join(profilesRoot, "blyr")
	baseExists := false
	if _, statErr := os.Stat(baseProfileDir); statErr == nil {
		baseExists = true
	}

	belayerDir, err := belayerHome()
	if err != nil {
		return fmt.Errorf("resolve belayer home: %w", err)
	}
	belayerDirExists := false
	if _, statErr := os.Stat(belayerDir); statErr == nil {
		belayerDirExists = true
	}

	out := cmd.OutOrStdout()

	if len(forkProfiles) == 0 && !baseExists && !belayerDirExists {
		fmt.Fprintln(out, "Nothing to remove (no belayer profiles or state found).")
		return nil
	}

	// Print summary.
	fmt.Fprintln(out, "Will remove ALL belayer state:")
	for _, p := range forkProfiles {
		fmt.Fprintf(out, "  profile: %s\n", p)
	}
	if baseExists {
		fmt.Fprintf(out, "  base profile: %s\n", baseProfileDir)
	}
	if belayerDirExists {
		fmt.Fprintf(out, "  belayer home: %s\n", belayerDir)
	}

	if !yes {
		if !stdinIsTTY() {
			return fmt.Errorf("stdin is not a TTY; re-run with --yes to confirm")
		}
		msg := "This will remove ALL belayer state, including the base profile,\n" +
			"all crag forks, talent catalog, and daemon state. Continue? [y/N]: "
		if !confirmPrompt(out, cmd.InOrStdin(), msg) {
			fmt.Fprintln(out, "Aborted.")
			return nil
		}
	}

	// Tear down all fork profiles.
	removedForks := 0
	for _, p := range forkProfiles {
		if err := daemon.TeardownProfile(p); err != nil {
			return fmt.Errorf("teardown profile %s: %w", p, err)
		}
		removedForks++
	}

	// Remove the base "blyr" profile directly (TeardownProfile refuses base).
	if baseExists {
		if err := os.RemoveAll(baseProfileDir); err != nil {
			return fmt.Errorf("remove base profile %s: %w", baseProfileDir, err)
		}
	}

	// Remove ~/.belayer/ entirely.
	if belayerDirExists {
		if err := os.RemoveAll(belayerDir); err != nil {
			return fmt.Errorf("remove belayer home %s: %w", belayerDir, err)
		}
	}

	// Remove ~/.hermes/plugins/belayer* symlinks if present (installed by
	// belayer auth ensure). We search for belayer* entries only under the
	// plugins/ dir, and skip quietly if the directory does not exist.
	// profilesRoot is ~/.hermes/profiles, so the hermes root is ~/.hermes.
	hermesRoot := filepath.Dir(profilesRoot)
	hermesPluginsDir := filepath.Join(hermesRoot, "plugins")
	if entries, err := os.ReadDir(hermesPluginsDir); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "belayer") {
				target := filepath.Join(hermesPluginsDir, e.Name())
				// Only remove symlinks to avoid accidentally destroying non-belayer
				// files that happen to start with "belayer".
				if e.Type()&os.ModeSymlink != 0 {
					if rmErr := os.Remove(target); rmErr != nil {
						// Non-fatal: log and continue.
						fmt.Fprintf(out, "Warning: could not remove %s: %v\n", target, rmErr)
					}
				}
			}
		}
	}

	// Print summary.
	totalProfiles := removedForks
	if baseExists {
		totalProfiles++
	}
	fmt.Fprintf(out, "Removed %d profile(s)", totalProfiles)
	if belayerDirExists {
		fmt.Fprintf(out, " + belayer home")
	}
	fmt.Fprintln(out, ". Belayer has been uninstalled.")
	return nil
}
