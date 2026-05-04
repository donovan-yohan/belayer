package cli

import (
	"encoding/json"
	"fmt"
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

type AgentRunView struct {
	store.AgentRun
	PendingMailCount int `json:"pending_mail_count,omitempty"`
	UnackedMailCount int `json:"unacked_mail_count,omitempty"`
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

			// Emit session status as the first line so scripts polling
			// `belayer roster | grep -qE "complete|failed|stalled"` can
			// detect terminal states (e.g. session=failed) without
			// requiring every agent row to carry the same status.
			sess, sessErr := c.GetSession(sessID)
			if sessErr == nil {
				fmt.Fprintf(cmd.OutOrStdout(), "session=%s\n", sess.Status)
			}

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
func rosterStatus(run AgentRunView) string {
	status := run.Status
	outcome := run.Outcome
	if outcome != "" {
		status += "/" + outcome
	}
	if run.DestructiveActions > 0 {
		return status + "⚠"
	}
	return status
}

func (c *Client) ListAgents(sessionID string) ([]AgentRunView, error) {
	resp, err := c.do("GET", "/sessions/"+sessionID+"/agents", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var runs []AgentRunView
	if err := json.NewDecoder(resp.Body).Decode(&runs); err != nil {
		return nil, fmt.Errorf("decode agent runs: %w", err)
	}
	return runs, nil
}
