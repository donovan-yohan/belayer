package cli

import (
	"github.com/spf13/cobra"

	v3cli "github.com/donovan-yohan/belayer/internal/v3/cli"
)

var version = "dev"

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "belayer",
		Short: "Temporal-backed pipeline orchestrator for autonomous coding agents",
		Long: `Belayer orchestrates autonomous coding agents through a declarative pipeline.

Define your pipeline topology in YAML, plug in role implementations
(Claude Code, Codex, or your own tools), and belayer handles execution
via Temporal workflows.

Getting started:
  belayer temporal start     Start the Temporal dev server
  belayer worker             Start the pipeline worker
  belayer start              Open a belayer session (brainstorm + observe)
  belayer attach             Attach to an active worker session
  belayer status             Check pipeline progress`,
	}

	cmd.Version = version

	v3cli.RegisterV3Commands(cmd)

	return cmd
}

func Execute() error {
	return NewRootCmd().Execute()
}
