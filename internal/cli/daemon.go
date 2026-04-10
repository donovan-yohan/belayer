package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/donovan-yohan/belayer/internal/daemon"
	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	var socketPath, dbPath string

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
	return cmd
}
