package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newSubmitCmd() *cobra.Command {
	var fileFlag, promptFlag string

	cmd := &cobra.Command{
		Use:   "submit [description]",
		Short: "Submit a design doc or prompt to the pipeline",
		Long: `Submit work to the belayer pipeline for autonomous execution.

The worker must be running (belayer worker). This command sends the
spec to the worker's HTTP API which starts a Climb workflow.

Examples:
  belayer submit --file design.md          # submit a design doc
  belayer submit --prompt "Add auth"       # submit a text prompt
  belayer submit "Implement feature X"     # positional args`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var spec string
			var designFile string

			if fileFlag != "" {
				data, err := os.ReadFile(fileFlag)
				if err != nil {
					return fmt.Errorf("read file %q: %w", fileFlag, err)
				}
				spec = strings.TrimSpace(string(data))
				designFile = fileFlag
			} else if promptFlag != "" {
				spec = promptFlag
			} else if len(args) > 0 {
				spec = strings.Join(args, " ")
			} else {
				return fmt.Errorf("provide --file, --prompt, or positional args")
			}

			if spec == "" {
				return fmt.Errorf("spec is empty")
			}

			payload := map[string]interface{}{
				"spec":   spec,
				"source": "submit",
			}
			if designFile != "" {
				payload["metadata"] = map[string]string{"design_file": designFile}
			}
			body, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("marshal request: %w", err)
			}

			httpClient := &http.Client{Timeout: 5 * time.Second}
			resp, err := httpClient.Post(
				fmt.Sprintf("http://127.0.0.1:%d/start", workerHTTPPort),
				"application/json",
				bytes.NewReader(body),
			)
			if err != nil {
				return fmt.Errorf("belayer worker is not running. Start it with: belayer worker")
			}
			defer resp.Body.Close()

			respBody, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("worker error (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
			}

			var result map[string]string
			if err := json.Unmarshal(respBody, &result); err != nil {
				fmt.Printf("Pipeline started. Response: %s\n", respBody)
				return nil
			}

			fmt.Printf("Pipeline started: workflow=%s\n", result["workflow_id"])
			return nil
		},
	}

	cmd.Flags().StringVar(&fileFlag, "file", "", "Design doc file path")
	cmd.Flags().StringVar(&promptFlag, "prompt", "", "Text prompt as pipeline input")

	return cmd
}
