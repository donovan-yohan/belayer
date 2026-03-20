package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"

	"github.com/donovan-yohan/belayer/internal/v2/model"
	beltemporal "github.com/donovan-yohan/belayer/internal/v2/temporal"
)

// newRoleCmd creates a command group for a role with finish/flare/fail subcommands.
// Example: belayer setter finish --task-id abc123
func newRoleCmd(roleName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   roleName,
		Short: fmt.Sprintf("Signal commands for the %s role", roleName),
	}

	cmd.AddCommand(
		newSignalCmd(roleName, model.SignalFinish),
		newSignalCmd(roleName, model.SignalFlare),
		newSignalCmd(roleName, model.SignalFail),
	)

	return cmd
}

func newSignalCmd(roleName string, action model.SignalAction) *cobra.Command {
	var taskID, message, outputFile, repo string

	cmd := &cobra.Command{
		Use:   string(action),
		Short: fmt.Sprintf("Signal %s for the %s role", action, roleName),
		RunE: func(cmd *cobra.Command, args []string) error {
			return sendRoleSignal(cmd.Context(), roleName, action, taskID, message, outputFile, repo)
		},
	}

	cmd.Flags().StringVar(&taskID, "task-id", "", "Task ID (workflow ID)")
	cmd.Flags().StringVar(&message, "message", "", "Human-readable context")
	cmd.Flags().StringVar(&repo, "repo", "", "Repo name (for multi-repo pipelines)")
	_ = cmd.MarkFlagRequired("task-id")

	if action == model.SignalFinish {
		cmd.Flags().StringVar(&outputFile, "output", "", "JSON output file path")
	}

	return cmd
}

func sendRoleSignal(ctx context.Context, roleName string, action model.SignalAction, taskID, message, outputFile, repo string) error {
	c, err := client.Dial(client.Options{})
	if err != nil {
		return fmt.Errorf("cannot connect to Temporal: %w", err)
	}
	defer c.Close()

	signal := model.RoleSignal{
		TaskID:  taskID,
		Role:    roleName,
		Repo:    repo,
		Action:  action,
		Message: message,
	}

	// Read output JSON from file if specified.
	if outputFile != "" {
		data, err := os.ReadFile(outputFile)
		if err != nil {
			return fmt.Errorf("read output file: %w", err)
		}
		if !json.Valid(data) {
			return fmt.Errorf("output file is not valid JSON: %s", outputFile)
		}
		signal.Output = data
	}

	err = c.SignalWorkflow(ctx, taskID, "", beltemporal.SignalChannelName, signal)
	if err != nil {
		// Idempotency: if the workflow is already completed/terminated, this is a no-op.
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "completed") || strings.Contains(errStr, "terminated") {
			fmt.Printf("%s %s: workflow already completed (idempotent no-op)\n", roleName, action)
			return nil
		}
		return fmt.Errorf("failed to send %s signal: %w", action, err)
	}

	fmt.Printf("%s %s signal sent for task %s\n", roleName, action, taskID)
	return nil
}
