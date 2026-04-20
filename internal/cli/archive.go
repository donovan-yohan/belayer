package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/donovan-yohan/belayer/internal/archive"
	"github.com/spf13/cobra"
)

// terminalStatuses is the set of valid terminal status values from LOG_FORMAT.md §5.
var terminalStatuses = map[string]bool{
	"complete":           true,
	"blocked":            true,
	"failed":             true,
	"cancelled":          true,
	"needs_human_review": true,
	"stalled":            true,
}

func newArchiveCmd() *cobra.Command {
	var output, socket string

	cmd := &cobra.Command{
		Use:   "archive <session-id>",
		Short: "Archive a session's events and metadata to disk",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := NewClient(resolveSocket(socket))

			sessionID, err := lookupSessionID(c, args[0])
			if err != nil {
				return fmt.Errorf("archive: %w", err)
			}

			sess, err := c.GetSession(sessionID)
			if err != nil {
				return fmt.Errorf("archive: get session: %w", err)
			}

			destDir, err := resolveOutputDir(output, sess.WorkspaceDir, sessionID)
			if err != nil {
				return err
			}

			health, err := c.Health()
			if err != nil {
				return fmt.Errorf("archive: cannot fetch daemon epoch from /health: %w (is the daemon running?)", err)
			}
			if health.DaemonInstanceID == "" {
				return fmt.Errorf("archive: daemon returned empty daemon_instance_id from /health")
			}
			daemonInstanceID := health.DaemonInstanceID

			isTerminal := terminalStatuses[sess.Status]
			if !isTerminal {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: session status %q is not a terminal value; archiving anyway\n",
					sess.Status)
			}

			rawEvents, err := c.GetEvents(sessionID)
			if err != nil {
				return fmt.Errorf("archive: get events: %w", err)
			}
			// TODO(large-sessions): paginate via GetEventsAfter for sessions with
			// many events once the daemon enforces a per-call cap.

			agents, err := c.ListAgents(sessionID)
			if err != nil {
				return fmt.Errorf("archive: list agents: %w", err)
			}

			roster := make([]archive.AgentInfo, len(agents))
			for i, a := range agents {
				roster[i] = archive.AgentInfo{Name: a.Name, Role: a.Role, Profile: a.Profile}
			}

			events := make([]archive.Event, len(rawEvents))
			for i, e := range rawEvents {
				events[i] = archive.Event{
					ID:        e.ID,
					SessionID: e.SessionID,
					Timestamp: e.Timestamp,
					Type:      e.Type,
					Data:      json.RawMessage(e.Data),
				}
			}

			artifacts, skipped := archive.ExtractArtifacts(events)
			if skipped > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: %d artifact_created event(s) had unparseable data; omitted from manifest.artifacts\n",
					skipped)
			}

			meta := archive.Meta{
				SchemaVersion:    "belayer-log/v1",
				DaemonInstanceID: daemonInstanceID,
				Session: archive.SessionMeta{
					ID:        sess.ID,
					Name:      sess.Name,
					Workspace: sess.WorkspaceDir,
					LogLevel:  sess.LogLevel,
				},
				AgentRoster: roster,
				Artifacts:   artifacts,
				FinalStatus: sess.Status,
				Partial:     !isTerminal,
				ArchivedAt:  time.Now().UTC(),
			}

			result, err := archive.Write(destDir, meta, events)
			if err != nil {
				return fmt.Errorf("archive: write: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(),
				"archived session %s -> %s (event_count=%d)\n",
				sessionID, destDir, result.EventCount)
			return nil
		},
	}

	cmd.Flags().StringVar(&output, "output", "", "Output directory (default: <workspace>/.belayer/archive/<session-id>/)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
}

// resolveOutputDir returns the destination directory for the archive.
func resolveOutputDir(outputFlag, workspaceDir, sessionID string) (string, error) {
	if outputFlag != "" {
		return outputFlag, nil
	}
	if workspaceDir == "" {
		return "", fmt.Errorf("session has no workspace directory; pass --output to specify a destination")
	}
	return filepath.Join(workspaceDir, ".belayer", "archive", sessionID), nil
}

