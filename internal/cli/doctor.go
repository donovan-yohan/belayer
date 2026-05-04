package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/daemon"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/spf13/cobra"
)

// doctorAuthStaleDefaultDays is the default number of days after which
// auth.json is considered stale. Can be overridden via BELAYER_AUTH_STALE_DAYS.
const doctorAuthStaleDefaultDays = 30

// doctorProfileStatus is the health status of a single belayer-* profile.
type doctorProfileStatus string

const (
	doctorProfileActive doctorProfileStatus = "active"
	doctorProfileOrphan doctorProfileStatus = "orphan"
)

// doctorProfileEntry holds display data for one belayer-* profile.
type doctorProfileEntry struct {
	ProfileName string              `json:"profile_name"`
	CragSlug    string              `json:"crag_slug"`
	Status      doctorProfileStatus `json:"status"`
	DiskBytes   int64               `json:"disk_bytes"`
	Reason      string              `json:"reason,omitempty"` // non-empty for orphans
}

// doctorBaseProfile holds data for the base belayer profile.
type doctorBaseProfile struct {
	Path        string     `json:"path"`
	Present     bool       `json:"present"`
	AuthMtime   *time.Time `json:"auth_mtime,omitempty"`
	AuthAgeDays int        `json:"auth_age_days,omitempty"`
	AuthStale   bool       `json:"auth_stale,omitempty"`
	PluginOK    bool       `json:"plugin_ok"`
}

// doctorReport is the top-level output for --json mode.
type doctorReport struct {
	Base            doctorBaseProfile  `json:"base"`
	Crags           []doctorCragReport `json:"crags"`
	Issues          []string           `json:"issues"`
	TotalDiskBytes  int64              `json:"total_disk_bytes"`
	TotalProfiles   int                `json:"total_profiles"`
	OrphanCount     int                `json:"orphan_count"`
	// orphanAgentRuns are profile names present in agent_runs but missing on disk.
	OrphanAgentRuns []string           `json:"orphan_agent_runs,omitempty"`
}

// doctorCragReport groups profiles for one crag slug.
type doctorCragReport struct {
	CragSlug  string               `json:"crag_slug"`
	Profiles  []doctorProfileEntry `json:"profiles"`
	DiskBytes int64                `json:"disk_bytes"`
}

func newDoctorCmd() *cobra.Command {
	var cragFilter string
	var jsonOut bool
	var dbPath string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Report Hermes profile health (orphans, auth staleness, disk usage)",
		Long: `Report health of belayer-* Hermes profiles.

Lists all belayer-* Hermes profiles, groups them by crag slug, and checks:
  - Active vs orphaned profiles (no matching agent_runs row).
  - Orphaned agent_runs rows (profile dir gone).
  - Base belayer profile presence and auth.json staleness.
  - Disk usage per crag.

Use --crag to filter to a single crag. Use --json to emit structured JSON
for tooling (e.g. jq).

Read-only: doctor never modifies any state.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve DB path: default is ~/.belayer/belayer.db.
			resolvedDB := dbPath
			if resolvedDB == "" {
				home, err := belayerHome()
				if err != nil {
					return fmt.Errorf("doctor: resolve home: %w", err)
				}
				resolvedDB = filepath.Join(home, "belayer.db")
			}

			// Resolve auth-stale threshold.
			staleDays := doctorAuthStaleDefaultDays
			if v := os.Getenv("BELAYER_AUTH_STALE_DAYS"); v != "" {
				var parsed int
				if _, err := fmt.Sscanf(v, "%d", &parsed); err == nil && parsed > 0 {
					staleDays = parsed
				}
			}

			report, err := buildDoctorReport(resolvedDB, cragFilter, staleDays)
			if err != nil {
				return fmt.Errorf("doctor: %w", err)
			}

			if jsonOut {
				return printDoctorJSON(cmd, report)
			}
			printDoctorText(cmd, report, staleDays)
			return nil
		},
	}

	cmd.Flags().StringVar(&cragFilter, "crag", "", "Filter to one crag slug")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit JSON instead of human-readable text")
	cmd.Flags().StringVar(&dbPath, "db", "", "SQLite database path (default ~/.belayer/belayer.db)")
	return cmd
}

// buildDoctorReport assembles a doctorReport from the profile dirs and store.
func buildDoctorReport(dbPath, cragFilter string, staleDays int) (doctorReport, error) {
	var report doctorReport

	// 1. Resolve ProfilesRoot and stat base profile.
	profilesRoot, err := daemon.ProfilesRoot()
	if err != nil {
		return report, fmt.Errorf("resolve profiles root: %w", err)
	}

	baseProfileDir := filepath.Join(profilesRoot, "belayer")
	report.Base = buildBaseProfileReport(baseProfileDir, staleDays)

	// 2. Enumerate belayer-* profile dirs (excludes base "belayer").
	profileDirs, err := doctorListProfileDirs(profilesRoot)
	if err != nil {
		return report, fmt.Errorf("list profile dirs: %w", err)
	}

	// 3. Open the store read-only and query known profiles.
	knownProfiles, orphanAgentRuns, err := doctorQueryStore(dbPath, profileDirs)
	if err != nil {
		// Store missing or not yet created: treat all dirs as having no known runs.
		// Still report the filesystem view, but issue a warning.
		knownProfiles = map[string]bool{}
		orphanAgentRuns = nil
	}

	// 4. Build per-profile entries, filtering by crag if requested.
	//    Build a set of dirs for orphanAgentRuns detection too.
	dirSet := make(map[string]bool, len(profileDirs))
	for _, p := range profileDirs {
		dirSet[p] = true
	}

	// Group profiles by crag.
	cragMap := make(map[string][]doctorProfileEntry)
	var cragOrder []string
	cragSeen := make(map[string]bool)

	for _, profileName := range profileDirs {
		cragSlug, _ := doctorReadCragSlug(filepath.Join(profilesRoot, profileName), profileName)

		if cragFilter != "" && cragSlug != cragFilter {
			continue
		}

		status := doctorProfileActive
		reason := ""
		if !knownProfiles[profileName] {
			status = doctorProfileOrphan
			reason = "no matching agent_run"
		}

		diskBytes := pruneDirSize(filepath.Join(profilesRoot, profileName))

		entry := doctorProfileEntry{
			ProfileName: profileName,
			CragSlug:    cragSlug,
			Status:      status,
			DiskBytes:   diskBytes,
			Reason:      reason,
		}

		key := cragSlug
		if key == "" {
			key = "(unknown)"
		}
		if !cragSeen[key] {
			cragOrder = append(cragOrder, key)
			cragSeen[key] = true
		}
		cragMap[key] = append(cragMap[key], entry)

		report.TotalProfiles++
		report.TotalDiskBytes += diskBytes
		if status == doctorProfileOrphan {
			report.OrphanCount++
		}
	}

	// Filter orphan agent_runs by crag if requested.
	filteredOrphanRuns := orphanAgentRuns
	if cragFilter != "" {
		filteredOrphanRuns = nil
		for _, name := range orphanAgentRuns {
			cragSlug, _ := doctorSplitProfileName(name)
			if cragSlug == cragFilter {
				filteredOrphanRuns = append(filteredOrphanRuns, name)
			}
		}
	}
	report.OrphanAgentRuns = filteredOrphanRuns

	// Sort crags alphabetically.
	sort.Strings(cragOrder)
	for _, key := range cragOrder {
		profiles := cragMap[key]
		var cragDisk int64
		for _, p := range profiles {
			cragDisk += p.DiskBytes
		}
		slug := key
		if slug == "(unknown)" {
			slug = ""
		}
		report.Crags = append(report.Crags, doctorCragReport{
			CragSlug:  slug,
			Profiles:  profiles,
			DiskBytes: cragDisk,
		})
	}

	// 5. Build issues list.
	report.Issues = buildDoctorIssues(report, staleDays)

	return report, nil
}

// buildBaseProfileReport stats the base belayer profile directory.
func buildBaseProfileReport(baseProfileDir string, staleDays int) doctorBaseProfile {
	base := doctorBaseProfile{
		Path: baseProfileDir,
	}

	info, err := os.Stat(baseProfileDir)
	if err != nil || !info.IsDir() {
		return base
	}
	base.Present = true

	// Check auth.json.
	authPath := filepath.Join(baseProfileDir, "auth.json")
	if authInfo, err := os.Stat(authPath); err == nil {
		mtime := authInfo.ModTime()
		base.AuthMtime = &mtime
		ageDays := int(time.Since(mtime).Hours() / 24)
		base.AuthAgeDays = ageDays
		base.AuthStale = ageDays > staleDays
	}

	// Check belayer plugin.
	pluginPath := filepath.Join(baseProfileDir, "plugins", "belayer", "plugin.yaml")
	if _, err := os.Stat(pluginPath); err == nil {
		base.PluginOK = true
	}

	return base
}

// doctorListProfileDirs returns the names (not full paths) of all belayer-*
// profile directories under profilesRoot, excluding the base "belayer" profile.
func doctorListProfileDirs(profilesRoot string) ([]string, error) {
	entries, err := os.ReadDir(profilesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read profiles dir: %w", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if name == "belayer" || !strings.HasPrefix(name, "belayer-") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// doctorQueryStore opens the store DB and returns:
//   - knownProfiles: set of profile names with at least one agent_runs row.
//   - orphanAgentRuns: profile names in agent_runs that have no matching dir.
func doctorQueryStore(dbPath string, profileDirs []string) (map[string]bool, []string, error) {
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	db := st.DB()
	rows, err := db.Query(
		`SELECT DISTINCT profile FROM agent_runs WHERE profile != '' AND profile IS NOT NULL AND profile LIKE 'belayer-%'`,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("query agent_runs: %w", err)
	}
	defer rows.Close()

	knownProfiles := make(map[string]bool)
	for rows.Next() {
		var profile string
		if err := rows.Scan(&profile); err != nil {
			return nil, nil, fmt.Errorf("scan profile: %w", err)
		}
		if profile != "" {
			knownProfiles[profile] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate agent_runs: %w", err)
	}

	// Find agent_runs profile names that have no matching directory.
	dirSet := make(map[string]bool, len(profileDirs))
	for _, p := range profileDirs {
		dirSet[p] = true
	}
	var orphanRuns []string
	for profileName := range knownProfiles {
		if !dirSet[profileName] {
			orphanRuns = append(orphanRuns, profileName)
		}
	}
	sort.Strings(orphanRuns)

	return knownProfiles, orphanRuns, nil
}

// doctorReadCragSlug reads the crag_slug from a profile's .belayer-talent.yaml.
// Falls back to parsing the profile name if the file is missing or unreadable.
func doctorReadCragSlug(profileDir, profileName string) (cragSlug, talentName string) {
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
	// Fallback: parse from profile name "belayer-<crag>-<talent>".
	return doctorSplitProfileName(profileName)
}

// doctorSplitProfileName extracts (cragSlug, talentName) from a profile name
// of the form "belayer-<crag>-<talent>". Best-effort single-segment split.
func doctorSplitProfileName(profileName string) (cragSlug, talentName string) {
	rest := strings.TrimPrefix(profileName, "belayer-")
	parts := strings.SplitN(rest, "-", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return rest, ""
}

// buildDoctorIssues returns a human-readable list of health issues.
func buildDoctorIssues(report doctorReport, staleDays int) []string {
	var issues []string

	if !report.Base.Present {
		issues = append(issues, "base profile not created (run `belayer auth ensure`)")
	} else {
		if report.Base.AuthMtime == nil {
			issues = append(issues, "auth.json missing from base profile (run `belayer auth ensure`)")
		} else if report.Base.AuthStale {
			issues = append(issues, fmt.Sprintf("auth.json is %d days old (refresh with `belayer auth ensure` if needed)", report.Base.AuthAgeDays))
		}
		if !report.Base.PluginOK {
			issues = append(issues, "belayer plugin missing from base profile (run `belayer auth ensure`)")
		}
	}

	if report.OrphanCount > 0 {
		issues = append(issues, fmt.Sprintf("%d orphan profile(s) found (run `belayer prune` to remove)", report.OrphanCount))
	}

	if len(report.OrphanAgentRuns) > 0 {
		issues = append(issues, fmt.Sprintf("%d agent_runs row(s) reference missing profile dirs", len(report.OrphanAgentRuns)))
	}

	return issues
}

// printDoctorText writes human-readable output to cmd.OutOrStdout().
func printDoctorText(cmd *cobra.Command, report doctorReport, staleDays int) {
	out := cmd.OutOrStdout()

	// Base profile section.
	fmt.Fprintf(out, "Base profile: %s\n", report.Base.Path)
	if !report.Base.Present {
		fmt.Fprintln(out, "  Status: not created (run `belayer auth ensure`)")
	} else {
		fmt.Fprintln(out, "  Status: present")
		if report.Base.AuthMtime != nil {
			age := time.Since(*report.Base.AuthMtime)
			ageDays := int(age.Hours() / 24)
			line := fmt.Sprintf("  auth.json: %s (modified %d days ago)",
				report.Base.AuthMtime.UTC().Format(time.RFC3339), ageDays)
			if report.Base.AuthStale {
				line += fmt.Sprintf("  [WARN: > %d days]", staleDays)
			}
			fmt.Fprintln(out, line)
		} else {
			fmt.Fprintln(out, "  auth.json: missing (run `belayer auth ensure`)")
		}
		if report.Base.PluginOK {
			fmt.Fprintln(out, "  belayer plugin: installed")
		} else {
			fmt.Fprintln(out, "  belayer plugin: missing (run `belayer auth ensure`)")
		}
	}
	fmt.Fprintln(out)

	// Per-crag sections.
	if len(report.Crags) == 0 && len(report.OrphanAgentRuns) == 0 {
		fmt.Fprintln(out, "No belayer-* profiles found.")
	}

	for _, crag := range report.Crags {
		slug := crag.CragSlug
		if slug == "" {
			slug = "(unknown)"
		}
		fmt.Fprintf(out, "Crag: %s (%d profile(s))\n", slug, len(crag.Profiles))
		for _, p := range crag.Profiles {
			statusLabel := "[active]"
			suffix := ""
			if p.Status == doctorProfileOrphan {
				statusLabel = "[orphan]"
				suffix = fmt.Sprintf("  -- %s", p.Reason)
			}
			fmt.Fprintf(out, "  %-52s %-10s %s%s\n",
				p.ProfileName, statusLabel, humanBytes(p.DiskBytes), suffix)
		}
		fmt.Fprintln(out)
	}

	// Orphan agent_runs rows.
	if len(report.OrphanAgentRuns) > 0 {
		fmt.Fprintln(out, "Orphan agent_runs (profile dir missing):")
		for _, name := range report.OrphanAgentRuns {
			fmt.Fprintf(out, "  %s  -- agent_runs row exists but profile dir gone\n", name)
		}
		fmt.Fprintln(out)
	}

	// Issues summary.
	if len(report.Issues) > 0 {
		fmt.Fprintln(out, "Issues:")
		for _, issue := range report.Issues {
			fmt.Fprintf(out, "  - %s\n", issue)
		}
		fmt.Fprintln(out)
	} else {
		fmt.Fprintln(out, "No issues found.")
	}

	// Total disk usage.
	fmt.Fprintf(out, "Total disk usage: %s across %d profile(s)\n",
		humanBytes(report.TotalDiskBytes), report.TotalProfiles)
}

// printDoctorJSON serialises the report as pretty-printed JSON to cmd.OutOrStdout().
func printDoctorJSON(cmd *cobra.Command, report doctorReport) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

