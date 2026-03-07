package cli

import (
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/donovan-yohan/belayer/internal/config"
	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/tui"
	"github.com/spf13/cobra"
)

func newTUICmd() *cobra.Command {
	var instanceName string

	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Launch the TUI dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			if instanceName == "" {
				cfg, err := config.Load()
				if err != nil {
					return fmt.Errorf("loading config: %w", err)
				}
				instanceName = cfg.DefaultInstance
				if instanceName == "" {
					return fmt.Errorf("no default instance set; use --instance flag")
				}
			}

			inst, instanceDir, err := instance.Load(instanceName)
			if err != nil {
				return fmt.Errorf("loading instance %q: %w", instanceName, err)
			}

			dbPath := filepath.Join(instanceDir, "belayer.db")
			database, err := db.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening database: %w", err)
			}
			defer database.Close()

			store := tui.NewStore(database.Conn())
			_ = inst // instance config loaded to verify it exists
			model := tui.NewModel(store, instanceName, instanceName)

			p := tea.NewProgram(model, tea.WithAltScreen())
			if _, err := p.Run(); err != nil {
				return fmt.Errorf("running TUI: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&instanceName, "instance", "", "Instance name (defaults to default instance)")
	return cmd
}
