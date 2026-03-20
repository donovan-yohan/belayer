package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"

	"github.com/donovan-yohan/belayer/internal/v2/model"
	beltemporal "github.com/donovan-yohan/belayer/internal/v2/temporal"
)

func newRunCmd() *cobra.Command {
	var pipelineFile, fromRole, toRole, inputFile string

	cmd := &cobra.Command{
		Use:   "run [description]",
		Short: "Start a pipeline run (ascent)",
		Long:  "Start a Temporal workflow that executes the pipeline from the DSL file.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			description := strings.Join(args, " ")
			return runPipeline(cmd.Context(), description, pipelineFile, fromRole, toRole, inputFile)
		},
	}

	cmd.Flags().StringVar(&pipelineFile, "pipeline", "", "Pipeline DSL file (default: belayer-pipeline.yaml)")
	cmd.Flags().StringVar(&fromRole, "from", "", "Start from this role (pipeline slicing)")
	cmd.Flags().StringVar(&toRole, "to", "", "Stop after this role")
	cmd.Flags().StringVar(&inputFile, "input", "", "JSON input file for --from role")

	return cmd
}

func runPipeline(ctx context.Context, description, pipelineFile, fromRole, toRole, inputFile string) error {
	c, err := client.Dial(client.Options{})
	if err != nil {
		return fmt.Errorf("cannot connect to Temporal server. Run 'belayer v2 temporal start' first.\n\nError: %w", err)
	}
	defer c.Close()

	input := model.RouteInput{
		Description:  description,
		PipelineFile: pipelineFile,
		FromRole:     fromRole,
		ToRole:       toRole,
	}

	opts := client.StartWorkflowOptions{
		ID:        fmt.Sprintf("belayer-route-%d", time.Now().UnixMilli()),
		TaskQueue: beltemporal.TaskQueueName,
	}

	run, err := c.ExecuteWorkflow(ctx, opts, beltemporal.RouteWorkflow, input)
	if err != nil {
		return fmt.Errorf("failed to start pipeline: %w", err)
	}

	fmt.Printf("Pipeline started!\n")
	fmt.Printf("  Run ID:    %s\n", run.GetRunID())
	fmt.Printf("  Workflow:  %s\n", run.GetID())
	fmt.Printf("  Web UI:    http://localhost:8233/namespaces/default/workflows/%s/%s\n",
		run.GetID(), run.GetRunID())
	fmt.Printf("\nThe pipeline is running. Interactive sessions will prompt you when ready.\n")
	fmt.Printf("Use 'belayer v2 status' to check progress.\n")

	return nil
}
