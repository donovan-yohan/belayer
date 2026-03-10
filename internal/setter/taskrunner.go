package setter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/donovan-yohan/belayer/internal/anchor"
	"github.com/donovan-yohan/belayer/internal/belayerconfig"
	"github.com/donovan-yohan/belayer/internal/defaults"
	"github.com/donovan-yohan/belayer/internal/goalctx"
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
	Goal            model.Goal
	TaskID          string
	SpotterFeedback string // empty on first attempt, populated on retry after spotter rejection
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

	// Config directories for prompt/profile resolution.
	globalConfigDir   string
	instanceConfigDir string

	// Anchor state
	anchorAttempt int
	anchorRunning bool
	taskDir        string // directory for VERDICT.json

	// Validation
	validationEnabled bool // when true, DONE.json triggers spotting instead of direct completion
}

// NewTaskRunner creates a TaskRunner for the given task.
func NewTaskRunner(task *model.Task, instanceDir, globalCfgDir, instanceCfgDir string, s *store.Store, tm tmux.TmuxManager, lm *logmgr.LogManager, sp lead.AgentSpawner) *TaskRunner {
	return &TaskRunner{
		task:              task,
		worktrees:         make(map[string]string),
		instanceDir:       instanceDir,
		globalConfigDir:   globalCfgDir,
		instanceConfigDir: instanceCfgDir,
		store:             s,
		tmux:              tm,
		logMgr:            lm,
		spawner:           sp,
		git:               &RealGitRunner{},
		startedAt:         make(map[string]time.Time),
		validationEnabled: true,
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
func (tr *TaskRunner) SpawnGoal(queued QueuedGoal) error {
	goal := queued.Goal

	// Guard: don't spawn if the goal is already running in the DAG
	if dagGoal := tr.dag.Get(goal.ID); dagGoal != nil && dagGoal.Status == model.GoalStatusRunning {
		return nil
	}

	// If this is a retry after spotter failure, reset goal status to pending first
	if dagGoal := tr.dag.Get(goal.ID); dagGoal != nil && dagGoal.Status == model.GoalStatusFailed {
		if err := tr.store.ResetGoalStatus(goal.ID); err != nil {
			return fmt.Errorf("resetting goal status: %w", err)
		}
		dagGoal.Status = model.GoalStatusPending
	}

	windowName := fmt.Sprintf("%s-%s", goal.RepoName, goal.ID)

	// Create tmux window
	if err := tr.tmux.NewWindow(tr.tmuxSession, windowName); err != nil {
		return fmt.Errorf("creating window %s: %w", windowName, err)
	}

	// Keep pane open after process exits for death detection
	if err := tr.tmux.SetRemainOnExit(tr.tmuxSession, windowName, true); err != nil {
		log.Printf("warning: set remain-on-exit for %s failed: %v", windowName, err)
	}

	// Enable pipe-pane logging
	logPath := tr.logMgr.LogPath(tr.task.ID, goal.ID)
	if err := tr.tmux.PipePane(tr.tmuxSession, windowName, logPath); err != nil {
		log.Printf("warning: pipe-pane for %s failed: %v", windowName, err)
	}

	// Prepare worktree environment with GOAL.json
	worktreePath := tr.worktrees[goal.RepoName]

	appendPrompt, err := defaults.FS.ReadFile("claudemd/lead.md")
	if err != nil {
		return fmt.Errorf("reading lead system prompt: %w", err)
	}

	if err := goalctx.WriteGoalJSON(worktreePath, goal.ID, goalctx.LeadGoal{
		Role:            "lead",
		TaskSpec:        tr.task.Spec,
		GoalID:          goal.ID,
		RepoName:        goal.RepoName,
		Description:     goal.Description,
		Attempt:         goal.Attempt,
		SpotterFeedback: queued.SpotterFeedback,
	}); err != nil {
		return fmt.Errorf("writing GOAL.json for %s: %w", goal.ID, err)
	}

	// Set mail address for the lead agent.
	// Note: session-level env var is safe because goals are spawned sequentially
	// within a TaskRunner (dequeued one at a time). The env is set and Spawn
	// called atomically before the next goal's SetEnvironment.
	mailAddr := fmt.Sprintf("task/%s/lead/%s/%s", tr.task.ID, goal.RepoName, goal.ID)
	if err := tr.tmux.SetEnvironment(tr.tmuxSession, "BELAYER_MAIL_ADDRESS", mailAddr); err != nil {
		log.Printf("warning: failed to set BELAYER_MAIL_ADDRESS: %v", err)
	}

	goalJSONPath := fmt.Sprintf(".lead/%s/GOAL.json", goal.ID)
	initialPrompt := fmt.Sprintf(`Read %s and begin working on your assignment. Follow these steps:

1. Read %s to understand your goal and task spec
2. If this repo does not have harness docs yet, run /harness:init
3. Run /harness:plan to create an implementation plan for your goal
4. Run /harness:orchestrate to execute the plan
5. Run /harness:complete to finalize — this will reflect, review, and commit
6. Write DONE.json in the same directory as your GOAL.json when complete`, goalJSONPath, goalJSONPath)

	if err := tr.spawner.Spawn(context.Background(), lead.SpawnOpts{
		TmuxSession:        tr.tmuxSession,
		WindowName:         windowName,
		WorkDir:            worktreePath,
		AppendSystemPrompt: string(appendPrompt),
		InitialPrompt:      initialPrompt,
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
// and the number of goals that completed this tick. When validation is enabled,
// goals transition to spotting instead of completing directly.
func (tr *TaskRunner) CheckCompletions() (newlyReady []QueuedGoal, completedCount int, err error) {
	for _, g := range tr.dag.Goals() {
		if g.Status != model.GoalStatusRunning {
			continue
		}

		worktreePath := tr.worktrees[g.RepoName]
		donePath := filepath.Join(worktreePath, ".lead", g.ID, "DONE.json")

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

		if tr.validationEnabled {
			// Transition to spotting — spotter will validate before marking complete
			if spotErr := tr.SpawnSpotter(g); spotErr != nil {
				log.Printf("warning: failed to spawn spotter for %s: %v", g.ID, spotErr)
				continue
			}
			// Do NOT kill the tmux window (spotter reuses it)
			// Do NOT count as completed (goal is still in-flight)
			log.Printf("setter: goal %s transitioned to spotting", g.ID)
		} else {
			// Validation disabled — mark complete directly
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

			// Kill the tmux window and its process tree
			windowName := fmt.Sprintf("%s-%s", g.RepoName, g.ID)
			tr.killPaneProcessTree(windowName)
			tr.tmux.KillWindow(tr.tmuxSession, windowName)

			// Check log rotation
			tr.logMgr.CheckRotation(tr.task.ID, g.ID)
		}
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
		var windowName string
		switch g.Status {
		case model.GoalStatusRunning:
			windowName = fmt.Sprintf("%s-%s", g.RepoName, g.ID)
		case model.GoalStatusSpotting:
			windowName = fmt.Sprintf("spot-%s", g.ID)
		default:
			continue
		}
		windowDead := !tr.windowExists(windowName)
		reason := "window died"

		startedAt, tracked := tr.startedAt[g.ID]
		timedOut := tracked && now.Sub(startedAt) > staleTimeout

		// Check for silence — no log output for silenceThreshold
		if !windowDead && !timedOut {
			logPath := tr.logMgr.LogPath(tr.task.ID, g.ID)
			if info, statErr := os.Stat(logPath); statErr == nil {
				silenceThreshold := 2 * time.Minute
				if now.Sub(info.ModTime()) > silenceThreshold {
					// Check if process has exited first (most definitive signal)
					if dead, deadErr := tr.tmux.IsPaneDead(tr.tmuxSession, windowName); deadErr == nil && dead {
						windowDead = true
						reason = "process exited without signal file"
					} else {
						// Capture pane to check if waiting for input
						paneContent, captureErr := tr.tmux.CapturePaneContent(tr.tmuxSession, windowName, 30)
						if captureErr == nil && looksLikeInputPrompt(paneContent) {
							windowDead = true
							reason = "waiting for input"
						}
					}
				}
			}
		}

		if !windowDead && !timedOut {
			continue
		}

		// Check one more time for signal file before marking failed
		worktreePath := tr.worktrees[g.RepoName]
		if g.Status == model.GoalStatusSpotting {
			spotPath := filepath.Join(worktreePath, ".lead", g.ID, "SPOT.json")
			if _, err := os.Stat(spotPath); err == nil {
				continue // will be picked up by CheckSpottingGoals
			}
		} else {
			donePath := filepath.Join(worktreePath, ".lead", g.ID, "DONE.json")
			if _, err := os.Stat(donePath); err == nil {
				continue // will be picked up by CheckCompletions
			}
		}

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

// CheckSpottingGoals checks goals in "spotting" status for SPOT.json results.
// Returns the count of goals that resolved (passed or failed), newly unblocked goals,
// and goals to re-queue for retry with spotter feedback.
// Note: CheckSpotResult already handles marking goals complete/failed, incrementing
// attempts, and removing DONE.json on failure — this method only collects results
// for the setter to manage queue/lead accounting.
func (tr *TaskRunner) CheckSpottingGoals() (resolvedCount int, newlyReady []QueuedGoal, retryGoals []QueuedGoal, err error) {
	for _, g := range tr.dag.Goals() {
		if g.Status != model.GoalStatusSpotting {
			continue
		}

		spot, found, checkErr := tr.CheckSpotResult(g)
		if checkErr != nil {
			log.Printf("setter: error checking spot result for %s: %v", g.ID, checkErr)
			continue
		}
		if !found {
			continue
		}

		resolvedCount++

		if spot.Pass {
			// Goal validated successfully, check for newly unblocked goals
			readyGoals := tr.dag.ReadyGoals()
			for _, rg := range readyGoals {
				newlyReady = append(newlyReady, QueuedGoal{Goal: rg, TaskID: tr.task.ID})
			}
		} else {
			// Spot failed — re-queue for retry if under max attempts
			// (CheckSpotResult already incremented attempt, so use <= 3)
			if g.Attempt <= 3 {
				retryGoals = append(retryGoals, QueuedGoal{
					Goal:            *g,
					TaskID:          tr.task.ID,
					SpotterFeedback: SpotterFeedbackForGoal(spot),
				})
				log.Printf("setter: goal %s re-queued for retry after spot failure (attempt %d)", g.ID, g.Attempt)
			}
		}
	}

	return resolvedCount, newlyReady, retryGoals, nil
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

// Cleanup kills process trees in all windows, then kills the tmux session and compresses logs.
func (tr *TaskRunner) Cleanup() {
	if tr.tmuxSession != "" {
		// Kill process trees in all windows before destroying the session
		windows, err := tr.tmux.ListWindows(tr.tmuxSession)
		if err == nil {
			for _, w := range windows {
				tr.killPaneProcessTree(w)
			}
			if len(windows) > 0 {
				time.Sleep(300 * time.Millisecond) // grace period for SIGTERM handlers
			}
		}
		tr.tmux.KillSession(tr.tmuxSession)
	}
	tr.logMgr.CompressTaskLogs(tr.task.ID)

	// Clean up mail for this task
	mailTaskDir := filepath.Join(tr.instanceDir, "mail", "task", tr.task.ID)
	if err := os.RemoveAll(mailTaskDir); err != nil {
		log.Printf("warning: failed to clean up task mail directory %s: %v", mailTaskDir, err)
	}
}

// killPaneProcessTree kills all descendant processes of a tmux pane before
// the window is destroyed. This is a best-effort safety net — the primary
// cleanup mechanism is the agent instructions (spotter.md/lead.md) which
// tell agents to stop their processes before writing result files.
//
// Note: if the pane shell has already exited (remain-on-exit), orphaned
// processes are reparented to init and won't appear under pgrep -P.
func (tr *TaskRunner) killPaneProcessTree(windowName string) {
	pid, err := tr.tmux.GetPanePID(tr.tmuxSession, windowName)
	if err != nil {
		log.Printf("warning: could not get pane PID for %s: %v", windowName, err)
		return
	}
	if pid <= 0 {
		return
	}

	// Collect all descendant PIDs recursively using pgrep
	descendants := collectDescendants(pid)
	if len(descendants) == 0 {
		return
	}

	// Kill descendants deepest-first (reverse order) then the root
	for i := len(descendants) - 1; i >= 0; i-- {
		if err := syscall.Kill(descendants[i], syscall.SIGTERM); err != nil && err != syscall.ESRCH {
			log.Printf("cleanup: warning: kill(%d, SIGTERM): %v", descendants[i], err)
		}
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		log.Printf("cleanup: warning: kill(%d, SIGTERM): %v", pid, err)
	}

	log.Printf("cleanup: sent SIGTERM to pane PID %d and %d descendants for window %s",
		pid, len(descendants), windowName)
}

// collectDescendants returns all descendant PIDs of the given parent PID.
func collectDescendants(ppid int) []int {
	out, err := exec.Command("pgrep", "-P", strconv.Itoa(ppid)).Output()
	if err != nil {
		return nil // no children
	}
	var pids []int
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		child, err := strconv.Atoi(line)
		if err != nil {
			continue
		}
		pids = append(pids, child)
		// Recurse into this child's descendants
		pids = append(pids, collectDescendants(child)...)
	}
	return pids
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

// writeProfiles writes validation profiles to <worktreePath>/.lead/<goalID>/profiles/ for agent discovery.
func (tr *TaskRunner) writeProfiles(worktreePath, goalID string) (map[string]string, error) {
	profileDir := filepath.Join(worktreePath, ".lead", goalID, "profiles")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating profiles directory: %w", err)
	}

	profiles := make(map[string]string)
	profileNames := []string{"frontend", "backend", "cli", "library"}
	for _, name := range profileNames {
		content, loadErr := belayerconfig.LoadProfile(tr.globalConfigDir, tr.instanceConfigDir, name)
		if loadErr != nil {
			if embedded, readErr := defaults.FS.ReadFile("profiles/" + name + ".toml"); readErr == nil {
				content = string(embedded)
			} else {
				continue
			}
		}
		profiles[name] = content
		profilePath := filepath.Join(profileDir, name+".toml")
		if err := os.WriteFile(profilePath, []byte(content), 0o644); err != nil {
			log.Printf("warning: failed to write profile %s: %v", name, err)
		}
	}
	return profiles, nil
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
func (tr *TaskRunner) GatherDiffs() []goalctx.RepoDiff {
	var diffs []goalctx.RepoDiff
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

		diffs = append(diffs, goalctx.RepoDiff{
			RepoName: repoName,
			DiffStat: diffStat,
			Diff:     diff,
		})
	}
	return diffs
}

// GatherSummaries reads DONE.json from each worktree and returns goal summaries.
func (tr *TaskRunner) GatherSummaries() []goalctx.GoalSummary {
	var summaries []goalctx.GoalSummary
	for _, g := range tr.dag.Goals() {
		summary := goalctx.GoalSummary{
			GoalID:      g.ID,
			RepoName:    g.RepoName,
			Description: g.Description,
			Status:      string(g.Status),
		}

		worktreePath := tr.worktrees[g.RepoName]
		donePath := filepath.Join(worktreePath, ".lead", g.ID, "DONE.json")
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

// SpawnSpotter transitions a goal to "spotting" and spawns a spotter agent in
// the same tmux window the lead used (the lead has already exited).
func (tr *TaskRunner) SpawnSpotter(goal *model.Goal) error {
	// Mark goal as spotting in DAG and SQLite
	tr.dag.MarkSpotting(goal.ID)
	if err := tr.store.UpdateGoalStatus(goal.ID, model.GoalStatusSpotting); err != nil {
		return fmt.Errorf("updating goal status to spotting: %w", err)
	}

	worktreePath := tr.worktrees[goal.RepoName]

	// Kill the lead's window and its process tree (dev servers, etc.)
	leadWindowName := fmt.Sprintf("%s-%s", goal.RepoName, goal.ID)
	tr.killPaneProcessTree(leadWindowName)
	time.Sleep(300 * time.Millisecond) // grace period for SIGTERM handlers
	tr.tmux.KillWindow(tr.tmuxSession, leadWindowName)

	// Create a new window for the spotter
	windowName := fmt.Sprintf("spot-%s", goal.ID)
	if err := tr.tmux.NewWindow(tr.tmuxSession, windowName); err != nil {
		return fmt.Errorf("creating spotter window %s: %w", windowName, err)
	}

	// Keep pane open after process exits for death detection
	if err := tr.tmux.SetRemainOnExit(tr.tmuxSession, windowName, true); err != nil {
		log.Printf("warning: set remain-on-exit for spotter %s failed: %v", windowName, err)
	}

	appendPrompt, err := defaults.FS.ReadFile("claudemd/spotter.md")
	if err != nil {
		return fmt.Errorf("reading spotter system prompt: %w", err)
	}

	// Write profiles to .lead/<goalID>/profiles/ for agent discovery
	profiles, err := tr.writeProfiles(worktreePath, goal.ID)
	if err != nil {
		return fmt.Errorf("writing profiles for %s: %w", goal.ID, err)
	}

	// Read DONE.json for context
	donePath := filepath.Join(worktreePath, ".lead", goal.ID, "DONE.json")
	doneData, readErr := os.ReadFile(donePath)
	if readErr != nil {
		doneData = []byte("{}")
	}

	// Write GOAL.json for spotter
	if err := goalctx.WriteGoalJSON(worktreePath, goal.ID, goalctx.SpotterGoal{
		Role:        "spotter",
		GoalID:      goal.ID,
		RepoName:    goal.RepoName,
		Description: goal.Description,
		WorkDir:     worktreePath,
		Profiles:    profiles,
		DoneJSON:    string(doneData),
	}); err != nil {
		return fmt.Errorf("writing spotter GOAL.json for %s: %w", goal.ID, err)
	}

	// Set mail address for the spotter agent
	spotterMailAddr := fmt.Sprintf("task/%s/spotter/%s/%s", tr.task.ID, goal.RepoName, goal.ID)
	if err := tr.tmux.SetEnvironment(tr.tmuxSession, "BELAYER_MAIL_ADDRESS", spotterMailAddr); err != nil {
		log.Printf("warning: failed to set BELAYER_MAIL_ADDRESS for spotter: %v", err)
	}

	goalJSONPath := fmt.Sprintf(".lead/%s/GOAL.json", goal.ID)

	// Spawn agent in a new window
	if err := tr.spawner.Spawn(context.Background(), lead.SpawnOpts{
		TmuxSession:        tr.tmuxSession,
		WindowName:         windowName,
		WorkDir:            worktreePath,
		AppendSystemPrompt: string(appendPrompt),
		InitialPrompt:      fmt.Sprintf("Read %s and begin validating the lead's work.", goalJSONPath),
	}); err != nil {
		return fmt.Errorf("spawning spotter for %s: %w", goal.ID, err)
	}

	if err := tr.store.InsertEvent(tr.task.ID, goal.ID, model.EventSpotterSpawned,
		fmt.Sprintf(`{"goal_id":"%s"}`, goal.ID)); err != nil {
		log.Printf("warning: failed to insert spotter_spawned event: %v", err)
	}

	tr.startedAt[goal.ID] = time.Now()

	log.Printf("spotter: spawned for goal %s (task %s)", goal.ID, tr.task.ID)
	return nil
}

// CheckSpotResult checks for a SPOT.json file in the goal's worktree and parses it.
// Returns the result, whether one was found, and any error.
func (tr *TaskRunner) CheckSpotResult(goal *model.Goal) (*spotter.SpotJSON, bool, error) {
	worktreePath := tr.worktrees[goal.RepoName]
	spotPath := filepath.Join(worktreePath, ".lead", goal.ID, "SPOT.json")

	data, err := os.ReadFile(spotPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("reading SPOT.json for %s: %w", goal.ID, err)
	}

	var spot spotter.SpotJSON
	if err := json.Unmarshal(data, &spot); err != nil {
		return nil, false, fmt.Errorf("parsing SPOT.json for %s: %w", goal.ID, err)
	}

	// Record the spotter verdict event
	payload, _ := json.Marshal(spot)
	if err := tr.store.InsertEvent(tr.task.ID, goal.ID, model.EventSpotterVerdict, string(payload)); err != nil {
		log.Printf("warning: failed to insert spotter_verdict event: %v", err)
	}

	// Kill the spotter tmux window and its process tree (dev servers, etc.)
	windowName := fmt.Sprintf("spot-%s", goal.ID)
	tr.killPaneProcessTree(windowName)
	time.Sleep(300 * time.Millisecond) // grace period for SIGTERM handlers
	tr.tmux.KillWindow(tr.tmuxSession, windowName)
	delete(tr.startedAt, goal.ID)

	// Remove SPOT.json so it's not picked up again
	os.Remove(spotPath)

	if spot.Pass {
		// Mark goal complete
		tr.dag.MarkComplete(goal.ID)
		if err := tr.store.UpdateGoalStatus(goal.ID, model.GoalStatusComplete); err != nil {
			return &spot, true, fmt.Errorf("updating goal status to complete: %w", err)
		}

		if err := tr.store.InsertEvent(tr.task.ID, goal.ID, model.EventGoalCompleted,
			string(payload)); err != nil {
			log.Printf("warning: failed to insert goal_completed event: %v", err)
		}

		// Check log rotation
		tr.logMgr.CheckRotation(tr.task.ID, goal.ID)

		log.Printf("spotter: goal %s passed validation", goal.ID)
	} else {
		// Mark goal failed for retry
		tr.dag.MarkFailed(goal.ID)
		if err := tr.store.UpdateGoalStatus(goal.ID, model.GoalStatusFailed); err != nil {
			return &spot, true, fmt.Errorf("updating goal status to failed: %w", err)
		}

		// Increment attempt for retry tracking
		if err := tr.store.IncrementGoalAttempt(goal.ID); err != nil {
			log.Printf("warning: failed to increment goal attempt: %v", err)
		}
		if dagGoal := tr.dag.Get(goal.ID); dagGoal != nil {
			dagGoal.Attempt++
		}

		if err := tr.store.InsertEvent(tr.task.ID, goal.ID, model.EventGoalFailed,
			string(payload)); err != nil {
			log.Printf("warning: failed to insert goal_failed event: %v", err)
		}

		// Remove DONE.json so the retry starts fresh
		os.Remove(filepath.Join(worktreePath, ".lead", goal.ID, "DONE.json"))

		log.Printf("spotter: goal %s failed validation with %d issues", goal.ID, len(spot.Issues))
	}

	return &spot, true, nil
}

// SpawnAnchor creates a tmux window for the anchor agent and launches it.
func (tr *TaskRunner) SpawnAnchor() error {
	tr.anchorAttempt++
	windowName := "anchor"

	// Kill previous anchor window on retry (it has remain-on-exit)
	if tr.anchorAttempt > 1 {
		tr.killPaneProcessTree(windowName)
		_ = tr.tmux.KillWindow(tr.tmuxSession, windowName)
	}

	// Create tmux window
	if err := tr.tmux.NewWindow(tr.tmuxSession, windowName); err != nil {
		return fmt.Errorf("creating anchor window: %w", err)
	}

	// Keep pane open after process exits for death detection
	if err := tr.tmux.SetRemainOnExit(tr.tmuxSession, windowName, true); err != nil {
		log.Printf("warning: set remain-on-exit for anchor failed: %v", err)
	}

	// Enable pipe-pane logging
	logPath := tr.logMgr.LogPath(tr.task.ID, fmt.Sprintf("anchor-%d", tr.anchorAttempt))
	if err := tr.tmux.PipePane(tr.tmuxSession, windowName, logPath); err != nil {
		log.Printf("warning: pipe-pane for anchor failed: %v", err)
	}

	appendPrompt, err := defaults.FS.ReadFile("claudemd/anchor.md")
	if err != nil {
		return fmt.Errorf("reading anchor system prompt: %w", err)
	}

	if err := goalctx.WriteGoalJSON(tr.taskDir, "anchor", goalctx.AnchorGoal{
		Role:      "anchor",
		TaskSpec:  tr.task.Spec,
		RepoDiffs: tr.GatherDiffs(),
		Summaries: tr.GatherSummaries(),
	}); err != nil {
		return fmt.Errorf("writing anchor GOAL.json: %w", err)
	}

	// Set mail address for the anchor agent
	anchorMailAddr := fmt.Sprintf("task/%s/anchor", tr.task.ID)
	if err := tr.tmux.SetEnvironment(tr.tmuxSession, "BELAYER_MAIL_ADDRESS", anchorMailAddr); err != nil {
		log.Printf("warning: failed to set BELAYER_MAIL_ADDRESS for anchor: %v", err)
	}

	// Spawn agent
	if err := tr.spawner.Spawn(context.Background(), lead.SpawnOpts{
		TmuxSession:        tr.tmuxSession,
		WindowName:         windowName,
		WorkDir:            tr.taskDir,
		AppendSystemPrompt: string(appendPrompt),
		InitialPrompt:      "Read .lead/anchor/GOAL.json and begin cross-repo review.",
	}); err != nil {
		return fmt.Errorf("spawning anchor agent: %w", err)
	}

	tr.anchorRunning = true

	if err := tr.store.InsertEvent(tr.task.ID, "", model.EventAnchorSpawned,
		fmt.Sprintf(`{"attempt":%d}`, tr.anchorAttempt)); err != nil {
		log.Printf("warning: failed to insert anchor_spawned event: %v", err)
	}

	log.Printf("anchor: spawned for task %s (attempt %d)", tr.task.ID, tr.anchorAttempt)
	return nil
}

// CheckAnchorVerdict checks for a VERDICT.json file and parses it.
// Returns the verdict, whether one was found, and any error.
func (tr *TaskRunner) CheckAnchorVerdict() (*anchor.VerdictJSON, bool, error) {
	verdictPath := filepath.Join(tr.taskDir, "VERDICT.json")
	data, err := os.ReadFile(verdictPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("reading VERDICT.json: %w", err)
	}

	var verdict anchor.VerdictJSON
	if err := json.Unmarshal(data, &verdict); err != nil {
		return nil, false, fmt.Errorf("parsing VERDICT.json: %w", err)
	}

	// Record the review in SQLite
	review := &model.SpotterReview{
		TaskID:  tr.task.ID,
		Attempt: tr.anchorAttempt,
		Verdict: verdict.Verdict,
		Output:  string(data),
	}
	if err := tr.store.InsertAnchorReview(review); err != nil {
		log.Printf("warning: failed to insert anchor review: %v", err)
	}

	payload, _ := json.Marshal(verdict)
	if err := tr.store.InsertEvent(tr.task.ID, "", model.EventAnchorVerdict, string(payload)); err != nil {
		log.Printf("warning: failed to insert anchor_verdict event: %v", err)
	}

	// Kill the anchor window and its process tree
	tr.killPaneProcessTree("anchor")
	tr.tmux.KillWindow(tr.tmuxSession, "anchor")
	tr.anchorRunning = false

	// Remove VERDICT.json so it's not picked up again
	os.Remove(verdictPath)

	log.Printf("anchor: verdict for task %s: %s", tr.task.ID, verdict.Verdict)
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

		log.Printf("anchor: created PR for %s: %s", repoName, prURL)
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
func (tr *TaskRunner) HandleRejection(verdict *anchor.VerdictJSON) ([]QueuedGoal, error) {
	var correctionGoals []model.Goal
	var queued []QueuedGoal

	for repoName, rv := range verdict.Repos {
		if rv.Status != "fail" {
			continue
		}

		// Remove old DONE.json files from the failing repo's worktree
		worktreePath, ok := tr.worktrees[repoName]
		if ok {
			for _, g := range tr.dag.Goals() {
				if g.RepoName == repoName {
					os.Remove(filepath.Join(worktreePath, ".lead", g.ID, "DONE.json"))
				}
			}
		}

		// Create correction goals
		for i, goalDesc := range rv.Goals {
			goalID := fmt.Sprintf("%s-corr-%d-%d", repoName, tr.anchorAttempt, i+1)
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

	log.Printf("anchor: created %d correction goals for task %s", len(correctionGoals), tr.task.ID)
	return queued, nil
}

// AnchorAttempt returns the current anchor review attempt count.
func (tr *TaskRunner) AnchorAttempt() int {
	return tr.anchorAttempt
}

// AnchorRunning returns whether the anchor is currently active.
func (tr *TaskRunner) AnchorRunning() bool {
	return tr.anchorRunning
}

// IsSingleRepo returns true if the task involves only one repository,
// meaning cross-repo anchor review can be skipped.
func (tr *TaskRunner) IsSingleRepo() bool {
	return len(tr.worktrees) <= 1
}

// SpotterFeedbackForGoal formats spotter issues into a string for the lead prompt retry.
func SpotterFeedbackForGoal(spot *spotter.SpotJSON) string {
	if spot == nil || spot.Pass {
		return ""
	}
	var buf strings.Builder
	buf.WriteString("FAILED CHECKS:\n")
	for _, issue := range spot.Issues {
		buf.WriteString(fmt.Sprintf("- %s [%s]: %s\n", issue.Check, issue.Severity, issue.Description))
	}
	return buf.String()
}

// looksLikeInputPrompt checks if captured pane content suggests the session
// is waiting for user input rather than actively working.
func looksLikeInputPrompt(content string) bool {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) == 0 {
		return false
	}
	lastLine := strings.TrimSpace(lines[len(lines)-1])
	// Claude Code shows ">" when waiting for input
	return lastLine == ">" || strings.HasSuffix(lastLine, "> ")
}
