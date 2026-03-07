package setter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/lead"
	"github.com/donovan-yohan/belayer/internal/logmgr"
	"github.com/donovan-yohan/belayer/internal/model"
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
	startedAt   map[string]time.Time // goalID -> when it started running
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

// CheckCompletions scans worktrees for DONE.json files and returns newly unblocked goals.
func (tr *TaskRunner) CheckCompletions() ([]QueuedGoal, error) {
	var newlyReady []QueuedGoal

	for _, g := range tr.dag.Goals() {
		if g.Status != model.GoalStatusRunning {
			continue
		}

		worktreePath := tr.worktrees[g.RepoName]
		donePath := filepath.Join(worktreePath, "DONE.json")

		data, err := os.ReadFile(donePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading DONE.json for %s: %w", g.ID, err)
		}

		// Parse DONE.json
		var done DoneJSON
		if err := json.Unmarshal(data, &done); err != nil {
			log.Printf("warning: invalid DONE.json for goal %s: %v", g.ID, err)
			continue
		}

		// Mark complete in DAG and SQLite
		tr.dag.MarkComplete(g.ID)
		if err := tr.store.UpdateGoalStatus(g.ID, model.GoalStatusComplete); err != nil {
			return nil, fmt.Errorf("updating goal status: %w", err)
		}

		payload, _ := json.Marshal(done)
		if err := tr.store.InsertEvent(tr.task.ID, g.ID, model.EventGoalCompleted, string(payload)); err != nil {
			return nil, fmt.Errorf("inserting goal_completed event: %w", err)
		}

		delete(tr.startedAt, g.ID)

		// Kill the tmux window
		windowName := fmt.Sprintf("%s-%s", g.RepoName, g.ID)
		tr.tmux.KillWindow(tr.tmuxSession, windowName)

		// Check log rotation
		tr.logMgr.CheckRotation(tr.task.ID, g.ID)
	}

	// Find newly ready goals
	readyGoals := tr.dag.ReadyGoals()
	for _, g := range readyGoals {
		newlyReady = append(newlyReady, QueuedGoal{Goal: g, TaskID: tr.task.ID})
	}

	return newlyReady, nil
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
