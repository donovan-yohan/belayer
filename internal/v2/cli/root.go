// Package cli defines the v2 CLI commands for belayer's Temporal-backed orchestrator.
package cli

import (
	"github.com/spf13/cobra"
)

// NewV2Cmd returns the v2 command group, added to the existing root.
// v2 commands use new names that don't conflict with v1.
func NewV2Cmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "v2",
		Short: "Belayer v2 — Temporal-backed orchestrator (preview)",
		Long:  "Preview commands for belayer v2. These use Temporal for durable pipeline execution.",
	}

	cmd.AddCommand(
		newRunCmd(),
		newStatusCmd(),
		newPipelineCmd(),
		newTemporalCmd(),
		newWorkerCmd(),
	)

	// Add role signal commands for known roles.
	for _, roleName := range []string{"setter", "explorer", "decomposer", "lead", "spotter", "anchor", "pr-creator", "pr-reviewer", "pr-manager"} {
		cmd.AddCommand(newRoleCmd(roleName))
	}

	return cmd
}
