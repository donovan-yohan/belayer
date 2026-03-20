package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"

	"github.com/donovan-yohan/belayer/internal/v2/model"
	"github.com/donovan-yohan/belayer/internal/v2/pipeline"
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

	// Parse the pipeline DSL.
	route, source, err := findAndParsePipeline(pipelineFile)
	if err != nil {
		return fmt.Errorf("pipeline: %w", err)
	}
	fmt.Printf("Using pipeline: %s (%s)\n", route.Name, source)

	routeJSON, err := json.Marshal(route)
	if err != nil {
		return fmt.Errorf("serialize pipeline: %w", err)
	}

	input := model.RouteInput{
		Description:  description,
		PipelineFile: pipelineFile,
		RouteJSON:    routeJSON,
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

	fmt.Printf("\nPipeline started!\n")
	fmt.Printf("  Run ID:    %s\n", run.GetRunID())
	fmt.Printf("  Workflow:  %s\n", run.GetID())
	fmt.Printf("  Web UI:    http://localhost:8233/namespaces/default/workflows/%s/%s\n",
		run.GetID(), run.GetRunID())
	fmt.Printf("\nThe pipeline is running. Interactive sessions will prompt you when ready.\n")
	fmt.Printf("Use 'belayer v2 status' to check progress.\n")

	return nil
}

// findAndParsePipeline locates and parses the pipeline YAML file.
// Returns the parsed Route, a description of the source, and any error.
func findAndParsePipeline(pipelineFlag string) (*pipeline.Route, string, error) {
	// Explicit flag takes priority.
	if pipelineFlag != "" {
		route, err := pipeline.ParseRouteFile(pipelineFlag)
		if err != nil {
			return nil, "", err
		}
		if err := pipeline.ValidateOrError(route); err != nil {
			return nil, "", err
		}
		return route, pipelineFlag, nil
	}

	// Look for belayer-pipeline.yaml in CWD.
	const defaultFile = "belayer-pipeline.yaml"
	if _, err := os.Stat(defaultFile); err == nil {
		route, err := pipeline.ParseRouteFile(defaultFile)
		if err != nil {
			return nil, "", err
		}
		if err := pipeline.ValidateOrError(route); err != nil {
			return nil, "", err
		}
		return route, defaultFile, nil
	}

	// Fall back to embedded default.
	route, err := pipeline.ParseRoute([]byte(pipeline.DefaultPipelineYAML))
	if err != nil {
		return nil, "", fmt.Errorf("parse default pipeline: %w", err)
	}
	return route, "built-in default (solo)", nil
}
