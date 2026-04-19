package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

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
	var socketPath, dbPath, belayerRoot, workdir, tcpAddr, dockerGateway string
	var bridgeAPIKey, bridgeBaseURL, bridgeProvider string
	var logLevel string

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Start the belayer daemon",
		Long:  "Starts the long-running belayer supervisor on a Unix socket.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load .belayer.env files into the daemon process environment so
			// bridge subprocesses inherit provider credentials (e.g. OPENCODE_GO_API_KEY).
			// Workspace-scoped file is loaded first so it takes precedence over home.
			wd0 := workdir
			if wd0 == "" {
				if cwd, err := os.Getwd(); err == nil {
					wd0 = cwd
				}
			}
			if wd0 != "" {
				loadEnvFile(filepath.Join(wd0, ".belayer", ".belayer.env"))
			}
			home, _ := os.UserHomeDir()
			loadEnvFile(filepath.Join(home, ".belayer.env"))

			cfg := daemon.DefaultConfig()
			if socketPath != "" {
				cfg.SocketPath = socketPath
			}
			if dbPath != "" {
				cfg.DBPath = dbPath
			}
			if belayerRoot != "" {
				cfg.BelayerRoot = belayerRoot
			}
			if tcpAddr != "" {
				cfg.TCPAddr = tcpAddr
			}
			if dockerGateway != "" {
				cfg.DockerHostGateway = dockerGateway
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

			wd := workdir
			if wd == "" {
				cwd, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("resolve working directory: %w", err)
				}
				wd = cwd
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
			// If the workdir has a .belayer/ directory, also bind a Unix socket
			// there so clamshell bridge subprocesses can reach the daemon via the
			// bind-mounted workspace path (/workspace/.belayer/daemon.sock).
			wsSock := filepath.Join(wd, ".belayer", "daemon.sock")
			if _, err := os.Stat(filepath.Dir(wsSock)); err == nil {
				cfg.WorkspaceSockPath = wsSock
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

	cmd.Flags().StringVar(&socketPath, "socket", "", "Unix socket path (default ~/.belayer/daemon.sock)")
	cmd.Flags().StringVar(&dbPath, "db", "", "SQLite database path (default ~/.belayer/belayer.db)")
	cmd.Flags().StringVar(&belayerRoot, "belayer-root", "", "Path to belayer repo root (for hermes_bridge PYTHONPATH)")
	cmd.Flags().StringVar(&workdir, "workdir", "", "Workspace directory (for .belayer/config.yaml lookup; default cwd)")
	cmd.Flags().StringVar(&tcpAddr, "tcp-addr", "", "Also bind a TCP listener (e.g. 0.0.0.0:7523) for clamshell container access")
	cmd.Flags().StringVar(&tcpAddr, "bind", "", "Alias of --tcp-addr: also bind a TCP listener (e.g. 0.0.0.0:7523)")
	cmd.Flags().StringVar(&dockerGateway, "docker-gateway", "", "Docker host gateway IP for clamshell bridge access (default 172.17.0.1)")
	cmd.Flags().StringVar(&bridgeAPIKey, "bridge-api-key", "", "LLM provider API key injected into bridge subprocesses (for clamshell where sandbox has no Hermes config)")
	cmd.Flags().StringVar(&bridgeBaseURL, "bridge-base-url", "", "LLM provider base URL injected into bridge subprocesses (e.g. https://opencode.ai/zen/go/v1)")
	cmd.Flags().StringVar(&bridgeProvider, "bridge-provider", "", "LLM provider name injected into bridge subprocesses (e.g. openai)")
	cmd.Flags().StringVar(&logLevel, "log-level", "", "Default log level for new sessions (standard|verbose|trace). Overridable per-run via `belayer run start --log-level` or BELAYER_LOG_LEVEL.")
	return cmd
}
