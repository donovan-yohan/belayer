package cli

import (
	"encoding/json"
	"fmt"

	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/spf13/cobra"
)

type spawnAgentRequest struct {
	Name     string `json:"name"`
	Identity string `json:"identity,omitempty"` // identity template under .belayer/agents/<identity>/; defaults to Name
	Role     string `json:"role"`
	Kind     string `json:"kind,omitempty"`
	Profile  string `json:"profile"`
	Repo     string `json:"repo,omitempty"`
	Workdir  string `json:"workdir,omitempty"`
	Branch   string `json:"branch,omitempty"` // git branch for worktree-isolated spawns
}

func newSpawnCmd() *cobra.Command {
	var session, socket, name, identity, role, profile, repo, workdir, branch string
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
			run, err := c.SpawnAgent(sessID, spawnAgentRequest{
				Name:     name,
				Identity: identity,
				Role:     role,
				Profile:  profile,
				Repo:     repo,
				Workdir:  workdir,
				Branch:   branch,
			})
			if err != nil {
				return fmt.Errorf("spawn agent: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Spawned %s (%s) via %s\n", run.Name, run.Profile, run.Transport)
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Session ID (required if BELAYER_SESSION_ID not set)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().StringVar(&name, "name", "", "Session-local agent name (e.g. reviewer-1)")
	cmd.Flags().StringVar(&identity, "identity", "", "Identity template under .belayer/agents/<identity>/ (defaults to --name)")
	cmd.Flags().StringVar(&role, "role", "", "Role description/id")
	cmd.Flags().StringVar(&profile, "profile", "", "Hermes runtime profile to launch (separate from --identity)")
	cmd.Flags().StringVar(&repo, "repo", "", "Repo scope label")
	cmd.Flags().StringVar(&workdir, "workdir", "", "Working directory for the agent")
	cmd.Flags().StringVar(&branch, "branch", "", "Git branch for worktree isolation (implementer-style spawns)")
	return cmd
}

func (c *Client) SpawnAgent(sessionID string, req spawnAgentRequest) (store.AgentRun, error) {
	resp, err := c.do("POST", "/sessions/"+sessionID+"/agents", req)
	if err != nil {
		return store.AgentRun{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		var payload struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err == nil && payload.Error != "" {
			if payload.Code != "" {
				return store.AgentRun{}, fmt.Errorf("%s: %s", payload.Code, payload.Error)
			}
			return store.AgentRun{}, fmt.Errorf("%s", payload.Error)
		}
		return store.AgentRun{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var run store.AgentRun
	if err := json.NewDecoder(resp.Body).Decode(&run); err != nil {
		return store.AgentRun{}, fmt.Errorf("decode agent run: %w", err)
	}
	return run, nil
}
