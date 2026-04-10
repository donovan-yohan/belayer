package cli

import "github.com/spf13/cobra"

var version = "dev"

func NewRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "belayer",
		Short: "Pipeline orchestrator for autonomous coding agents",
		Long: `Belayer orchestrates autonomous coding agents through declarative YAML pipelines.

Three pipeline primitives:
  Nodes     Constructive steps that produce artifacts (code, specs, PRs)
  Gates     Adversarial quality checks with multi-dimensional scoring
  Routers   Agentic N-way branching — LLM picks a path, runs as child workflow

Define your pipeline in YAML, install a framework (belayer setup --framework),
and belayer handles execution, scoring, routing, and retries via Temporal.

Getting started:
  belayer setup --framework gstack        Install a framework
  belayer climb "description"             Start a pipeline run
  belayer worker                          Start the worker daemon
  belayer submit "description"            Submit to a running worker
  belayer status                          Check pipeline progress

See docs/PIPELINE_REFERENCE.md for the full YAML schema.`,
	}

	cmd.Version = version

	cmd.AddCommand(
		NewClimbCmd(),
		NewNodeCompleteCmd(),
		newStatusCmd(),
		newWorkerCmd(),
		newStartCmd(),
		newSetupCmd(),
		newSubmitCmd(),
		newLogsCmd(),
		newToolCmd(),
	)

	return cmd
}

func Execute() error {
	return NewRootCmd().Execute()
}
