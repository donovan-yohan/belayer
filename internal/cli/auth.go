package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

// belayerProfileName is the canonical Hermes profile name that holds shared
// auth, plugin registration, and skill catalog for all per-talent forks.
// Per-talent profiles in Phase 2 will be named belayer-<crag>-<instance>
// and symlink auth.json + plugins/ + skills/ from this base profile.
const belayerProfileName = "belayer"

// hermesProfileDirs mirrors hermes_cli/profiles.py#_PROFILE_DIRS so a profile
// scaffolded by belayer matches one created by `hermes profile create` in shape.
// Subprocess HOME isolation (the `home/` subdir) is intentional — see Hermes
// hermes_constants.get_subprocess_home() for details.
var hermesProfileDirs = []string{
	"memories",
	"sessions",
	"skills",
	"skins",
	"logs",
	"plans",
	"workspace",
	"cron",
	"home",
}

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage the base belayer Hermes profile (auth, plugin install)",
		Long: `Manage the base belayer Hermes profile.

The base profile lives at ~/.hermes/profiles/belayer/ and holds shared auth
credentials, the belayer Hermes plugin, and skill catalog. Per-talent forks
created during a climb symlink auth.json + plugins/ + skills/ from this base,
so a single 'belayer auth' login propagates to every spawned talent.`,
	}
	cmd.AddCommand(newAuthEnsureCmd(), newAuthStatusCmd())
	return cmd
}

func newAuthEnsureCmd() *cobra.Command {
	var skipLogin bool
	cmd := &cobra.Command{
		Use:   "ensure",
		Short: "Scaffold ~/.hermes/profiles/belayer/, install belayer plugin, run hermes auth login",
		Long: `Idempotent setup of the base belayer profile.

Creates the profile directory tree (matching Hermes's profile schema), extracts
the embedded belayer plugin into <profile>/plugins/belayer/, ensures the plugin
is enabled in <profile>/config.yaml, and finally runs 'hermes auth login' with
HERMES_HOME pointed at the profile.

Safe to re-run; existing files are preserved and only stale plugin files are
pruned. Use --skip-login for headless setup or test environments.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			profileDir, err := belayerProfileDir()
			if err != nil {
				return err
			}
			created, err := scaffoldBelayerProfile(profileDir)
			if err != nil {
				return fmt.Errorf("scaffold profile: %w", err)
			}
			if created {
				fmt.Fprintf(cmd.OutOrStdout(), "Created belayer profile at %s\n", profileDir)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Belayer profile present at %s\n", profileDir)
			}
			if err := extractPluginsToHermesHome(profileDir); err != nil {
				return fmt.Errorf("install belayer plugin: %w", err)
			}
			if changed, err := ensureHermesPluginEnabled(profileDir, "belayer"); err != nil {
				return fmt.Errorf("enable belayer plugin: %w", err)
			} else if changed {
				fmt.Fprintln(cmd.OutOrStdout(), "Enabled belayer plugin in profile config.yaml")
			}
			if skipLogin {
				fmt.Fprintln(cmd.OutOrStdout(), "Skipped 'hermes auth login' (--skip-login)")
				return nil
			}
			return runHermesAuthLogin(profileDir, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().BoolVar(&skipLogin, "skip-login", false, "Skip running 'hermes auth login' (for headless setup or tests)")
	return cmd
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report base belayer profile state (path, auth.json mtime, plugin presence)",
		RunE: func(cmd *cobra.Command, args []string) error {
			profileDir, err := belayerProfileDir()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Profile: %s\n", profileDir)
			info, err := os.Stat(profileDir)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintln(out, "  Status: not created (run `belayer auth ensure`)")
					return nil
				}
				return fmt.Errorf("stat profile: %w", err)
			}
			if !info.IsDir() {
				return fmt.Errorf("profile path %q exists but is not a directory", profileDir)
			}
			fmt.Fprintln(out, "  Status: present")

			authPath := filepath.Join(profileDir, "auth.json")
			if authInfo, err := os.Stat(authPath); err == nil {
				age := time.Since(authInfo.ModTime()).Round(time.Second)
				fmt.Fprintf(out, "  auth.json: %s (modified %s ago)\n", authInfo.ModTime().Format(time.RFC3339), age)
			} else if os.IsNotExist(err) {
				fmt.Fprintln(out, "  auth.json: missing (run `hermes auth login` with HERMES_HOME=" + profileDir + ")")
			} else {
				return fmt.Errorf("stat auth.json: %w", err)
			}

			pluginPath := filepath.Join(profileDir, "plugins", "belayer", "plugin.yaml")
			if _, err := os.Stat(pluginPath); err == nil {
				fmt.Fprintln(out, "  belayer plugin: installed")
			} else {
				fmt.Fprintln(out, "  belayer plugin: missing (run `belayer auth ensure`)")
			}
			return nil
		},
	}
}

// belayerProfileDir returns the absolute path to the base belayer Hermes
// profile. Honours HERMES_HOME's parent layout: when HERMES_HOME is set we
// place profiles under HERMES_HOME/../profiles/belayer if HERMES_HOME ends
// in /profiles/<name>, otherwise HERMES_HOME/profiles/belayer.
//
// Today's only caller is the auth command, which always wants the canonical
// home location. We keep the env-aware logic so test fixtures can redirect
// the whole tree by setting HERMES_HOME=<tmpdir>.
func belayerProfileDir() (string, error) {
	if env := os.Getenv("HERMES_HOME"); env != "" {
		// If HERMES_HOME already points at a profile dir, the parent profiles/
		// is the canonical location. Detect by checking if parent is named
		// "profiles". Otherwise treat HERMES_HOME as the root and nest profiles
		// underneath.
		parent := filepath.Base(filepath.Dir(env))
		if parent == "profiles" {
			return filepath.Join(filepath.Dir(env), belayerProfileName), nil
		}
		return filepath.Join(env, "profiles", belayerProfileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".hermes", "profiles", belayerProfileName), nil
}

// scaffoldBelayerProfile creates the profile dir and the standard Hermes
// subdirectory layout (matching hermes_cli/profiles.py#_PROFILE_DIRS).
// Returns (createdNewProfile, error). Idempotent: re-running on an existing
// profile is a no-op.
func scaffoldBelayerProfile(profileDir string) (bool, error) {
	created := false
	if _, err := os.Stat(profileDir); errors.Is(err, os.ErrNotExist) {
		created = true
	} else if err != nil {
		return false, fmt.Errorf("stat profile dir: %w", err)
	}
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return false, fmt.Errorf("mkdir profile root: %w", err)
	}
	for _, sub := range hermesProfileDirs {
		if err := os.MkdirAll(filepath.Join(profileDir, sub), 0o755); err != nil {
			return false, fmt.Errorf("mkdir profile %s: %w", sub, err)
		}
	}
	return created, nil
}

// runHermesAuthLogin invokes `hermes auth login` against the given profile
// dir. The hermes binary must be on PATH; if missing we print an actionable
// error directing the operator to install hermes-agent first.
//
// Stdin/stdout/stderr are wired through so OAuth device-flow URLs and any
// interactive prompts surface to the user's terminal as if they ran
// `hermes auth login` themselves.
func runHermesAuthLogin(profileDir string, stdout, stderr io.Writer) error {
	bin, err := exec.LookPath("hermes")
	if err != nil {
		return fmt.Errorf("'hermes' binary not found on PATH; install hermes-agent first (https://github.com/NousResearch/hermes-agent): %w", err)
	}
	cmd := exec.Command(bin, "auth", "login")
	cmd.Env = append(os.Environ(), "HERMES_HOME="+profileDir)
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hermes auth login failed: %w", err)
	}
	return nil
}
