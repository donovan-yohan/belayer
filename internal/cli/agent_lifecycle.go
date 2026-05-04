package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/spf13/cobra"
)

type finishAgentRequest struct {
	Summary string `json:"summary"`
	Blocked bool   `json:"blocked"`
}

func resolveAgentID(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if id := os.Getenv("BELAYER_AGENT_ID"); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("BELAYER_AGENT_ID is not set and --agent flag is required")
}

func newFinishCmd() *cobra.Command {
	var session, socket, agent string
	var blocked bool
	cmd := &cobra.Command{
		Use:   "finish " + "\"summary\"",
		Short: "Mark the current agent's work as complete or blocked",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID, err := resolveSessionID(session)
			if err != nil {
				return err
			}
			agentID, err := resolveAgentID(agent)
			if err != nil {
				return err
			}
			c := NewClient(resolveSocket(socket))
			run, err := c.FinishAgent(sessID, agentID, finishAgentRequest{Summary: args[0], Blocked: blocked})
			if err != nil {
				return fmt.Errorf("finish agent: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s marked %s\n", run.Name, run.Status)
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Session ID (required if BELAYER_SESSION_ID not set)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().StringVar(&agent, "agent", "", "Agent ID (required if BELAYER_AGENT_ID not set)")
	cmd.Flags().BoolVar(&blocked, "blocked", false, "Mark the current work as blocked instead of complete")
	return cmd
}

func newRequestCompletionCmd() *cobra.Command {
	var session, socket, agent, specArtifact string
	cmd := &cobra.Command{
		Use:   "request-completion \"summary\"",
		Short: "Signal that all work is complete and request PM verification",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID, err := resolveSessionID(session)
			if err != nil {
				return err
			}
			agentID, err := resolveAgentID(agent)
			if err != nil {
				return err
			}
			c := NewClient(resolveSocket(socket))
			eventData := mustJSON(map[string]string{
				"agent":         agentID,
				"summary":       args[0],
				"spec_artifact": specArtifact,
			})
			if err := c.LogEvent(sessID, "bridge:completion_requested", eventData); err != nil {
				return fmt.Errorf("request completion: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Completion review requested. Acceptance gate agent will be spawned to verify.")
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Session ID (required if BELAYER_SESSION_ID not set)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().StringVar(&agent, "agent", "", "Agent ID (required if BELAYER_AGENT_ID not set)")
	cmd.Flags().StringVar(&specArtifact, "spec", "", "Path to spec artifact for PM to verify against")
	return cmd
}

func (c *Client) FinishAgent(sessionID, agentID string, req finishAgentRequest) (store.AgentRun, error) {
	resp, err := c.do("POST", "/sessions/"+sessionID+"/agents/"+agentID+"/finish", req)
	if err != nil {
		return store.AgentRun{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return store.AgentRun{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var run store.AgentRun
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		return store.AgentRun{}, fmt.Errorf("decode agent run: %w", err)
	}
	return run, nil
}
