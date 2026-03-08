package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/donovan-yohan/belayer/internal/belayerconfig"
	"github.com/donovan-yohan/belayer/internal/config"
	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/lead"
	"github.com/donovan-yohan/belayer/internal/setter"
	"github.com/donovan-yohan/belayer/internal/tmux"
	"github.com/spf13/cobra"
)

func newSetterCmd() *cobra.Command {
	var instanceName string
	var maxLeads int
	var pollInterval time.Duration
	var staleTimeout time.Duration

	cmd := &cobra.Command{
		Use:   "setter",
		Short: "Start the setter daemon for an instance",
		Long:  "Starts a long-running daemon that polls SQLite for pending tasks, builds goal DAGs, spawns tmux sessions for leads, and manages task lifecycle.",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveInstanceName(instanceName)
			if err != nil {
				return err
			}

			_, instanceDir, err := instance.Load(name)
			if err != nil {
				return err
			}

			dbPath := filepath.Join(instanceDir, "belayer.db")
			database, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer database.Close()

			if err := database.Migrate(); err != nil {
				return fmt.Errorf("running migrations: %w", err)
			}

			// Load belayer config
			globalDir, err := config.Dir()
			if err != nil {
				return fmt.Errorf("getting global config dir: %w", err)
			}
			globalCfgDir := filepath.Join(globalDir, "config")
			instanceCfgDir := filepath.Join(instanceDir, "config")

			bcfg, err := belayerconfig.Load(globalCfgDir, instanceCfgDir)
			if err != nil {
				return fmt.Errorf("loading belayer config: %w", err)
			}

			cfg := setter.Config{
				InstanceName: name,
				InstanceDir:  instanceDir,
				MaxLeads:     maxLeads,
				PollInterval: pollInterval,
				StaleTimeout: staleTimeout,
			}

			tm := tmux.NewRealTmux()
			sp := lead.NewClaudeSpawner(tm)
			s := setter.New(cfg, bcfg, globalCfgDir, instanceCfgDir, database.Conn(), tm, sp)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				fmt.Println("\nSetter shutting down...")
				cancel()
			}()

			return s.Run(ctx)
		},
	}

	cmd.Flags().StringVarP(&instanceName, "instance", "i", "", "Instance name (defaults to default instance)")
	cmd.Flags().IntVar(&maxLeads, "max-leads", 8, "Maximum concurrent lead sessions")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 5*time.Second, "Polling interval for new tasks")
	cmd.Flags().DurationVar(&staleTimeout, "stale-timeout", 30*time.Minute, "Timeout for stale goal detection")

	return cmd
}
