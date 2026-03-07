package coordinator

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/donovan-yohan/belayer/internal/model"
)

// AgenticResult holds the output of an agentic node execution.
type AgenticResult struct {
	Raw      string        // Raw stdout from claude
	Duration time.Duration // Execution duration
}

// AgenticNode runs ephemeral claude -p sessions for judgment calls.
type AgenticNode struct {
	store    *Store
	model    string
	nodeType model.AgenticNodeType
}

// NewAgenticNode creates an agentic node executor.
func NewAgenticNode(store *Store, nodeType model.AgenticNodeType, model string) *AgenticNode {
	return &AgenticNode{
		store:    store,
		model:    model,
		nodeType: nodeType,
	}
}

// Execute runs claude -p with the given prompt, stores the result in SQLite, and returns it.
// The prompt is passed directly to claude. The taskID is used for the agentic_decisions record.
func (n *AgenticNode) Execute(ctx context.Context, taskID, prompt string) (*AgenticResult, error) {
	cmd := exec.CommandContext(ctx, "claude", "-p", "--model", n.model, "--output-format", "json", prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	output := stdout.String()

	// Build the decision record — we store it regardless of success or failure.
	decision := &model.AgenticDecision{
		ID:         fmt.Sprintf("ad-%s-%s-%d", taskID, string(n.nodeType), start.UnixNano()),
		TaskID:     taskID,
		NodeType:   n.nodeType,
		Input:      prompt,
		Output:     output,
		Model:      n.model,
		DurationMs: duration.Milliseconds(),
		CreatedAt:  time.Now().UTC(),
	}

	// On command error, include stderr info in the stored output.
	if runErr != nil {
		errOutput := stderr.String()
		decision.Output = fmt.Sprintf("error: %s\nstdout: %s\nstderr: %s", runErr.Error(), output, errOutput)
	}

	// Always attempt to store the decision.
	storeErr := n.store.InsertAgenticDecision(decision)

	// If the command failed, return that error (wrapping any store error too).
	if runErr != nil {
		if storeErr != nil {
			return nil, fmt.Errorf("command failed: %w (also failed to store decision: %v)", runErr, storeErr)
		}
		return nil, fmt.Errorf("command failed: %w", runErr)
	}

	// Command succeeded but store failed — still an error.
	if storeErr != nil {
		return nil, fmt.Errorf("storing agentic decision: %w", storeErr)
	}

	return &AgenticResult{
		Raw:      output,
		Duration: duration,
	}, nil
}
