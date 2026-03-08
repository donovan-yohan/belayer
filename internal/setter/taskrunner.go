package setter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/lead"
	"github.com/donovan-yohan/belayer/internal/logmgr"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/spotter"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/donovan-yohan/belayer/internal/tmux"
)

// DoneJSON is the structured output a lead writes to DONE.json.
type DoneJSON struct {
	Status       string   `json:"status"`
	Summary      string   `json:"summary"`
	FilesChanged []string `json:"files_changed"`
	Notes        string   `json:"notes"`
}

// QueuedGoal is a goal waiting to be spawned.
type QueuedGoal struct {
	Goal   model.Goal
	TaskID string
}

// GitRunner abstracts git command execution for testability.
type GitRunner interface {
	Run(workdir string, args ...string) (string, error)
}

// RealGitRunner runs git commands by shelling out.
type RealGitRunner struct{}

func (r *RealGitRunner) Run(workdir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// TaskRunner manages the lifecycle of a single task.
type TaskRunner struct {
	task        *model.Task
	dag         *DAG
	worktrees   map[string]string // repoName -> worktreePath
	tmuxSession string
	instanceDir string
	store       *store.Store
	tmux        tmux.TmuxManager
	logMgr      *logmgr.LogManager
	spawner     lead.AgentSpawner
	git         GitRunner
	startedAt   map[string]time.Time // goalID -> when it started running

	// Spotter state
	spotterAttempt int
	spotterRunning bool
	taskDir        string // directory for VERDICT.json
}

// NewTaskRunner creates a TaskRunner for the given task.
func NewTaskRunner(task *model.Task, instanceDir string, s *store.Store, tm tmux.TmuxManager, lm *logmgr.LogManager, sp lead.AgentSpawner) *TaskRunner {
	return &TaskRunner{
		task:        task,
		worktrees:   make(map[string]string),
		instanceDir: instanceDir,
		store:       s,
		tmux:        tm,
		logMgr:      lm,
		spawner:     sp,
		git:         &RealGitRunner{},
		startedAt:   make(map[string]time.Time),
	}
}

// Init initializes the task: creates worktrees, tmux session, and builds the DAG.
// Returns ready goals that should be queued for spawning.
func (tr *TaskRunner) Init() ([]QueuedGoal, error) {
	// Update task status to running
	if err := tr.store.UpdateTaskStatus(tr.task.ID, model.TaskStatusRunning); err != nil {
		return nil, fmt.Errorf("updating task status: %w", err)
	}
	tr.task.Status = model.TaskStatusRunning

	// Parse goals from the task
	goals, err := tr.store.GetGoalsForTask(tr.task.ID)
	if err != nil {
		return nil, fmt.Errorf("getting goals: %w", err)
	}

	// Build DAG
	tr.dag = BuildDAG(goals)

	// Get unique repos
	repos := make(map[string]bool)
	for _, g := range goals {
		repos[g.RepoName] = true
	}

	// Create worktrees
	for repoName := range repos {
		worktreePath, err := instance.CreateWorktree(tr.instanceDir, tr.task.ID, repoName)
		if err != nil {
			return nil, fmt.Errorf("creating worktree for %s: %w", repoName, err)
		}
		tr.worktrees[repoName] = worktreePath
	}

	// Set task directory for VERDICT.json
	tr.taskDir = filepath.Join(tr.instanceDir, "tasks", tr.task.ID)
	os.MkdirAll(tr.taskDir, 0o755)

	// Create tmux session
	tr.tmuxSession = fmt.Sprintf("belayer-task-%s", tr.task.ID)
	if !tr.tmux.HasSession(tr.tmuxSession) {
		if err := tr.tmux.NewSession(tr.tmuxSession); err != nil {
			return nil, fmt.Errorf("creating tmux session: %w", err)
		}
	}

	// Ensure log directory exists
	if err := tr.logMgr.EnsureDir(tr.task.ID); err != nil {
		return nil, fmt.Errorf("ensuring log dir: %w", err)
	}

	// Find ready goals
	readyGoals := tr.dag.ReadyGoals()
	var queued []QueuedGoal
	for _, g := range readyGoals {
		queued = append(queued, QueuedGoal{Goal: g, TaskID: tr.task.ID})
	}

	return queued, nil
}

// SpawnGoal creates a tmux window for a goal and launches an agent session.
func (tr *TaskRunner) SpawnGoal(goal model.Goal) error {
	// Guard: don't spawn if the goal is already running in the DAG
	if dagGoal := tr.dag.Get(goal.ID); dagGoal != nil && dagGoal.Status == model.GoalStatusRunning {
		return nil
	}

	windowName := fmt.Sprintf("%s-%s", goal.RepoName, goal.ID)

	// Create tmux window
	if err := tr.tmux.NewWindow(tr.tmuxSession, windowName); err != nil {
		return fmt.Errorf("creating window %s: %w", windowName, err)
	}

	// Enable pipe-pane logging
	logPath := tr.logMgr.LogPath(tr.task.ID, goal.ID)
	if err := tr.tmux.PipePane(tr.tmuxSession, windowName, logPath); err != nil {
		log.Printf("warning: pipe-pane for %s failed: %v", windowName, err)
	}

	// Build prompt and spawn agent
	worktreePath := tr.worktrees[goal.RepoName]
	prompt, err := lead.BuildPrompt(lead.PromptData{
		Spec:        tr.task.Spec,
		GoalID:      goal.ID,
		RepoName:    goal.RepoName,
		Description: goal.Description,
	})
	if err != nil {
		return fmt.Errorf("building prompt for %s: %w", goal.ID, err)
	}

	if err := tr.spawner.Spawn(context.Background(), lead.SpawnOpts{
		TmuxSession: tr.tmuxSession,
		WindowName:  windowName,
		WorkDir:     worktreePath,
		Prompt:      prompt,
	}); err != nil {
		return fmt.Errorf("spawning agent for %s: %w", goal.ID, err)
	}

	// Update status in DAG and SQLite
	tr.dag.MarkRunning(goal.ID)
	if err := tr.store.UpdateGoalStatus(goal.ID, model.GoalStatusRunning); err != nil {
		return fmt.Errorf("updating goal status: %w", err)
	}

	if err := tr.store.InsertEvent(tr.task.ID, goal.ID, model.EventGoalStarted, "{}"); err != nil {
		return fmt.Errorf("inserting goal_started event: %w", err)
	}

	tr.startedAt[goal.ID] = time.Now()

	return nil
}

// CheckCompletions scans worktrees for DONE.json files and returns newly unblocked goals
// and the number of goals that completed this tick.
func (tr *TaskRunner) CheckCompletions() (newlyReady []QueuedGoal, completedCount int, err error) {
	for _, g := range tr.dag.Goals() {
		if g.Status != model.GoalStatusRunning {
			continue
		}

		worktreePath := tr.worktrees[g.RepoName]
		donePath := filepath.Join(worktreePath, "DONE.json")

		data, readErr := os.ReadFile(donePath)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				continue
			}
			return nil, 0, fmt.Errorf("reading DONE.json for %s: %w", g.ID, readErr)
		}

		// Parse DONE.json
		var done DoneJSON
		if jsonErr := json.Unmarshal(data, &done); jsonErr != nil {
			log.Printf("warning: invalid DONE.json for goal %s: %v", g.ID, jsonErr)
			continue
		}

		// Mark complete in DAG and SQLite
		tr.dag.MarkComplete(g.ID)
		if storeErr := tr.store.UpdateGoalStatus(g.ID, model.GoalStatusComplete); storeErr != nil {
			return nil, 0, fmt.Errorf("updating goal status: %w", storeErr)
		}

		payload, _ := json.Marshal(done)
		if eventErr := tr.store.InsertEvent(tr.task.ID, g.ID, model.EventGoalCompleted, string(payload)); eventErr != nil {
			return nil, 0, fmt.Errorf("inserting goal_completed event: %w", eventErr)
		}

		delete(tr.startedAt, g.ID)
		completedCount++

		// Kill the tmux window
		windowName := fmt.Sprintf("%s-%s", g.RepoName, g.ID)
		tr.tmux.KillWindow(tr.tmuxSession, windowName)

		// Check log rotation
		tr.logMgr.CheckRotation(tr.task.ID, g.ID)
	}

	// Only check for newly unblocked goals if something actually completed
	if completedCount > 0 {
		readyGoals := tr.dag.ReadyGoals()
		for _, g := range readyGoals {
			newlyReady = append(newlyReady, QueuedGoal{Goal: g, TaskID: tr.task.ID})
		}
	}

	return newlyReady, completedCount, nil
}

// CheckStaleGoals checks for goals that have been running too long or whose window died.
func (tr *TaskRunner) CheckStaleGoals(staleTimeout time.Duration) ([]QueuedGoal, error) {
	var retryGoals []QueuedGoal
	now := time.Now()

	for _, g := range tr.dag.Goals() {
		if g.Status != model.GoalStatusRunning {
			continue
		}

		windowName := fmt.Sprintf("%s-%s", g.RepoName, g.ID)
		windowDead := !tr.windowExists(windowName)

		startedAt, tracked := tr.startedAt[g.ID]
		timedOut := tracked && now.Sub(startedAt) > staleTimeout

		if !windowDead && !timedOut {
			continue
		}

		// Check one more time for DONE.json before marking failed
		worktreePath := tr.worktrees[g.RepoName]
		donePath := filepath.Join(worktreePath, "DONE.json")
		if _, err := os.Stat(donePath); err == nil {
			continue // will be picked up by CheckCompletions
		}

		reason := "window died"
		if timedOut {
			reason = "timed out"
		}
		log.Printf("goal %s marked failed: %s", g.ID, reason)

		// Mark failed
		tr.dag.MarkFailed(g.ID)
		if err := tr.store.UpdateGoalStatus(g.ID, model.GoalStatusFailed); err != nil {
			return nil, fmt.Errorf("updating goal status: %w", err)
		}

		payload := fmt.Sprintf(`{"reason":"%s"}`, reason)
		if err := tr.store.InsertEvent(tr.task.ID, g.ID, model.EventGoalFailed, payload); err != nil {
			return nil, fmt.Errorf("inserting goal_failed event: %w", err)
		}

		delete(tr.startedAt, g.ID)

		// Retry if under 3 attempts
		if g.Attempt < 3 {
			if err := tr.store.IncrementGoalAttempt(g.ID); err != nil {
				return nil, fmt.Errorf("incrementing goal attempt: %w", err)
			}
			if err := tr.store.ResetGoalStatus(g.ID); err != nil {
				return nil, fmt.Errorf("resetting goal status: %w", err)
			}
			g.Attempt++
			tr.dag.Get(g.ID).Status = model.GoalStatusPending
			tr.dag.Get(g.ID).Attempt = g.Attempt
			retryGoals = append(retryGoals, QueuedGoal{Goal: *tr.dag.Get(g.ID), TaskID: tr.task.ID})
		}
	}

	return retryGoals, nil
}

// AllGoalsComplete returns true if all goals in the DAG are complete.
func (tr *TaskRunner) AllGoalsComplete() bool {
	return tr.dag.AllComplete()
}

// HasStuckGoals returns true if any goal has failed with max attempts reached.
func (tr *TaskRunner) HasStuckGoals() bool {
	for _, g := range tr.dag.Goals() {
		if g.Status == model.GoalStatusFailed && g.Attempt >= 3 {
			return true
		}
	}
	return false
}

// Cleanup kills the tmux session and compresses logs.
func (tr *TaskRunner) Cleanup() {
	if tr.tmuxSession != "" {
		tr.tmux.KillSession(tr.tmuxSession)
	}
	tr.logMgr.CompressTaskLogs(tr.task.ID)
}

// windowExists checks if a window exists in the task's tmux session.
func (tr *TaskRunner) windowExists(windowName string) bool {
	windows, err := tr.tmux.ListWindows(tr.tmuxSession)
	if err != nil {
		return false
	}
	for _, w := range windows {
		if w == windowName {
			return true
		}
	}
	return false
}

// TaskID returns the task's ID.
func (tr *TaskRunner) TaskID() string {
	return tr.task.ID
}

// TmuxSession returns the tmux session name.
func (tr *TaskRunner) TmuxSession() string {
	return tr.tmuxSession
}

// GatherDiffs collects git diffs from all repo worktrees.
func (tr *TaskRunner) GatherDiffs() []spotter.RepoDiff {
	var diffs []spotter.RepoDiff
	for repoName, worktreePath := range tr.worktrees {
		diffStat, err := tr.git.Run(worktreePath, "diff", "--stat", "HEAD")
		if err != nil {
			diffStat = fmt.Sprintf("(error getting diff stat: %v)", err)
		}

		diff, err := tr.git.Run(worktreePath, "diff", "HEAD")
		if err != nil {
			diff = fmt.Sprintf("(error getting diff: %v)", err)
		}

		// If HEAD diff is empty, try showing the log of commits
		if diff == "" {
			logOut, err := tr.git.Run(worktreePath, "log", "--oneline", "-20")
			if err == nil && logOut != "" {
				diffStat = logOut
			}
			// Try diff against the initial commit
			diff2, err := tr.git.Run(worktreePath, "diff", "HEAD~1")
			if err == nil && diff2 != "" {
				diff = diff2
			}
		}

		diffs = append(diffs, spotter.RepoDiff{
			RepoName: repoName,
			DiffStat: diffStat,
			Diff:     diff,
		})
	}
	return diffs
}

// GatherSummaries reads DONE.json from each worktree and returns goal summaries.
func (tr *TaskRunner) GatherSummaries() []spotter.GoalSummary {
	var summaries []spotter.GoalSummary
	for _, g := range tr.dag.Goals() {
		summary := spotter.GoalSummary{
			GoalID:      g.ID,
			RepoName:    g.RepoName,
			Description: g.Description,
			Status:      string(g.Status),
		}

		worktreePath := tr.worktrees[g.RepoName]
		donePath := filepath.Join(worktreePath, "DONE.json")
		data, err := os.ReadFile(donePath)
		if err == nil {
			var done DoneJSON
			if json.Unmarshal(data, &done) == nil {
				summary.Summary = done.Summary
				summary.Notes = done.Notes
				summary.Status = done.Status
			}
		}

		summaries = append(summaries, summary)
	}
	return summaries
}

// SpawnSpotter creates a tmux window for the spotter agent and launches it.
func (tr *TaskRunner) SpawnSpotter() error {
	tr.spotterAttempt++
	windowName := fmt.Sprintf("spotter-%d", tr.spotterAttempt)

	// Create tmux window
	if err := tr.tmux.NewWindow(tr.tmuxSession, windowName); err != nil {
		return fmt.Errorf("creating spotter window: %w", err)
	}

	// Enable pipe-pane logging
	logPath := tr.logMgr.LogPath(tr.task.ID, fmt.Sprintf("spotter-%d", tr.spotterAttempt))
	if err := tr.tmux.PipePane(tr.tmuxSession, windowName, logPath); err != nil {
		log.Printf("warning: pipe-pane for spotter failed: %v", err)
	}

	// Build spotter prompt
	promptData := spotter.SpotterPromptData{
		Spec:      tr.task.Spec,
		RepoDiffs: tr.GatherDiffs(),
		Summaries: tr.GatherSummaries(),
	}

	prompt, err := spotter.BuildSpotterPrompt(promptData)
	if err != nil {
		return fmt.Errorf("building spotter prompt: %w", err)
	}

	// Spawn agent
	if err := tr.spawner.Spawn(context.Background(), lead.SpawnOpts{
		TmuxSession: tr.tmuxSession,
		WindowName:  windowName,
		WorkDir:     tr.taskDir,
		Prompt:      prompt,
	}); err != nil {
		return fmt.Errorf("spawning spotter agent: %w", err)
	}

	tr.spotterRunning = true

	if err := tr.store.InsertEvent(tr.task.ID, "", model.EventSpotterSpawned,
		fmt.Sprintf(`{"attempt":%d}`, tr.spotterAttempt)); err != nil {
		log.Printf("warning: failed to insert spotter_spawned event: %v", err)
	}

	log.Printf("spotter: spawned for task %s (attempt %d)", tr.task.ID, tr.spotterAttempt)
	return nil
}

// CheckSpotterVerdict checks for a VERDICT.json file and parses it.
// Returns the verdict, whether one was found, and any error.
func (tr *TaskRunner) CheckSpotterVerdict() (*spotter.VerdictJSON, bool, error) {
	verdictPath := filepath.Join(tr.taskDir, "VERDICT.json")
	data, err := os.ReadFile(verdictPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("reading VERDICT.json: %w", err)
	}

	var verdict spotter.VerdictJSON
	if err := json.Unmarshal(data, &verdict); err != nil {
		return nil, false, fmt.Errorf("parsing VERDICT.json: %w", err)
	}

	// Record the review in SQLite
	review := &model.SpotterReview{
		TaskID:  tr.task.ID,
		Attempt: tr.spotterAttempt,
		Verdict: verdict.Verdict,
		Output:  string(data),
	}
	if err := tr.store.InsertSpotterReview(review); err != nil {
		log.Printf("warning: failed to insert spotter review: %v", err)
	}

	payload, _ := json.Marshal(verdict)
	if err := tr.store.InsertEvent(tr.task.ID, "", model.EventReviewVerdict, string(payload)); err != nil {
		log.Printf("warning: failed to insert review_verdict event: %v", err)
	}

	// Kill the spotter window
	windowName := fmt.Sprintf("spotter-%d", tr.spotterAttempt)
	tr.tmux.KillWindow(tr.tmuxSession, windowName)
	tr.spotterRunning = false

	// Remove VERDICT.json so it's not picked up again
	os.Remove(verdictPath)

	log.Printf("spotter: verdict for task %s: %s", tr.task.ID, verdict.Verdict)
	return &verdict, true, nil
}

// HandleApproval creates PRs for all repos after spotter approval.
func (tr *TaskRunner) HandleApproval() error {
	for repoName, worktreePath := range tr.worktrees {
		prURL, err := tr.createPR(repoName, worktreePath)
		if err != nil {
			log.Printf("warning: failed to create PR for %s: %v", repoName, err)
			continue
		}

		payload := fmt.Sprintf(`{"repo":"%s","url":"%s"}`, repoName, prURL)
		if err := tr.store.InsertEvent(tr.task.ID, "", model.EventPRCreated, payload); err != nil {
			log.Printf("warning: failed to insert pr_created event: %v", err)
		}

		log.Printf("spotter: created PR for %s: %s", repoName, prURL)
	}
	return nil
}

// createPR pushes the worktree branch and creates a PR via gh CLI.
func (tr *TaskRunner) createPR(repoName, worktreePath string) (string, error) {
	// Push the branch
	branchName := fmt.Sprintf("belayer/task-%s/%s", tr.task.ID, repoName)

	if _, err := tr.git.Run(worktreePath, "push", "-u", "origin", "HEAD:"+branchName); err != nil {
		return "", fmt.Errorf("pushing branch: %w", err)
	}

	// Create PR via gh CLI
	title := fmt.Sprintf("[belayer] Task %s: %s", tr.task.ID, repoName)
	prURL, err := tr.git.Run(worktreePath, "-c", "gh", "pr", "create", "--title", title, "--body", "Created by belayer spotter review.", "--head", branchName)
	if err != nil {
		// gh isn't a git command — need to exec directly
		cmd := exec.Command("gh", "pr", "create", "--title", title, "--body", "Created by belayer spotter review.", "--head", branchName)
		cmd.Dir = worktreePath
		out, execErr := cmd.CombinedOutput()
		if execErr != nil {
			return "", fmt.Errorf("creating PR: %s: %w", strings.TrimSpace(string(out)), execErr)
		}
		prURL = strings.TrimSpace(string(out))
	}

	return prURL, nil
}

// HandleRejection creates correction goals for failing repos and prepares for new leads.
func (tr *TaskRunner) HandleRejection(verdict *spotter.VerdictJSON) ([]QueuedGoal, error) {
	var correctionGoals []model.Goal
	var queued []QueuedGoal

	for repoName, rv := range verdict.Repos {
		if rv.Status != "fail" {
			continue
		}

		// Remove old DONE.json from the failing repo's worktree
		worktreePath, ok := tr.worktrees[repoName]
		if ok {
			os.Remove(filepath.Join(worktreePath, "DONE.json"))
		}

		// Create correction goals
		for i, goalDesc := range rv.Goals {
			goalID := fmt.Sprintf("%s-corr-%d-%d", repoName, tr.spotterAttempt, i+1)
			g := model.Goal{
				ID:          goalID,
				TaskID:      tr.task.ID,
				RepoName:    repoName,
				Description: goalDesc,
				DependsOn:   []string{},
				Status:      model.GoalStatusPending,
			}
			correctionGoals = append(correctionGoals, g)
			queued = append(queued, QueuedGoal{Goal: g, TaskID: tr.task.ID})
		}
	}

	if len(correctionGoals) == 0 {
		return nil, nil
	}

	// Insert correction goals into SQLite
	if err := tr.store.InsertGoals(correctionGoals); err != nil {
		return nil, fmt.Errorf("inserting correction goals: %w", err)
	}

	// Add to DAG
	tr.dag.AddGoals(correctionGoals)

	log.Printf("spotter: created %d correction goals for task %s", len(correctionGoals), tr.task.ID)
	return queued, nil
}

// SpotterAttempt returns the current spotter review attempt count.
func (tr *TaskRunner) SpotterAttempt() int {
	return tr.spotterAttempt
}

// SpotterRunning returns whether the spotter is currently active.
func (tr *TaskRunner) SpotterRunning() bool {
	return tr.spotterRunning
}
