package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Nightshift run lifecycle commands",
	}
	cmd.AddCommand(newRunStartCmd())
	return cmd
}

func newRunStartCmd() *cobra.Command {
	var socket, name, task, plannerProfile, workdir string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Create a run, spawn planner, and deliver the initial task",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(task) == "" {
				return fmt.Errorf("--task is required")
			}
			if strings.TrimSpace(plannerProfile) == "" {
				return fmt.Errorf("--planner-profile is required")
			}
			c := NewClient(resolveSocket(socket))
			sessionName := name
			if sessionName == "" {
				sessionName = "nightshift-run"
			}
			sess, err := c.CreateSession(sessionName, "nightshift")
			if err != nil {
				return fmt.Errorf("create session: %w", err)
			}
			if _, err := c.UpdateSession(sess.ID, "running"); err != nil {
				return fmt.Errorf("mark session running: %w", err)
			}
			if _, err := c.SpawnAgent(sess.ID, spawnAgentRequest{Name: "planner", Role: "planner", Profile: plannerProfile, Workdir: workdir}); err != nil {
				return fmt.Errorf("spawn planner: %w", err)
			}
			if _, err := c.SendMessage(sess.ID, "planner", task, "instruction", true); err != nil {
				return fmt.Errorf("deliver initial task: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Run started: %s (%s)\n", sess.ID, sess.Name)
			fmt.Fprintln(cmd.OutOrStdout(), "Planner: planner")
			fmt.Fprintln(cmd.OutOrStdout(), "Monitor with:")
			fmt.Fprintf(cmd.OutOrStdout(), "  BELAYER_SESSION_ID=%s belayer roster\n", sess.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "  belayer logs %s\n", sess.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().StringVar(&name, "name", "", "Run/session name")
	cmd.Flags().StringVar(&task, "task", "", "Initial task text for the planner")
	cmd.Flags().StringVar(&plannerProfile, "planner-profile", "", "Hermes profile for the planner")
	cmd.Flags().StringVar(&workdir, "workdir", "", "Working directory for planner and spawned agents")
	return cmd
}
