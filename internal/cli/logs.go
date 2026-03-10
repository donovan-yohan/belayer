package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/logmgr"
	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View and manage lead session logs",
	}

	cmd.AddCommand(
		newLogsViewCmd(),
		newLogsCleanupCmd(),
		newLogsStatsCmd(),
	)
	return cmd
}

func newLogsViewCmd() *cobra.Command {
	var instanceName string
	var taskID string
	var goalID string

	cmd := &cobra.Command{
		Use:   "view",
		Short: "View log file for a problem or climb",
		RunE: func(cmd *cobra.Command, args []string) error {
			if taskID == "" {
				return fmt.Errorf("--task is required")
			}

			name, err := resolveInstanceName(instanceName)
			if err != nil {
				return err
			}

			_, instanceDir, err := instance.Load(name)
			if err != nil {
				return err
			}

			lm := logmgr.New(filepath.Join(instanceDir, "logs"))

			if goalID != "" {
				logPath := lm.LogPath(taskID, goalID)
				return printFile(cmd, logPath)
			}

			// Show all logs for the task
			taskDir := filepath.Join(instanceDir, "logs", taskID)
			entries, err := os.ReadDir(taskDir)
			if err != nil {
				return fmt.Errorf("reading task log directory: %w", err)
			}

			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "=== %s ===\n", entry.Name())
				if err := printFile(cmd, filepath.Join(taskDir, entry.Name())); err != nil {
					fmt.Fprintf(cmd.OutOrStdout(), "  (error reading: %v)\n", err)
				}
				fmt.Fprintln(cmd.OutOrStdout())
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&instanceName, "instance", "i", "", "Instance name")
	cmd.Flags().StringVar(&taskID, "task", "", "Task ID")
	cmd.Flags().StringVar(&goalID, "goal", "", "Goal ID (optional, shows specific goal log)")
	return cmd
}

func newLogsCleanupCmd() *cobra.Command {
	var instanceName string

	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Clean up old log files",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveInstanceName(instanceName)
			if err != nil {
				return err
			}

			_, instanceDir, err := instance.Load(name)
			if err != nil {
				return err
			}

			lm := logmgr.New(filepath.Join(instanceDir, "logs"))
			if err := lm.Cleanup(); err != nil {
				return fmt.Errorf("cleanup failed: %w", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Log cleanup complete.")
			return nil
		},
	}

	cmd.Flags().StringVarP(&instanceName, "instance", "i", "", "Instance name")
	return cmd
}

func newLogsStatsCmd() *cobra.Command {
	var instanceName string

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show log disk usage statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			name, err := resolveInstanceName(instanceName)
			if err != nil {
				return err
			}

			_, instanceDir, err := instance.Load(name)
			if err != nil {
				return err
			}

			lm := logmgr.New(filepath.Join(instanceDir, "logs"))
			stats, err := lm.Stats()
			if err != nil {
				return fmt.Errorf("getting stats: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Total size: %s\n", formatBytes(stats.TotalSize))
			fmt.Fprintf(cmd.OutOrStdout(), "Total files: %d\n", stats.FileCount)
			if len(stats.TaskSizes) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "\nPer problem:")
				for taskID, size := range stats.TaskSizes {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s: %s\n", taskID, formatBytes(size))
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&instanceName, "instance", "i", "", "Instance name")
	return cmd
}

func printFile(cmd *cobra.Command, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), string(data))
	return nil
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
