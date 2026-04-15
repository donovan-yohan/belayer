package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/spf13/cobra"
)

func resolveAgentID(flagVal string) (string, error) {
	if id := os.Getenv("BELAYER_AGENT_ID"); id != "" {
		return id, nil
	}
	if flagVal != "" {
		return flagVal, nil
	}
	return "", fmt.Errorf("BELAYER_AGENT_ID is not set and --agent flag is required")
}

func newSpawnCmd() *cobra.Command {
	var session, socket, name, role, profile, repo, workdir string
	cmd := &cobra.Command{
		Use:   "spawn",
		Short: "Spawn a new agent in the current session",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID, err := resolveSessionID(session)
			if err != nil {
				return err
			}
			if name == "" || profile == "" {
				return fmt.Errorf("--name and --profile are required")
			}
			c := NewClient(resolveSocket(socket))
			run, err := c.SpawnAgent(sessID, spawnAgentRequest{Name: name, Role: role, Profile: profile, Repo: repo, Workdir: workdir})
			if err != nil {
				return fmt.Errorf("spawn agent: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Spawned %s (%s) via %s\n", run.Name, run.Profile, run.Transport)
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Session ID (required if BELAYER_SESSION_ID not set)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().StringVar(&name, "name", "", "Logical agent name")
	cmd.Flags().StringVar(&role, "role", "", "Role description/id")
	cmd.Flags().StringVar(&profile, "profile", "", "Hermes profile to launch")
	cmd.Flags().StringVar(&repo, "repo", "", "Repo scope label")
	cmd.Flags().StringVar(&workdir, "workdir", "", "Working directory for the agent")
	return cmd
}

func newRosterCmd() *cobra.Command {
	var session, socket string
	cmd := &cobra.Command{
		Use:   "roster",
		Short: "List active agents in the current session",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID, err := resolveSessionID(session)
			if err != nil {
				return err
			}
			c := NewClient(resolveSocket(socket))
			runs, err := c.ListAgents(sessID)
			if err != nil {
				return fmt.Errorf("roster: %w", err)
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tROLE\tPROFILE\tSTATUS\tTRANSPORT")
			for _, run := range runs {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", run.Name, run.Role, run.Profile, run.Status, run.Transport)
			}
			w.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Session ID (required if BELAYER_SESSION_ID not set)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
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

type spawnAgentRequest struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	Profile string `json:"profile"`
	Repo    string `json:"repo,omitempty"`
	Workdir string `json:"workdir,omitempty"`
}

type finishAgentRequest struct {
	Summary string `json:"summary"`
	Blocked bool   `json:"blocked"`
}

func (c *Client) SpawnAgent(sessionID string, req spawnAgentRequest) (store.AgentRun, error) {
	resp, err := c.do("POST", "/sessions/"+sessionID+"/agents", req)
	if err != nil {
		return store.AgentRun{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		return store.AgentRun{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var run store.AgentRun
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		return store.AgentRun{}, fmt.Errorf("decode agent run: %w", err)
	}
	return run, nil
}

func (c *Client) ListAgents(sessionID string) ([]store.AgentRun, error) {
	resp, err := c.do("GET", "/sessions/"+sessionID+"/agents", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var runs []store.AgentRun
	if err := json.NewDecoder(resp.Body).Decode(&runs); err != nil {
		return nil, fmt.Errorf("decode agent runs: %w", err)
	}
	return runs, nil
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
