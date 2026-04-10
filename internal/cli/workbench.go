package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newWorkbenchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workbench",
		Short: "Manage the workbench for a session",
	}
	cmd.AddCommand(
		newWorkbenchUpCmd(),
		newWorkbenchStatusCmd(),
		newWorkbenchDownCmd(),
	)
	return cmd
}

func newWorkbenchUpCmd() *cobra.Command {
	var sessionID, socket string

	cmd := &cobra.Command{
		Use:   "up",
		Short: "Provision the workbench for the current session",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionID == "" {
				return fmt.Errorf("--session is required")
			}
			c := NewClient(resolveSocket(socket))
			wb, err := c.CreateWorkbench(sessionID, "{}")
			if err != nil {
				return fmt.Errorf("workbench up: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Workbench provisioned: %s (status: %s)\n", wb.ID, wb.Status)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID (required)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	_ = cmd.MarkFlagRequired("session")
	return cmd
}

func newWorkbenchStatusCmd() *cobra.Command {
	var sessionID, socket string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check workbench readiness and endpoints",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionID == "" {
				return fmt.Errorf("--session is required")
			}
			c := NewClient(resolveSocket(socket))
			wb, err := c.GetWorkbenchStatus(sessionID)
			if err != nil {
				return fmt.Errorf("workbench status: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Status:    %s\n", wb.Status)
			fmt.Fprintf(cmd.OutOrStdout(), "Endpoints: %s\n", wb.Endpoints)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID (required)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	_ = cmd.MarkFlagRequired("session")
	return cmd
}

func newWorkbenchDownCmd() *cobra.Command {
	var sessionID, socket string

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Tear down the workbench",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionID == "" {
				return fmt.Errorf("--session is required")
			}
			c := NewClient(resolveSocket(socket))
			if err := c.DeleteWorkbenchBySession(sessionID); err != nil {
				return fmt.Errorf("workbench down: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Workbench torn down for session %s\n", sessionID)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID (required)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	_ = cmd.MarkFlagRequired("session")
	return cmd
}
