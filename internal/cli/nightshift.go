package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/spf13/cobra"
)

// ansiEscapeRe matches ANSI terminal escape sequences.
var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// sanitizeCmdForDisplay strips control characters and ANSI escapes from s so it
// is safe to pass to tabwriter without corrupting column alignment.
func sanitizeCmdForDisplay(s string) string {
	s = ansiEscapeRe.ReplaceAllString(s, "")
	s = strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s)
	return s
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

func newRosterCmd() *cobra.Command {
	var session, socket string
	var verbose bool
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
			if verbose {
				fmt.Fprintln(w, "NAME\tROLE\tPROFILE\tSTATUS\tTRANSPORT\tDESTRUCTIVE\tLAST_CMD")
				for _, run := range runs {
					status := rosterStatus(run)
					lastCmd := sanitizeCmdForDisplay(run.LastDestructiveCmd)
					if lastCmd == "" {
						lastCmd = "-"
					}
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
						run.Name, run.Role, run.Profile, status, run.Transport,
						run.DestructiveActions, lastCmd)
				}
			} else {
				fmt.Fprintln(w, "NAME\tROLE\tPROFILE\tSTATUS\tTRANSPORT")
				for _, run := range runs {
					fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
						run.Name, run.Role, run.Profile, rosterStatus(run), run.Transport)
				}
			}
			w.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Session ID (required if BELAYER_SESSION_ID not set)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show destructive action count and last command snippet")
	return cmd
}

// rosterStatus returns the display status string for a roster row.
// When an agent has recorded destructive actions, a warning suffix (⚠) is
// appended so supervisors and PM agents can distinguish a clean completion
// from one that nuked its own workspace first. See Gap 16 in VARIANCE_REPORT.md.
func rosterStatus(run store.AgentRun) string {
	if run.DestructiveActions > 0 {
		return run.Status + "⚠"
	}
	return run.Status
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
	Name     string `json:"name"`
	Identity string `json:"identity,omitempty"` // identity template under .belayer/agents/<identity>/; defaults to Name
	Role     string `json:"role"`
	Profile  string `json:"profile"`
	Repo     string `json:"repo,omitempty"`
	Workdir  string `json:"workdir,omitempty"`
	Branch   string `json:"branch,omitempty"` // git branch for worktree-isolated spawns
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
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
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
			fmt.Fprintln(cmd.OutOrStdout(), "Completion review requested. PM agent will be spawned to verify.")
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Session ID (required if BELAYER_SESSION_ID not set)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().StringVar(&agent, "agent", "", "Agent ID (required if BELAYER_AGENT_ID not set)")
	cmd.Flags().StringVar(&specArtifact, "spec", "", "Path to spec artifact for PM to verify against")
	return cmd
}
