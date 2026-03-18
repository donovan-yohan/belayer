// Package agentic provides utilities for running ephemeral Claude sessions
// ("agentic nodes") that receive structured input, produce structured output,
// and exit. These are single-shot `claude -p` calls, not interactive sessions.
package agentic

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
)

// NodeConfig configures an agentic node invocation.
type NodeConfig struct {
	Model  string // Claude model to use (e.g. "sonnet")
	Prompt string // The full prompt to send
}

// RunNode executes a single-shot `claude -p` call and returns the raw output.
// The caller is responsible for parsing the output.
func RunNode(ctx context.Context, cfg NodeConfig) (string, error) {
	args := []string{"-p"}
	if cfg.Model != "" {
		args = append(args, "--model", cfg.Model)
	}
	args = append(args, cfg.Prompt)

	cmd := exec.CommandContext(ctx, "claude", args...)

	// Process group isolation: kill the entire process tree on context cancellation.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("agentic node: %w", err)
	}
	return string(out), nil
}

// codeBlockRe matches markdown code fences wrapping JSON output.
var codeBlockRe = regexp.MustCompile("(?s)```(?:json)?\\s*\n?(.*?)\\s*```")

// StripMarkdownJSON extracts JSON from a response that may be wrapped in
// markdown code fences. If no code fences are found, the raw string is returned.
func StripMarkdownJSON(raw string) string {
	matches := codeBlockRe.FindStringSubmatch(raw)
	if len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	return strings.TrimSpace(raw)
}

// RunNodeJSON executes a single-shot `claude -p` call, strips markdown fences,
// and unmarshals the JSON output into the provided target.
func RunNodeJSON(ctx context.Context, cfg NodeConfig, target any) error {
	raw, err := RunNode(ctx, cfg)
	if err != nil {
		return err
	}
	cleaned := StripMarkdownJSON(raw)
	if err := json.Unmarshal([]byte(cleaned), target); err != nil {
		return fmt.Errorf("agentic node: parsing JSON: %w (raw: %.200s)", err, cleaned)
	}
	return nil
}
