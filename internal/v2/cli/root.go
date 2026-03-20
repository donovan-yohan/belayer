// Package cli defines the CLI commands for belayer's Temporal-backed orchestrator.
package cli

import (
	"github.com/spf13/cobra"
)

// RegisterCommands adds all belayer commands directly to the given root command.
func RegisterCommands(root *cobra.Command) {
	root.AddCommand(
		newRunCmd(),
		newStatusCmd(),
		newPipelineCmd(),
		newTemporalCmd(),
		newWorkerCmd(),
		newAttachCmd(),
	)

	// Add role signal commands for known roles.
	for _, roleName := range []string{"setter", "explorer", "decomposer", "lead", "spotter", "anchor", "pr-creator", "pr-reviewer", "pr-manager"} {
		root.AddCommand(newRoleCmd(roleName))
	}
}
