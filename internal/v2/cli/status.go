package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"
	"go.temporal.io/api/workflowservice/v1"

	beltemporal "github.com/donovan-yohan/belayer/internal/v2/temporal"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show active pipeline runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return showStatus(cmd.Context())
		},
	}
}

func showStatus(ctx context.Context) error {
	c, err := client.Dial(client.Options{})
	if err != nil {
		return fmt.Errorf("cannot connect to Temporal. Run 'belayer v2 temporal start' first.\n\nError: %w", err)
	}
	defer c.Close()

	query := fmt.Sprintf("TaskQueue = '%s'", beltemporal.TaskQueueName)
	resp, err := c.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
		Query: query,
	})
	if err != nil {
		return fmt.Errorf("list workflows: %w", err)
	}

	if len(resp.Executions) == 0 {
		fmt.Println("No active pipeline runs.")
		fmt.Println("Start one with: belayer v2 run \"your description\"")
		return nil
	}

	for _, exec := range resp.Executions {
		status := exec.GetStatus().String()
		runID := exec.Execution.RunId
		if len(runID) > 8 {
			runID = runID[:8]
		}
		fmt.Printf("  %s  %s  (%s)\n", exec.Execution.WorkflowId, status, runID)
	}
	fmt.Printf("\n%d pipeline run(s) found.\n", len(resp.Executions))
	fmt.Println("Web UI: http://localhost:8233")

	return nil
}
