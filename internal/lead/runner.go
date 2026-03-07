package lead

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/donovan-yohan/belayer/internal/model"
)

//go:embed scripts/lead.sh
var leadScriptFS embed.FS

// RunConfig holds configuration for a lead execution run.
type RunConfig struct {
	WorktreePath  string
	LeadID        string
	TaskID        string
	Spec          string
	Goals         []model.GoalSpec
	MaxAttempts   int
	ExecuteModel  string
	ReviewModel   string
}

// RunResult captures the outcome of a lead run.
type RunResult struct {
	Status   model.LeadStatus
	Output   string
	GoalIDs  []string
}

// Runner manages lead execution processes.
type Runner struct {
	store *Store
}

// NewRunner creates a new lead runner.
func NewRunner(store *Store) *Runner {
	return &Runner{store: store}
}

// Run executes the lead loop in the given worktree.
// It extracts the shell script, launches it, monitors stdout for events,
// and updates the database accordingly.
func (r *Runner) Run(ctx context.Context, cfg RunConfig) (*RunResult, error) {
	if err := r.setupLeadDir(cfg); err != nil {
		return nil, fmt.Errorf("setting up .lead directory: %w", err)
	}

	// Mark lead as started
	if err := r.store.SetLeadStarted(cfg.LeadID); err != nil {
		return nil, fmt.Errorf("marking lead started: %w", err)
	}
	if err := r.store.InsertEvent(cfg.TaskID, cfg.LeadID, model.EventLeadStarted, "{}"); err != nil {
		return nil, fmt.Errorf("emitting lead started event: %w", err)
	}

	// Insert lead goals into DB
	goalIDs := make([]string, len(cfg.Goals))
	for i, goal := range cfg.Goals {
		goalID := fmt.Sprintf("%s-goal-%d", cfg.LeadID, goal.Index)
		goalIDs[i] = goalID
		lg := &model.LeadGoal{
			ID:          goalID,
			LeadID:      cfg.LeadID,
			GoalIndex:   goal.Index,
			Description: goal.Description,
			Status:      model.LeadGoalPending,
		}
		if err := r.store.InsertLeadGoal(lg); err != nil {
			return nil, fmt.Errorf("inserting lead goal %d: %w", i, err)
		}
	}

	// Launch the shell script
	result, err := r.executeScript(ctx, cfg, goalIDs)
	if err != nil {
		// Mark lead as failed on process error
		_ = r.store.SetLeadFinished(cfg.LeadID, model.LeadStatusFailed, err.Error())
		_ = r.store.InsertEvent(cfg.TaskID, cfg.LeadID, model.EventLeadFailed, fmt.Sprintf(`{"error":%q}`, err.Error()))
		return &RunResult{Status: model.LeadStatusFailed, Output: err.Error(), GoalIDs: goalIDs}, err
	}

	return result, nil
}

// setupLeadDir creates the .lead/ directory and writes spec, goals, and script.
func (r *Runner) setupLeadDir(cfg RunConfig) error {
	leadDir := filepath.Join(cfg.WorktreePath, ".lead")
	outputDir := filepath.Join(leadDir, "output")

	for _, dir := range []string{leadDir, outputDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	// Write spec
	if err := os.WriteFile(filepath.Join(leadDir, "spec.md"), []byte(cfg.Spec), 0644); err != nil {
		return fmt.Errorf("writing spec: %w", err)
	}

	// Write goals
	goalsJSON, err := json.MarshalIndent(cfg.Goals, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling goals: %w", err)
	}
	if err := os.WriteFile(filepath.Join(leadDir, "goals.json"), goalsJSON, 0644); err != nil {
		return fmt.Errorf("writing goals: %w", err)
	}

	// Extract and write lead script
	scriptContent, err := leadScriptFS.ReadFile("scripts/lead.sh")
	if err != nil {
		return fmt.Errorf("reading embedded lead script: %w", err)
	}
	scriptPath := filepath.Join(leadDir, "lead.sh")
	if err := os.WriteFile(scriptPath, scriptContent, 0755); err != nil {
		return fmt.Errorf("writing lead script: %w", err)
	}

	return nil
}

// executeScript launches the lead shell script and monitors its output.
func (r *Runner) executeScript(ctx context.Context, cfg RunConfig, goalIDs []string) (*RunResult, error) {
	scriptPath := filepath.Join(cfg.WorktreePath, ".lead", "lead.sh")

	cmd := exec.CommandContext(ctx, "bash", scriptPath)
	cmd.Dir = cfg.WorktreePath
	cmd.Env = append(os.Environ(),
		"LEAD_DIR=.lead",
		fmt.Sprintf("MAX_ATTEMPTS=%d", cfg.MaxAttempts),
		fmt.Sprintf("EXECUTE_MODEL=%s", cfg.ExecuteModel),
		fmt.Sprintf("REVIEW_MODEL=%s", cfg.ReviewModel),
	)

	// Capture stdout for event parsing
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	// Capture stderr for logging
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting lead script: %w", err)
	}

	// Drain stderr in background
	stderrOutput := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(stderr)
		stderrOutput <- string(data)
	}()

	// Process events from stdout
	var lastEvent *model.LeadEvent
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		event, parseErr := ParseLeadEvent(line)
		if parseErr != nil {
			continue // Skip non-JSON lines
		}
		lastEvent = event
		r.handleEvent(cfg, event, goalIDs)
	}

	// Wait for process to finish
	waitErr := cmd.Wait()
	stderrText := <-stderrOutput

	// Determine final status
	var finalStatus model.LeadStatus
	var output string

	if waitErr != nil {
		if lastEvent != nil && lastEvent.Type == "stuck" {
			finalStatus = model.LeadStatusStuck
			output = "Lead stuck after exhausting retries"
			_ = r.store.InsertEvent(cfg.TaskID, cfg.LeadID, model.EventLeadStuck, fmt.Sprintf(`{"output":%q}`, output))
		} else {
			finalStatus = model.LeadStatusFailed
			output = fmt.Sprintf("Process exited with error: %v. Stderr: %s", waitErr, strings.TrimSpace(stderrText))
			_ = r.store.InsertEvent(cfg.TaskID, cfg.LeadID, model.EventLeadFailed, fmt.Sprintf(`{"error":%q}`, output))
		}
	} else {
		finalStatus = model.LeadStatusComplete
		output = "All goals completed successfully"
		_ = r.store.InsertEvent(cfg.TaskID, cfg.LeadID, model.EventLeadComplete, `{"output":"all goals passed"}`)
	}

	_ = r.store.SetLeadFinished(cfg.LeadID, finalStatus, output)

	return &RunResult{
		Status:  finalStatus,
		Output:  output,
		GoalIDs: goalIDs,
	}, nil
}

// handleEvent processes a single event from the lead script and updates the DB.
func (r *Runner) handleEvent(cfg RunConfig, event *model.LeadEvent, goalIDs []string) {
	switch event.Type {
	case "started":
		// Already handled in Run()

	case "goal_started":
		if event.Goal >= 0 && event.Goal < len(goalIDs) {
			_ = r.store.SetLeadGoalStarted(goalIDs[event.Goal])
		}

	case "goal_executing", "goal_reviewing":
		if event.Goal >= 0 && event.Goal < len(goalIDs) {
			_ = r.store.UpdateLeadGoalStatus(goalIDs[event.Goal], model.LeadGoalRunning, event.Attempt, "", "")
			_ = r.store.UpdateLeadAttempt(cfg.LeadID, event.Attempt)
			payload := fmt.Sprintf(`{"goal":%d,"attempt":%d,"phase":%q}`, event.Goal, event.Attempt, event.Type)
			_ = r.store.InsertEvent(cfg.TaskID, cfg.LeadID, model.EventLeadProgress, payload)
		}

	case "goal_verdict":
		if event.Goal >= 0 && event.Goal < len(goalIDs) {
			verdictJSON := "{}"
			if event.Pass != nil {
				v := model.Verdict{Pass: *event.Pass, Summary: event.Summary}
				if data, err := json.Marshal(v); err == nil {
					verdictJSON = string(data)
				}
			}
			_ = r.store.UpdateLeadGoalStatus(goalIDs[event.Goal], model.LeadGoalRunning, event.Attempt, "", verdictJSON)

			// Read and store the full verdict.json from disk if available
			verdictPath := filepath.Join(cfg.WorktreePath, ".lead", "verdict.json")
			if data, err := os.ReadFile(verdictPath); err == nil {
				_ = r.store.UpdateLeadGoalStatus(goalIDs[event.Goal], model.LeadGoalRunning, event.Attempt, "", string(data))
			}
		}

	case "goal_complete":
		if event.Goal >= 0 && event.Goal < len(goalIDs) {
			// Read final verdict from disk
			verdictJSON := "{}"
			verdictPath := filepath.Join(cfg.WorktreePath, ".lead", "verdict.json")
			if data, err := os.ReadFile(verdictPath); err == nil {
				verdictJSON = string(data)
			}
			_ = r.store.SetLeadGoalFinished(goalIDs[event.Goal], model.LeadGoalComplete, verdictJSON)
		}

	case "goal_stuck":
		if event.Goal >= 0 && event.Goal < len(goalIDs) {
			_ = r.store.SetLeadGoalFinished(goalIDs[event.Goal], model.LeadGoalStuck, "")
		}

	case "complete":
		// Final status handled in executeScript

	case "stuck":
		// Final status handled in executeScript

	case "error":
		_ = r.store.InsertEvent(cfg.TaskID, cfg.LeadID, model.EventLeadFailed, fmt.Sprintf(`{"error":%q}`, event.Error))
	}
}

// ParseLeadEvent parses a JSON line from the lead script's stdout.
func ParseLeadEvent(line string) (*model.LeadEvent, error) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] != '{' {
		return nil, fmt.Errorf("not a JSON line")
	}

	var event model.LeadEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return nil, fmt.Errorf("parsing event JSON: %w", err)
	}

	if event.Type == "" {
		return nil, fmt.Errorf("event has no type")
	}

	return &event, nil
}

// DefaultRunConfig returns a RunConfig with sensible defaults.
func DefaultRunConfig(worktreePath, leadID, taskID, spec string, goals []model.GoalSpec) RunConfig {
	return RunConfig{
		WorktreePath: worktreePath,
		LeadID:       leadID,
		TaskID:       taskID,
		Spec:         spec,
		Goals:        goals,
		MaxAttempts:  3,
		ExecuteModel: "claude-sonnet-4-6",
		ReviewModel:  "claude-sonnet-4-6",
	}
}

// GracefulShutdown sends SIGTERM to the process and waits for it to exit.
func GracefulShutdown(cmd *exec.Cmd, timeout time.Duration) error {
	if cmd.Process == nil {
		return nil
	}

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("sending SIGTERM: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		return fmt.Errorf("process did not exit within %v, killed", timeout)
	}
}
