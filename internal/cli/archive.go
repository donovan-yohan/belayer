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

			// TODO(phase-5): populate DaemonInstanceID from GET /health once the
			// health extension lands; leave empty for now.
			daemonInstanceID := ""

			if !terminalStatuses[sess.Status] {
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

			artifacts, skipped := extractArtifacts(events)
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
				},
				AgentRoster: roster,
				Artifacts:   artifacts,
				FinalStatus: sess.Status,
				Partial:     false,
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

// extractArtifacts scans events for artifact_created events and returns the
// ArtifactInfo list along with a count of artifact_created events that had
// unparseable data (silently dropped would hide belayer bugs — the caller surfaces
// this as a warning so cragd and operators can see the mismatch between
// artifact_created events in the NDJSON and artifacts in the manifest).
func extractArtifacts(events []archive.Event) (arts []archive.ArtifactInfo, skipped int) {
	for _, e := range events {
		if e.Type != "artifact_created" {
			continue
		}
		var payload struct {
			Kind string `json:"kind"`
			Path string `json:"path"`
		}
		// e.Data may be a JSON string (HTTP shape) or an object. Try both.
		raw := e.Data
		if len(raw) > 0 && raw[0] == '"' {
			var s string
			if err := json.Unmarshal(raw, &s); err == nil {
				raw = json.RawMessage(s)
			}
		}
		if err := json.Unmarshal(raw, &payload); err != nil || payload.Kind == "" {
			skipped++
			continue
		}
		arts = append(arts, archive.ArtifactInfo{
			ID:   fmt.Sprintf("%d", e.ID),
			Kind: payload.Kind,
			Path: payload.Path,
		})
	}
	return arts, skipped
}
