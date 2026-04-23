package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/donovan-yohan/belayer/internal/bridge"
	"github.com/donovan-yohan/belayer/internal/daemon"
	"github.com/donovan-yohan/belayer/internal/runtime"
	"github.com/donovan-yohan/belayer/internal/sandbox"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

// loadEnvFile reads KEY=VALUE lines from path into the current process
// environment without overwriting already-set vars (workspace file wins over
// home file because workspace is loaded first).
func loadEnvFile(path string) {
	if _, err := os.Stat(path); err != nil {
		return
	}
	_ = godotenv.Load(path) // godotenv.Load skips keys already in os.Environ
}

func newDaemonCmd() *cobra.Command {
	var socketPath, dbPath, belayerRoot, workdir, tcpAddr string
	var bridgeAPIKey, bridgeBaseURL, bridgeProvider string
	var logLevel, authToken string
	var corsOrigins []string
	var confineAgentWrites bool

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Start the belayer daemon",
		Long:  "Starts the long-running belayer supervisor on a Unix socket." + ChannelsFooter,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Resolve working directory first — drives all defaults.
			wd := workdir
			if wd == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("resolve working directory: %w", err)
				}
				wd = cwd
			}

			// Load .belayer.env files into the daemon process environment so
			// bridge subprocesses inherit provider credentials (e.g. OPENCODE_GO_API_KEY).
			// Workspace-scoped file is loaded first so it takes precedence over home.
			loadEnvFile(filepath.Join(wd, ".belayer", ".belayer.env"))
			home, _ := os.UserHomeDir()
			loadEnvFile(filepath.Join(home, ".belayer.env"))

			cfg := daemon.DefaultConfig()
			if socketPath != "" {
				cfg.SocketPath = socketPath
			} else {
				cfg.SocketPath = filepath.Join(wd, ".belayer", "daemon.sock")
			}
			if dbPath != "" {
				cfg.DBPath = dbPath
			} else {
				cfg.DBPath = filepath.Join(wd, ".belayer", "belayer.db")
			}
			if belayerRoot != "" {
				cfg.BelayerRoot = belayerRoot
			}

			// Resolve the runtime dir and extract the embedded hermes_bridge package.
			// This must happen before daemon.New so bridge subprocesses spawned
			// immediately after Start have the package available.
			runtimeDir, err := resolveRuntimeDir(wd)
			if err != nil {
				return fmt.Errorf("resolve runtime dir: %w", err)
			}
			if err := extractBridgeToRuntimeDir(runtimeDir, wd); err != nil {
				return fmt.Errorf("extract bridge to runtime dir: %w", err)
			}
			cfg.RuntimeDir = runtimeDir
			fmt.Fprintf(cmd.OutOrStdout(), "belayer runtime dir: %s\n", runtimeDir)

			if tcpAddr != "" {
				cfg.TCPAddr = tcpAddr
			}
			if bridgeAPIKey != "" {
				cfg.BridgeAPIKey = bridgeAPIKey
			}
			if bridgeBaseURL != "" {
				cfg.BridgeBaseURL = bridgeBaseURL
			}
			if bridgeProvider != "" {
				cfg.BridgeProvider = bridgeProvider
			}
			if logLevel != "" {
				cfg.DefaultLogLevel = logLevel
			}
			if authToken != "" {
				cfg.AuthToken = authToken
			}
			if len(corsOrigins) > 0 {
				cfg.CORSOrigins = corsOrigins
			}
			if confineAgentWrites {
				cfg.ConfineAgentWrites = true
			}

			rtCfg, err := runtime.LoadConfig(wd)
			if err != nil {
				return err
			}
			cfg.Runtime = runtime.NewFromConfig(rtCfg)
			// Drivers register themselves from init(); hand the daemon the
			// process-wide registry so per-session sandbox.mode can resolve.
			cfg.SandboxDrivers = sandbox.Default
			if rtCfg.Empty() {
				fmt.Fprintln(cmd.OutOrStdout(), "belayer runtime: noop (no .belayer/config.yaml runtime section found)")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "belayer runtime: command (loaded from %s/.belayer/config.yaml)\n", wd)
			}
			// Load bridge config (skip_openrouter_probe etc.) from .belayer/config.yaml.
			// Default: SkipOpenRouterProbe=true — suppresses the openrouter metadata
			// probe that causes proxy-denied CONNECTs on sandboxed deployments.
			bridgeCfg, err := bridge.LoadProjectConfig(wd)
			if err != nil {
				return fmt.Errorf("load bridge config: %w", err)
			}
			cfg.SkipOpenRouterProbe = bridgeCfg.SkipOpenRouterProbe
			if caps, err := daemon.LoadRuntimeCaps(wd); err != nil {
				return fmt.Errorf("load runtime caps: %w", err)
			} else {
				cfg.MaxConcurrentAgents = caps.MaxConcurrentAgents
				cfg.MaxConcurrentMains = caps.MaxConcurrentMains
				cfg.MaxConcurrentSides = caps.MaxConcurrentSides
				cfg.MaxSideSummonsPerSession = caps.MaxSideSummonsPerSession
			}

			// If the primary socket is not already inside the workspace, also bind
			// a workspace-local Unix socket so sandboxed bridge subprocesses can
			// reach the daemon via the bind-mounted path.
			wsSock := filepath.Join(wd, ".belayer", "daemon.sock")
			if cfg.SocketPath != wsSock {
				if _, err := os.Stat(filepath.Dir(wsSock)); err == nil {
					cfg.WorkspaceSockPath = wsSock
				}
			}

			d, err := daemon.New(cfg)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			fmt.Fprintf(cmd.OutOrStdout(), "belayer daemon listening on %s\n", cfg.SocketPath)
			return d.Start(ctx)
		},
	}

	cmd.Flags().StringVar(&socketPath, "socket", "", "Unix socket path (default <workdir>/.belayer/daemon.sock)")
	cmd.Flags().StringVar(&dbPath, "db", "", "SQLite database path (default <workdir>/.belayer/belayer.db)")
	cmd.Flags().StringVar(&belayerRoot, "belayer-root", "", "Path to belayer repo root (for hermes_bridge PYTHONPATH)")
	cmd.Flags().StringVar(&workdir, "workdir", "", "Workspace directory (for .belayer/config.yaml lookup; default cwd)")
	cmd.Flags().StringVar(&tcpAddr, "tcp-addr", "", "Also bind a TCP listener (e.g. 0.0.0.0:7523) for sandboxed container access")
	cmd.Flags().StringVar(&tcpAddr, "bind", "", "Alias of --tcp-addr: also bind a TCP listener (e.g. 0.0.0.0:7523)")
	cmd.Flags().StringVar(&bridgeAPIKey, "bridge-api-key", "", "LLM provider API key injected into bridge subprocesses (for sandboxes with no Hermes config on the container user)")
	cmd.Flags().StringVar(&bridgeBaseURL, "bridge-base-url", "", "LLM provider base URL injected into bridge subprocesses (e.g. https://opencode.ai/zen/go/v1)")
	cmd.Flags().StringVar(&bridgeProvider, "bridge-provider", "", "LLM provider name injected into bridge subprocesses (e.g. openai)")
	cmd.Flags().StringVar(&logLevel, "log-level", "", "Default log level for new sessions (standard|verbose|trace). Overridable per-run via `belayer run start --log-level` or BELAYER_LOG_LEVEL.")
	cmd.Flags().StringVar(&authToken, "auth-token", "", "Bearer token required for TCP listener requests (auto-generated if --tcp-addr/--bind is set and this flag is not)")
	cmd.Flags().StringSliceVar(&corsOrigins, "cors-origin", nil, "Allowed CORS origin (repeatable, e.g. --cors-origin https://app.example)")
	cmd.Flags().BoolVar(&confineAgentWrites, "confine-agent-writes", false, "Enable kernel-enforced write confinement via Landlock v2 for agent bridge subprocesses (Linux 5.19+ only; degrades gracefully on older kernels)")
	return cmd
}
