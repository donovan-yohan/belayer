package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// newToolCmd returns the "belayer tool" command group.
func newToolCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tool",
		Short: "Execute and manage session tools",
		Long: `Execute and manage tools registered to a session.

Tools are shell-backed commands that the daemon routes to the correct
execution target (agent container, workbench, infra, or host). Every
tool call is logged to the session event stream for audit.

Examples:
  belayer tool list --session <id>
  belayer tool run db-query --input '{"query":"SELECT 1"}' --session <id>`,
	}
	cmd.AddCommand(
		newToolRunCmd(),
		newToolListCmd(),
	)
	return cmd
}

// newToolRunCmd returns the "belayer tool run" subcommand.
func newToolRunCmd() *cobra.Command {
	var (
		inputFlag     string
		sessionFlag   string
		agentFlag     string
		socketFlag    string
	)

	cmd := &cobra.Command{
		Use:   "run <tool-name>",
		Short: "Execute a tool in the context of a session",
		Long: `Execute a named tool against its configured execution target.

The tool must be registered for the session (via POST /sessions/{id}/tools).
Input is provided as a JSON object. The exit code of this command mirrors
the tool's exit code so callers can detect failures.

Examples:
  belayer tool run db-query --input '{"query":"SELECT 1"}' --session abc123
  belayer tool run list-files --input '{}' --session abc123 --agent implementer`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			toolName := args[0]
			if sessionFlag == "" {
				sessionFlag = os.Getenv("BELAYER_SESSION_ID")
			}
			if agentFlag == "" {
				agentFlag = os.Getenv("BELAYER_AGENT_ID")
			}
			if sessionFlag == "" {
				return fmt.Errorf("--session is required")
			}

			// Parse input JSON.
			input := map[string]string{}
			if inputFlag != "" {
				if err := json.Unmarshal([]byte(inputFlag), &input); err != nil {
					return fmt.Errorf("--input must be a valid JSON object: %w", err)
				}
			}

			client := NewClient(resolveSocket(socketFlag))
			result, err := client.ExecuteTool(sessionFlag, toolName, input, agentFlag)
			if err != nil {
				return fmt.Errorf("tool run: %w", err)
			}

			// Print output to stdout.
			if result.Output != "" {
				fmt.Fprint(cmd.OutOrStdout(), result.Output)
				// Ensure trailing newline for terminal friendliness.
				if len(result.Output) > 0 && result.Output[len(result.Output)-1] != '\n' {
					fmt.Fprintln(cmd.OutOrStdout())
				}
			}

			// Print stderr to stderr.
			if result.Stderr != "" {
				fmt.Fprint(os.Stderr, result.Stderr)
			}

			// Mirror the tool's exit code.
			if result.ExitCode != 0 {
				os.Exit(result.ExitCode)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&inputFlag, "input", "{}", `JSON object of tool inputs, e.g. '{"key":"value"}'`)
	cmd.Flags().StringVar(&sessionFlag, "session", "", "Session ID (required)")
	cmd.Flags().StringVar(&agentFlag, "agent", "", "Calling agent name (for audit log)")
	cmd.Flags().StringVar(&socketFlag, "socket", "", "Daemon socket path (default: ~/.belayer/daemon.sock)")

	return cmd
}

// newToolListCmd returns the "belayer tool list" subcommand.
func newToolListCmd() *cobra.Command {
	var (
		sessionFlag string
		socketFlag  string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tools registered for a session",
		Long: `List all tools registered for a session, showing name, description, and target.

Examples:
  belayer tool list --session abc123`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionFlag == "" {
				sessionFlag = os.Getenv("BELAYER_SESSION_ID")
			}
			if sessionFlag == "" {
				return fmt.Errorf("--session is required")
			}
			client := NewClient(resolveSocket(socketFlag))
			tools, err := client.ListTools(sessionFlag)
			if err != nil {
				return fmt.Errorf("tool list: %w", err)
			}

			if len(tools) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No tools registered for this session.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tTARGET\tDESCRIPTION")
			fmt.Fprintln(w, "----\t------\t-----------")
			for _, t := range tools {
				desc := t.Description
				if desc == "" {
					desc = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", t.Name, t.Exec.Target, desc)
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&sessionFlag, "session", "", "Session ID (required)")
	cmd.Flags().StringVar(&socketFlag, "socket", "", "Daemon socket path (default: ~/.belayer/daemon.sock)")

	return cmd
}
