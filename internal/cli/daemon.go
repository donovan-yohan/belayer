package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/donovan-yohan/belayer/internal/daemon"
	"github.com/donovan-yohan/belayer/internal/runtime"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	var socketPath, dbPath, belayerRoot, workdir string

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Start the belayer daemon",
		Long:  "Starts the long-running belayer supervisor on a Unix socket.",
		RunE: func(cmd *cobra.Command, args []string) error {
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
			if rtCfg.Empty() {
				fmt.Fprintln(cmd.OutOrStdout(), "belayer runtime: noop (no .belayer/config.yaml runtime section found)")
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "belayer runtime: command (loaded from %s/.belayer/config.yaml)\n", wd)
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
	return cmd
}
