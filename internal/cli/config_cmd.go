package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/donovan-yohan/belayer/internal/belayerconfig"
	"github.com/donovan-yohan/belayer/internal/config"
	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	var cragName string

	cmd := &cobra.Command{
		Use:   "config",
		Short: "View and modify belayer configuration",
		Long:  "Read and write belayer.toml settings. Uses crag config when --crag is set or BELAYER_CRAG is in the environment; otherwise uses global config.",
	}

	cmd.PersistentFlags().StringVarP(&cragName, "crag", "c", "", "Crag name (default: $BELAYER_CRAG or global)")

	cmd.AddCommand(
		newConfigShowCmd(&cragName),
		newConfigGetCmd(&cragName),
		newConfigSetCmd(&cragName),
	)

	return cmd
}

// resolveConfigDirs returns (globalConfigDir, cragConfigDir) for the resolved crag.
// If no crag is specified, cragConfigDir is empty.
func resolveConfigDirs(cragName string) (string, string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", "", err
	}
	globalCfgDir := filepath.Join(dir, "config")

	// Try to resolve crag
	resolved, _ := resolveCragName(cragName)
	if resolved != "" {
		_, cragDir, err := instance.Load(resolved)
		if err == nil {
			return globalCfgDir, cragDir, nil
		}
	}

	return globalCfgDir, "", nil
}

func newConfigShowCmd(cragName *string) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show resolved configuration",
		Long:  "Displays the fully resolved configuration (embedded defaults + global + crag overrides).",
		RunE: func(cmd *cobra.Command, args []string) error {
			globalDir, cragDir, err := resolveConfigDirs(*cragName)
			if err != nil {
				return err
			}

			cfg, err := belayerconfig.Load(globalDir, cragDir)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			return toml.NewEncoder(cmd.OutOrStdout()).Encode(cfg)
		},
	}
}

func newConfigGetCmd(cragName *string) *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Long:  "Get a resolved config value by dotted key path (e.g. agents.provider, agents.lead_provider).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			globalDir, cragDir, err := resolveConfigDirs(*cragName)
			if err != nil {
				return err
			}

			cfg, err := belayerconfig.Load(globalDir, cragDir)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			val, err := getConfigValue(cfg, args[0])
			if err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), val)
			return nil
		},
	}
}

func newConfigSetCmd(cragName *string) *cobra.Command {
	var global bool

	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Long:  "Set a config value by dotted key path. Writes to crag config by default; use --global for global config.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			globalDir, cragDir, err := resolveConfigDirs(*cragName)
			if err != nil {
				return err
			}

			// Determine target file
			targetDir := cragDir
			if global || cragDir == "" {
				targetDir = globalDir
			}

			tomlPath := filepath.Join(targetDir, "belayer.toml")

			// Load existing file (or empty config)
			var cfg belayerconfig.Config
			data, err := os.ReadFile(tomlPath)
			if err == nil {
				if _, err := toml.Decode(string(data), &cfg); err != nil {
					return fmt.Errorf("parsing %s: %w", tomlPath, err)
				}
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("reading %s: %w", tomlPath, err)
			}

			if err := setConfigValue(&cfg, args[0], args[1]); err != nil {
				return err
			}

			// Ensure directory exists
			if err := os.MkdirAll(targetDir, 0o755); err != nil {
				return fmt.Errorf("creating config dir: %w", err)
			}

			f, err := os.Create(tomlPath)
			if err != nil {
				return fmt.Errorf("creating %s: %w", tomlPath, err)
			}
			defer f.Close()

			if err := toml.NewEncoder(f).Encode(cfg); err != nil {
				return fmt.Errorf("writing %s: %w", tomlPath, err)
			}

			scope := "crag"
			if global || cragDir == "" {
				scope = "global"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Set %s = %q (%s config)\n", args[0], args[1], scope)
			return nil
		},
	}

	cmd.Flags().BoolVar(&global, "global", false, "Write to global config instead of crag config")
	return cmd
}

// getConfigValue reads a dotted key path from a resolved Config.
func getConfigValue(cfg *belayerconfig.Config, key string) (string, error) {
	switch key {
	// agents
	case "agents.provider":
		return cfg.Agents.Provider, nil
	case "agents.lead_provider":
		return cfg.Agents.LeadProvider, nil
	case "agents.spotter_provider":
		return cfg.Agents.SpotterProvider, nil
	case "agents.anchor_provider":
		return cfg.Agents.AnchorProvider, nil
	case "agents.lead_model":
		return cfg.Agents.LeadModel, nil
	case "agents.review_model":
		return cfg.Agents.ReviewModel, nil
	case "agents.permissions":
		return cfg.Agents.Permissions, nil
	// execution
	case "execution.max_leads":
		return fmt.Sprintf("%d", cfg.Execution.MaxLeads), nil
	case "execution.poll_interval":
		return cfg.Execution.PollInterval, nil
	case "execution.stale_timeout":
		return cfg.Execution.StaleTimeout, nil
	case "execution.max_retries":
		return fmt.Sprintf("%d", cfg.Execution.MaxRetries), nil
	// validation
	case "validation.enabled":
		return fmt.Sprintf("%t", cfg.Validation.Enabled), nil
	case "validation.fallback_profile":
		return cfg.Validation.FallbackProfile, nil
	// anchor
	case "anchor.enabled":
		return fmt.Sprintf("%t", cfg.Anchor.Enabled), nil
	case "anchor.max_attempts":
		return fmt.Sprintf("%d", cfg.Anchor.MaxAttempts), nil
	// tracker
	case "tracker.provider":
		return cfg.Tracker.Provider, nil
	case "tracker.label":
		return cfg.Tracker.Label, nil
	// review
	case "review.ci_fix_attempts":
		return fmt.Sprintf("%d", cfg.Review.CIFixAttempts), nil
	case "review.auto_merge":
		return fmt.Sprintf("%t", cfg.Review.AutoMerge), nil
	// pr
	case "pr.draft":
		return fmt.Sprintf("%t", cfg.PR.Draft), nil
	case "pr.stack_threshold":
		return fmt.Sprintf("%d", cfg.PR.StackThreshold), nil
	default:
		return "", fmt.Errorf("unknown config key: %q\nAvailable keys: %s", key, strings.Join(availableKeys(), ", "))
	}
}

// setConfigValue writes a value to a Config struct by dotted key path.
func setConfigValue(cfg *belayerconfig.Config, key, value string) error {
	switch key {
	// agents
	case "agents.provider":
		cfg.Agents.Provider = value
	case "agents.lead_provider":
		cfg.Agents.LeadProvider = value
	case "agents.spotter_provider":
		cfg.Agents.SpotterProvider = value
	case "agents.anchor_provider":
		cfg.Agents.AnchorProvider = value
	case "agents.lead_model":
		cfg.Agents.LeadModel = value
	case "agents.review_model":
		cfg.Agents.ReviewModel = value
	case "agents.permissions":
		cfg.Agents.Permissions = value
	// execution
	case "execution.max_leads":
		var v int
		if _, err := fmt.Sscanf(value, "%d", &v); err != nil {
			return fmt.Errorf("invalid integer for %s: %w", key, err)
		}
		cfg.Execution.MaxLeads = v
	case "execution.poll_interval":
		cfg.Execution.PollInterval = value
	case "execution.stale_timeout":
		cfg.Execution.StaleTimeout = value
	case "execution.max_retries":
		var v int
		if _, err := fmt.Sscanf(value, "%d", &v); err != nil {
			return fmt.Errorf("invalid integer for %s: %w", key, err)
		}
		cfg.Execution.MaxRetries = v
	default:
		return fmt.Errorf("unknown or read-only config key: %q\nSettable keys: agents.provider, agents.lead_provider, agents.spotter_provider, agents.anchor_provider, agents.lead_model, agents.review_model, agents.permissions, execution.max_leads, execution.poll_interval, execution.stale_timeout, execution.max_retries", key)
	}
	return nil
}

func availableKeys() []string {
	return []string{
		"agents.provider", "agents.lead_provider", "agents.spotter_provider", "agents.anchor_provider",
		"agents.lead_model", "agents.review_model", "agents.permissions",
		"execution.max_leads", "execution.poll_interval", "execution.stale_timeout", "execution.max_retries",
		"validation.enabled", "validation.fallback_profile",
		"anchor.enabled", "anchor.max_attempts",
		"tracker.provider", "tracker.label",
		"review.ci_fix_attempts", "review.auto_merge",
		"pr.draft", "pr.stack_threshold",
	}
}
