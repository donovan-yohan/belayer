package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var version = "dev"

// ChannelsFooter is appended to the Long help of observability-facing commands
// (logs, bridges, daemon) so operators see the three-channel model from any
// entry point. Kept verbatim in tests — do not rewrap.
const ChannelsFooter = `
Channels:
  events       — SessionEvent rows (id, type, data). Stream via /events/stream (SSE)
                 or query via /sessions/{id}/events. Filters: ?agent, ?type_prefix,
                 ?tier, ?since=<reader_id>, ?digest=0. Compact TSV via
                 Accept: text/tab-separated-values.
  transcripts  — per-agent Hermes transcripts at
                 .belayer/climbs/<session>/transcripts/<agent>.jsonl (verbose+).
                 HTTP: /sessions/{id}/transcripts[/{agent}].
  traces       — per-agent spill fragments at traces/<session>/<agent>/NNNN.jsonl[.zst]
                 (trace tier). HTTP: /sessions/{id}/trace/{agent}/{fragment} with
                 Range support. See docs/LOG_FORMAT.md and docs/OBSERVABILITY.md.
`

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "belayer",
		Short:        "Belayer — agent control plane for Nightshift",
		SilenceUsage: true,
		Long: `Belayer v7 — climb-local agent control plane for Nightshift.

Coordinates supervisor + specialist agents via the Hermes bridge within a single
worker climb. Daemon manages sessions, agent roster, messages, events, and artifacts
over SQLite.`,
	}

	cmd.Version = version
	cmd.AddCommand(
		newVersionCmd(),
		newDaemonCmd(),
		newDashboardCmd(),
		newSessionCmd(),
		newLogsCmd(),
		newStatusCmd(),
		newRecallCmd(),
		newSpawnCmd(),
		newFinishCmd(),
		newRosterCmd(),
		newMessageCmd(),
		newRequestCompletionCmd(),
		newRunCmd(),
		newArtifactCmd(),
		newTeamCmd(),
		newCragCmd(),
		newInitCmd(),
		newAuthCmd(),
		newDoctorCmd(),
		newPruneCmd(),
		newUninstallCmd(),
		newArchiveCmd(),
		newBridgesCmd(),
	)
	return cmd
}

func Execute() error {
	return NewRootCmd().Execute()
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the belayer build version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version)
		},
	}
}
