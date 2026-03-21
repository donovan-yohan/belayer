package outcome

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
)

// verdictFile is the path within workDir where an explicit verdict is written.
const verdictFile = ".belayer/output/verdict.txt"

// Detect determines the CompletionResult for a node execution.
// Precedence:
//  1. .belayer/output/verdict.txt — parse first line for PASS / RETRY target / FAIL
//  2. Output file first line — for file-type nodes, check output.path, parse first line
//  3. Type-based default — file: exists=PASS, missing=FAIL; code: PASS
func Detect(node *pipeline.NodeConfig, workDir string, attempt int) model.CompletionResult {
	// 1. Verdict file
	vpath := filepath.Join(workDir, verdictFile)
	if line, ok := readFirstLine(vpath); ok {
		outcome, target := parseFirstLine(line)
		if outcome.IsValid() {
			return model.CompletionResult{
				Outcome:    outcome,
				TargetNode: target,
				Attempt:    attempt,
			}
		}
	}

	// 2. Output file first line (file-type nodes with a path)
	if node.Output.Type == "file" && node.Output.Path != "" {
		opath := filepath.Join(workDir, node.Output.Path)
		if line, ok := readFirstLine(opath); ok {
			outcome, target := parseFirstLine(line)
			if outcome.IsValid() {
				return model.CompletionResult{
					Outcome:    outcome,
					TargetNode: target,
					OutputPath: opath,
					Attempt:    attempt,
				}
			}
		}
	}

	// 3. Type-based default
	return typeDefault(node, workDir, attempt)
}

// parseFirstLine parses a verdict line and returns (outcome, retryTarget).
// Handles "PASS", "FAIL", "RETRY", "RETRY lead" formats.
func parseFirstLine(line string) (model.NodeOutcome, string) {
	line = strings.TrimSpace(line)
	upper := strings.ToUpper(line)

	if upper == "PASS" {
		return model.OutcomePass, ""
	}
	if upper == "FAIL" {
		return model.OutcomeFail, ""
	}
	if upper == "RETRY" || strings.HasPrefix(upper, "RETRY ") {
		parts := strings.SplitN(line, " ", 2)
		target := ""
		if len(parts) == 2 {
			target = strings.TrimSpace(parts[1])
		}
		return model.OutcomeRetry, target
	}

	return "", ""
}

// typeDefault returns a CompletionResult based purely on node output type.
func typeDefault(node *pipeline.NodeConfig, workDir string, attempt int) model.CompletionResult {
	switch node.Output.Type {
	case "file":
		if node.Output.Path != "" {
			opath := filepath.Join(workDir, node.Output.Path)
			if _, err := os.Stat(opath); err == nil {
				return model.CompletionResult{Outcome: model.OutcomePass, OutputPath: opath, Attempt: attempt}
			}
		}
		return model.CompletionResult{Outcome: model.OutcomeFail, Attempt: attempt}
	default:
		// code and unknown types default to PASS (caller checks commits)
		return model.CompletionResult{Outcome: model.OutcomePass, Attempt: attempt}
	}
}

// readFirstLine opens the file and returns its first non-empty trimmed line.
func readFirstLine(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			return line, true
		}
	}
	return "", false
}
