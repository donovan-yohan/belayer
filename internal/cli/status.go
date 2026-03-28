package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "List ClimbWorkflow pipeline runs",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.Dial(client.Options{})
			if err != nil {
				return fmt.Errorf("connect to Temporal: %w", err)
			}
			defer c.Close()

			resp, err := c.ListWorkflow(cmd.Context(), &workflowservice.ListWorkflowExecutionsRequest{
				Namespace: "default",
				Query:     `WorkflowType = "ClimbWorkflow"`,
			})
			if err != nil {
				return fmt.Errorf("list workflows: %w", err)
			}

			if len(resp.Executions) == 0 {
				fmt.Println("No pipeline runs found.")
				return nil
			}

			fmt.Printf("%-40s  %-20s  %s\n", "WORKFLOW ID", "STATUS", "STARTED")
			fmt.Printf("%-40s  %-20s  %s\n", "---", "---", "---")
			for _, wf := range resp.Executions {
				id := wf.GetExecution().GetWorkflowId()
				status := wf.GetStatus().String()
				started := wf.GetStartTime().AsTime().Format("2006-01-02 15:04:05")
				fmt.Printf("%-40s  %-20s  %s\n", id, status, started)
			}
			return nil
		},
	}
}
