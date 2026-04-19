package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"text/tabwriter"

	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/spf13/cobra"
)

func newArtifactCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "artifact", Short: "Create and inspect run artifacts"}
	cmd.AddCommand(newArtifactCreateCmd(), newArtifactListCmd(), newArtifactGetCmd())
	return cmd
}

func newArtifactCreateCmd() *cobra.Command {
	var session, socket, kind, path, producer, summary string
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Register a durable artifact for the current session",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID, err := resolveSessionID(session)
			if err != nil {
				return err
			}
			if kind == "" || path == "" {
				return fmt.Errorf("--kind and --path are required")
			}
			if producer == "" {
				producer = senderID()
			}
			c := NewClient(resolveSocket(socket))
			a, err := c.CreateArtifact(sessID, artifactCreateCLIRequest{Kind: kind, Path: path, Producer: producer, Summary: summary})
			if err != nil {
				return fmt.Errorf("create artifact: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Created artifact %s (%s) by %s\n", a.Kind, a.Path, a.Producer)
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Session ID (required if BELAYER_SESSION_ID not set)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().StringVar(&kind, "kind", "", "Artifact kind")
	cmd.Flags().StringVar(&path, "path", "", "Artifact path relative to run/workdir")
	cmd.Flags().StringVar(&producer, "producer", "", "Agent creating the artifact")
	cmd.Flags().StringVar(&summary, "summary", "", "Short summary")
	return cmd
}

func newArtifactListCmd() *cobra.Command {
	var session, socket string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List artifacts for the current session",
		RunE: func(cmd *cobra.Command, args []string) error {
			sessID, err := resolveSessionID(session)
			if err != nil {
				return err
			}
			c := NewClient(resolveSocket(socket))
			artifacts, err := c.ListArtifacts(sessID)
			if err != nil {
				return fmt.Errorf("list artifacts: %w", err)
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "KIND\tPRODUCER\tPATH\tSUMMARY")
			for _, a := range artifacts {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.Kind, a.Producer, a.Path, a.Summary)
			}
			w.Flush()
			return nil
		},
	}
	cmd.Flags().StringVar(&session, "session", "", "Session ID (required if BELAYER_SESSION_ID not set)")
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	return cmd
}

func newArtifactGetCmd() *cobra.Command {
	var socket, output string
	cmd := &cobra.Command{
		Use:   "get <session> <artifact_id>",
		Short: "Download an artifact's raw bytes",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionArg := args[0]
			artifactID := args[1]

			c := NewClient(resolveSocket(socket))

			// Attempt to resolve session name -> ID, but fall through on error.
			sessionID := sessionArg
			if resolved, err := lookupSessionID(c, sessionArg); err == nil {
				sessionID = resolved
			}

			data, err := c.GetArtifactBytes(sessionID, artifactID)
			if err != nil {
				return fmt.Errorf("get artifact: %w", err)
			}

			if output != "" {
				if err := os.WriteFile(output, data, 0o666); err != nil {
					return fmt.Errorf("write output file: %w", err)
				}
				return nil
			}

			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "Daemon socket path")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Write bytes to this file instead of stdout")
	return cmd
}

type artifactCreateCLIRequest struct {
	Kind     string `json:"kind"`
	Path     string `json:"path"`
	Producer string `json:"producer"`
	Summary  string `json:"summary"`
}

func (c *Client) CreateArtifact(sessionID string, req artifactCreateCLIRequest) (store.Artifact, error) {
	resp, err := c.do("POST", "/sessions/"+sessionID+"/artifacts", req)
	if err != nil {
		return store.Artifact{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		return store.Artifact{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var a store.Artifact
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
		return store.Artifact{}, fmt.Errorf("decode artifact: %w", err)
	}
	return a, nil
}

func (c *Client) ListArtifacts(sessionID string) ([]store.Artifact, error) {
	resp, err := c.do("GET", "/sessions/"+sessionID+"/artifacts", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	var artifacts []store.Artifact
	if err := json.NewDecoder(resp.Body).Decode(&artifacts); err != nil {
		return nil, fmt.Errorf("decode artifacts: %w", err)
	}
	return artifacts, nil
}

// GetArtifactBytes downloads the raw bytes of an artifact from the daemon.
func (c *Client) GetArtifactBytes(sessionID, artifactID string) ([]byte, error) {
	path := "/sessions/" + url.PathEscape(sessionID) + "/artifacts/" + url.PathEscape(artifactID)
	resp, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}
	return io.ReadAll(resp.Body)
}
