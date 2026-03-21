package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/outcome"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
	"github.com/spf13/cobra"
)

// NewNodeCompleteCmd returns the `belayer node-complete` cobra command.
func NewNodeCompleteCmd() *cobra.Command {
	var taskID, nodeName string
	var attempt int

	cmd := &cobra.Command{
		Use:   "node-complete",
		Short: "Record completion of a pipeline node (Stop hook handler)",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Fall back to env vars when flags are not provided.
			if taskID == "" {
				taskID = os.Getenv("BELAYER_TASK_ID")
			}
			if nodeName == "" {
				nodeName = os.Getenv("BELAYER_NODE")
			}
			if !cmd.Flags().Changed("attempt") {
				if v := os.Getenv("BELAYER_ATTEMPT"); v != "" {
					if n, err := strconv.Atoi(v); err == nil {
						attempt = n
					}
				}
			}

			if taskID == "" {
				return fmt.Errorf("--task-id or BELAYER_TASK_ID is required")
			}
			if nodeName == "" {
				return fmt.Errorf("--node or BELAYER_NODE is required")
			}

			workDir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}

			node := resolveNodeConfig(workDir, nodeName)
			result := outcome.Detect(node, workDir, attempt)

			if err := writeCompletionFile(workDir, taskID, nodeName, result); err != nil {
				return fmt.Errorf("write completion file: %w", err)
			}

			fmt.Printf("node-complete: task=%s node=%s attempt=%d outcome=%s\n",
				taskID, nodeName, attempt, result.Outcome)
			return nil
		},
	}

	cmd.Flags().StringVar(&taskID, "task-id", "", "Task ID (env: BELAYER_TASK_ID)")
	cmd.Flags().StringVar(&nodeName, "node", "", "Node name (env: BELAYER_NODE)")
	cmd.Flags().IntVar(&attempt, "attempt", 0, "Attempt number (env: BELAYER_ATTEMPT)")

	return cmd
}

// resolveNodeConfig finds the NodeConfig for nodeName by checking pipeline files
// in order. Falls back to a generic file node if nothing is found.
func resolveNodeConfig(workDir, nodeName string) *pipeline.NodeConfig {
	candidates := []string{
		filepath.Join(workDir, "belayer-pipeline.yaml"),
		filepath.Join(workDir, ".belayer", "pipeline.yaml"),
	}

	for _, path := range candidates {
		cfg, err := pipeline.ParsePipelineFile(path)
		if err != nil {
			continue
		}
		if n := cfg.FindNode(nodeName); n != nil {
			return n
		}
	}

	// Try the built-in default pipeline.
	if cfg, err := pipeline.ParsePipeline([]byte(pipeline.DefaultPipelineYAML)); err == nil {
		if n := cfg.FindNode(nodeName); n != nil {
			return n
		}
	}

	// Generic fallback: treat as a file node with no specific path.
	return &pipeline.NodeConfig{
		Name:   nodeName,
		Output: pipeline.OutputConfig{Type: "file"},
	}
}

// completionFilePath returns the path for an attempt-scoped completion file.
func completionFilePath(workDir, taskID, nodeName string, attempt int) string {
	filename := fmt.Sprintf("%s-%s-attempt-%d.json", taskID, nodeName, attempt)
	return filepath.Join(workDir, ".belayer", "completion", filename)
}

// writeCompletionFile marshals result as JSON and writes it to the
// attempt-scoped completion file path.
func writeCompletionFile(workDir, taskID, nodeName string, result model.CompletionResult) error {
	path := completionFilePath(workDir, taskID, nodeName, result.Attempt)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create completion dir: %w", err)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal completion result: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write completion file: %w", err)
	}
	return nil
}
