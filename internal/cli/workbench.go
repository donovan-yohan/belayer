package cli

import (
	"fmt"
	"os"
	"sort"
	"strings"

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
				sessionID = os.Getenv("BELAYER_SESSION_ID")
			}
			if sessionID == "" {
				return fmt.Errorf("--session is required")
			}
			c := NewClient(resolveSocket(socket))
			wb, err := c.CreateWorkbench(sessionID, "{}")
			if err != nil {
				return fmt.Errorf("workbench up: %w", err)
			}
			printWorkbench(cmd, wb, true)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID (required)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
}

func newWorkbenchStatusCmd() *cobra.Command {
	var sessionID, socket string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check workbench readiness and endpoints",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionID == "" {
				sessionID = os.Getenv("BELAYER_SESSION_ID")
			}
			if sessionID == "" {
				return fmt.Errorf("--session is required")
			}
			c := NewClient(resolveSocket(socket))
			wb, err := c.GetWorkbenchStatus(sessionID)
			if err != nil {
				return fmt.Errorf("workbench status: %w", err)
			}
			printWorkbench(cmd, wb, false)
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID (required)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
}

func printWorkbench(cmd *cobra.Command, wb workbenchResponse, includeID bool) {
	out := cmd.OutOrStdout()
	if includeID {
		fmt.Fprintf(out, "Workbench: %s\n", wb.ID)
	}
	fmt.Fprintf(out, "Status: %s\n", wb.Status)
	if len(wb.Endpoints) == 0 {
		fmt.Fprintln(out, "Endpoints: none")
	} else {
		fmt.Fprintln(out, "Endpoints:")
		names := make([]string, 0, len(wb.Endpoints))
		for name := range wb.Endpoints {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(out, "  %s: %s\n", name, wb.Endpoints[name])
		}
	}
	if len(wb.Services) == 0 {
		return
	}
	fmt.Fprintln(out, "Services:")
	for _, service := range wb.Services {
		status := strings.TrimSpace(service.State)
		health := strings.TrimSpace(service.Health)
		if health != "" && !strings.EqualFold(health, "none") {
			if status != "" {
				status += "/"
			}
			status += health
		}
		if status == "" {
			status = "unknown"
		}
		fmt.Fprintf(out, "  %s: %s\n", service.Name, status)
	}
}

func newWorkbenchDownCmd() *cobra.Command {
	var sessionID, socket string

	cmd := &cobra.Command{
		Use:   "down",
		Short: "Tear down the workbench",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionID == "" {
				sessionID = os.Getenv("BELAYER_SESSION_ID")
			}
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
	return cmd
}
