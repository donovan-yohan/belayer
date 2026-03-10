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
	"github.com/donovan-yohan/belayer/internal/climbctx"
	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/lead"
	"github.com/donovan-yohan/belayer/internal/logmgr"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/spotter"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/donovan-yohan/belayer/internal/tmux"
)

// TopJSON is the structured output a lead writes to TOP.json.
type TopJSON struct {
	Status       string   `json:"status"`
	Summary      string   `json:"summary"`
	FilesChanged []string `json:"files_changed"`
	Notes        string   `json:"notes"`
}

// QueuedClimb is a climb waiting to be spawned.
type QueuedClimb struct {
	Goal            model.Climb
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

// ProblemRunner manages the lifecycle of a single problem.
type ProblemRunner struct {
	task        *model.Problem
	dag         *DAG
	worktrees   map[string]string // repoName -> worktreePath
	tmuxSession string
	instanceDir string
	store       *store.Store
	tmux        tmux.TmuxManager
	logMgr      *logmgr.LogManager
	spawner     lead.AgentSpawner
	git         GitRunner
	startedAt   map[string]time.Time // climbID -> when it started running

	// Config directories for prompt/profile resolution.
	globalConfigDir   string
	instanceConfigDir string

	// Anchor state
	anchorAttempt int
	anchorRunning bool
	problemDir    string // directory for VERDICT.json

	// Validation
	validationEnabled bool // when true, TOP.json triggers spotting instead of direct completion
}

// NewProblemRunner creates a ProblemRunner for the given problem.
func NewProblemRunner(task *model.Problem, instanceDir, globalCfgDir, instanceCfgDir string, s *store.Store, tm tmux.TmuxManager, lm *logmgr.LogManager, sp lead.AgentSpawner) *ProblemRunner {
	return &ProblemRunner{
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

// Init initializes the problem: creates worktrees, tmux session, and builds the DAG.
// Returns ready climbs that should be queued for spawning.
func (tr *ProblemRunner) Init() ([]QueuedClimb, error) {
	// Update problem status to running
	if err := tr.store.UpdateProblemStatus(tr.task.ID, model.ProblemStatusRunning); err != nil {
		return nil, fmt.Errorf("updating problem status: %w", err)
	}
	tr.task.Status = model.ProblemStatusRunning

	// Parse climbs from the problem
	climbs, err := tr.store.GetClimbsForProblem(tr.task.ID)
	if err != nil {
		return nil, fmt.Errorf("getting climbs: %w", err)
	}

	// Build DAG
	tr.dag = BuildDAG(climbs)

	// Get unique repos
	repos := make(map[string]bool)
	for _, climb := range climbs {
		repos[climb.RepoName] = true
	}

	// Create worktrees
	for repoName := range repos {
		worktreePath, err := instance.CreateWorktree(tr.instanceDir, tr.task.ID, repoName)
		if err != nil {
			return nil, fmt.Errorf("creating worktree for %s: %w", repoName, err)
		}
		tr.worktrees[repoName] = worktreePath
	}

	// Set problem directory for VERDICT.json
	tr.problemDir = filepath.Join(tr.instanceDir, "tasks", tr.task.ID)
	os.MkdirAll(tr.problemDir, 0o755)

	// Create tmux session
	tr.tmuxSession = fmt.Sprintf("belayer-problem-%s", tr.task.ID)
	if !tr.tmux.HasSession(tr.tmuxSession) {
		if err := tr.tmux.NewSession(tr.tmuxSession); err != nil {
			return nil, fmt.Errorf("creating tmux session: %w", err)
		}
	}

	// Ensure log directory exists
	if err := tr.logMgr.EnsureDir(tr.task.ID); err != nil {
		return nil, fmt.Errorf("ensuring log dir: %w", err)
	}

	// Find ready climbs
	readyClimbs := tr.dag.ReadyClimbs()
	var queued []QueuedClimb
	for _, climb := range readyClimbs {
		queued = append(queued, QueuedClimb{Goal: climb, TaskID: tr.task.ID})
	}

	return queued, nil
}

// SpawnClimb creates a tmux window for a climb and launches an agent session.
func (tr *ProblemRunner) SpawnClimb(queued QueuedClimb) error {
	climb := queued.Goal

	// Guard: don't spawn if the climb is already running in the DAG
	if dagClimb := tr.dag.Get(climb.ID); dagClimb != nil && dagClimb.Status == model.ClimbStatusRunning {
		return nil
	}

	// If this is a retry after spotter failure, reset climb status to pending first
	if dagClimb := tr.dag.Get(climb.ID); dagClimb != nil && dagClimb.Status == model.ClimbStatusFailed {
		if err := tr.store.ResetClimbStatus(climb.ID); err != nil {
			return fmt.Errorf("resetting climb status: %w", err)
		}
		dagClimb.Status = model.ClimbStatusPending
	}

	windowName := fmt.Sprintf("%s-%s", climb.RepoName, climb.ID)

	// Create tmux window
	if err := tr.tmux.NewWindow(tr.tmuxSession, windowName); err != nil {
		return fmt.Errorf("creating window %s: %w", windowName, err)
	}

	// Keep pane open after process exits for death detection
	if err := tr.tmux.SetRemainOnExit(tr.tmuxSession, windowName, true); err != nil {
		log.Printf("warning: set remain-on-exit for %s failed: %v", windowName, err)
	}

	// Enable pipe-pane logging
	logPath := tr.logMgr.LogPath(tr.task.ID, climb.ID)
	if err := tr.tmux.PipePane(tr.tmuxSession, windowName, logPath); err != nil {
		log.Printf("warning: pipe-pane for %s failed: %v", windowName, err)
	}

	// Prepare worktree environment with GOAL.json
	worktreePath := tr.worktrees[climb.RepoName]

	appendPrompt, err := defaults.FS.ReadFile("claudemd/lead.md")
	if err != nil {
		return fmt.Errorf("reading lead system prompt: %w", err)
	}

	if err := climbctx.WriteClimbJSON(worktreePath, climb.ID, climbctx.LeadClimb{
		Role:            "lead",
		ProblemSpec:     tr.task.Spec,
		ClimbID:         climb.ID,
		RepoName:        climb.RepoName,
		Description:     climb.Description,
		Attempt:         climb.Attempt,
		SpotterFeedback: queued.SpotterFeedback,
	}); err != nil {
		return fmt.Errorf("writing GOAL.json for %s: %w", climb.ID, err)
	}

	mailAddr := fmt.Sprintf("problem/%s/lead/%s/%s", tr.task.ID, climb.RepoName, climb.ID)

	goalJSONPath := fmt.Sprintf(".lead/%s/GOAL.json", climb.ID)
	initialPrompt := fmt.Sprintf(`Read %s and begin working on your assignment. Follow the harness pipeline:

1. Read %s to understand your goal and task spec
2. If this repo does not have harness docs yet, run /harness:init
3. Run /harness:plan to create an implementation plan from your goal spec
4. Run /harness:orchestrate to execute the plan with worker agents
5. Run /harness:review to run multi-agent code review and fix any findings
6. Run /harness:reflect to update docs, capture learnings and retrospective
7. Run /harness:complete to archive the plan and commit
8. Write TOP.json in .lead/%s/ when complete

You are fully autonomous. Make decisions, document drift, and keep moving.`, goalJSONPath, goalJSONPath, climb.ID)

	if err := tr.spawner.Spawn(context.Background(), lead.SpawnOpts{
		TmuxSession:        tr.tmuxSession,
		WindowName:         windowName,
		WorkDir:            worktreePath,
		AppendSystemPrompt: string(appendPrompt),
		InitialPrompt:      initialPrompt,
		Env:                map[string]string{"BELAYER_MAIL_ADDRESS": mailAddr},
	}); err != nil {
		return fmt.Errorf("spawning agent for %s: %w", climb.ID, err)
	}

	// Update status in DAG and SQLite
	tr.dag.MarkRunning(climb.ID)
	if err := tr.store.UpdateClimbStatus(climb.ID, model.ClimbStatusRunning); err != nil {
		return fmt.Errorf("updating climb status: %w", err)
	}

	if err := tr.store.InsertEvent(tr.task.ID, climb.ID, model.EventClimbStarted, "{}"); err != nil {
		return fmt.Errorf("inserting climb_started event: %w", err)
	}

	tr.startedAt[climb.ID] = time.Now()

	return nil
}

// CheckCompletions scans worktrees for TOP.json files and returns newly unblocked climbs
// and the number of climbs that completed this tick. When validation is enabled,
// climbs transition to spotting instead of completing directly.
func (tr *ProblemRunner) CheckCompletions() (newlyReady []QueuedClimb, completedCount int, err error) {
	for _, climb := range tr.dag.Climbs() {
		if climb.Status != model.ClimbStatusRunning {
			continue
		}

		worktreePath := tr.worktrees[climb.RepoName]
		topPath := filepath.Join(worktreePath, ".lead", climb.ID, "TOP.json")

		data, readErr := os.ReadFile(topPath)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				continue
			}
			return nil, 0, fmt.Errorf("reading TOP.json for %s: %w", climb.ID, readErr)
		}

		// Parse TOP.json
		var top TopJSON
		if jsonErr := json.Unmarshal(data, &top); jsonErr != nil {
			log.Printf("warning: invalid TOP.json for climb %s: %v", climb.ID, jsonErr)
			continue
		}

		if tr.validationEnabled {
			// Transition to spotting — spotter will validate before marking complete
			if spotErr := tr.SpawnSpotter(climb); spotErr != nil {
				log.Printf("warning: failed to spawn spotter for %s: %v", climb.ID, spotErr)
				continue
			}
			// Do NOT kill the tmux window (spotter reuses it)
			// Do NOT count as completed (climb is still in-flight)
			log.Printf("setter: climb %s transitioned to spotting", climb.ID)
		} else {
			// Validation disabled — mark complete directly
			tr.dag.MarkComplete(climb.ID)
			if storeErr := tr.store.UpdateClimbStatus(climb.ID, model.ClimbStatusComplete); storeErr != nil {
				return nil, 0, fmt.Errorf("updating climb status: %w", storeErr)
			}

			payload, _ := json.Marshal(top)
			if eventErr := tr.store.InsertEvent(tr.task.ID, climb.ID, model.EventClimbCompleted, string(payload)); eventErr != nil {
				return nil, 0, fmt.Errorf("inserting climb_completed event: %w", eventErr)
			}

			delete(tr.startedAt, climb.ID)
			completedCount++

			// Kill the tmux window and its process tree
			windowName := fmt.Sprintf("%s-%s", climb.RepoName, climb.ID)
			tr.killPaneProcessTree(windowName)
			tr.tmux.KillWindow(tr.tmuxSession, windowName)

			// Check log rotation
			tr.logMgr.CheckRotation(tr.task.ID, climb.ID)
		}
	}

	// Only check for newly unblocked climbs if something actually completed
	if completedCount > 0 {
		readyClimbs := tr.dag.ReadyClimbs()
		for _, climb := range readyClimbs {
			newlyReady = append(newlyReady, QueuedClimb{Goal: climb, TaskID: tr.task.ID})
		}
	}

	return newlyReady, completedCount, nil
}

// CheckStaleClimbs checks for climbs that have been running too long or whose window died.
func (tr *ProblemRunner) CheckStaleClimbs(staleTimeout time.Duration) ([]QueuedClimb, error) {
	var retryClimbs []QueuedClimb
	now := time.Now()

	for _, climb := range tr.dag.Climbs() {
		var windowName string
		switch climb.Status {
		case model.ClimbStatusRunning:
			windowName = fmt.Sprintf("%s-%s", climb.RepoName, climb.ID)
		case model.ClimbStatusSpotting:
			windowName = fmt.Sprintf("spot-%s", climb.ID)
		default:
			continue
		}
		windowDead := !tr.windowExists(windowName)
		reason := "window died"

		startedAt, tracked := tr.startedAt[climb.ID]
		timedOut := tracked && now.Sub(startedAt) > staleTimeout

		// Check for silence — no log output for silenceThreshold
		if !windowDead && !timedOut {
			logPath := tr.logMgr.LogPath(tr.task.ID, climb.ID)
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
		worktreePath := tr.worktrees[climb.RepoName]
		if climb.Status == model.ClimbStatusSpotting {
			spotPath := filepath.Join(worktreePath, ".lead", climb.ID, "SPOT.json")
			if _, err := os.Stat(spotPath); err == nil {
				continue // will be picked up by CheckSpottingClimbs
			}
		} else {
			topPath := filepath.Join(worktreePath, ".lead", climb.ID, "TOP.json")
			if _, err := os.Stat(topPath); err == nil {
				continue // will be picked up by CheckCompletions
			}
		}

		if timedOut {
			reason = "timed out"
		}
		log.Printf("climb %s marked failed: %s", climb.ID, reason)

		// Mark failed
		tr.dag.MarkFailed(climb.ID)
		if err := tr.store.UpdateClimbStatus(climb.ID, model.ClimbStatusFailed); err != nil {
			return nil, fmt.Errorf("updating climb status: %w", err)
		}

		payload := fmt.Sprintf(`{"reason":"%s"}`, reason)
		if err := tr.store.InsertEvent(tr.task.ID, climb.ID, model.EventClimbFailed, payload); err != nil {
			return nil, fmt.Errorf("inserting climb_failed event: %w", err)
		}

		delete(tr.startedAt, climb.ID)

		// Retry if under 3 attempts
		if climb.Attempt < 3 {
			if err := tr.store.IncrementClimbAttempt(climb.ID); err != nil {
				return nil, fmt.Errorf("incrementing climb attempt: %w", err)
			}
			if err := tr.store.ResetClimbStatus(climb.ID); err != nil {
				return nil, fmt.Errorf("resetting climb status: %w", err)
			}
			climb.Attempt++
			tr.dag.Get(climb.ID).Status = model.ClimbStatusPending
			tr.dag.Get(climb.ID).Attempt = climb.Attempt
			retryClimbs = append(retryClimbs, QueuedClimb{Goal: *tr.dag.Get(climb.ID), TaskID: tr.task.ID})
		}
	}

	return retryClimbs, nil
}

// CheckSpottingClimbs checks climbs in "spotting" status for SPOT.json results.
// Returns the count of climbs that resolved (passed or failed), newly unblocked climbs,
// and climbs to re-queue for retry with spotter feedback.
// Note: CheckSpotResult already handles marking climbs complete/failed, incrementing
// attempts, and removing TOP.json on failure — this method only collects results
// for the setter to manage queue/lead accounting.
func (tr *ProblemRunner) CheckSpottingClimbs() (resolvedCount int, newlyReady []QueuedClimb, retryClimbs []QueuedClimb, err error) {
	for _, climb := range tr.dag.Climbs() {
		if climb.Status != model.ClimbStatusSpotting {
			continue
		}

		spot, found, checkErr := tr.CheckSpotResult(climb)
		if checkErr != nil {
			log.Printf("setter: error checking spot result for %s: %v", climb.ID, checkErr)
			continue
		}
		if !found {
			continue
		}

		resolvedCount++

		if spot.Pass {
			// Climb validated successfully, check for newly unblocked climbs
			readyClimbs := tr.dag.ReadyClimbs()
			for _, rc := range readyClimbs {
				newlyReady = append(newlyReady, QueuedClimb{Goal: rc, TaskID: tr.task.ID})
			}
		} else {
			// Spot failed — re-queue for retry if under max attempts
			// (CheckSpotResult already incremented attempt, so use <= 3)
			if climb.Attempt <= 3 {
				retryClimbs = append(retryClimbs, QueuedClimb{
					Goal:            *climb,
					TaskID:          tr.task.ID,
					SpotterFeedback: SpotterFeedbackForGoal(spot),
				})
				log.Printf("setter: climb %s re-queued for retry after spot failure (attempt %d)", climb.ID, climb.Attempt)
			}
		}
	}

	return resolvedCount, newlyReady, retryClimbs, nil
}

// AllClimbsComplete returns true if all climbs in the DAG are complete.
func (tr *ProblemRunner) AllClimbsComplete() bool {
	return tr.dag.AllComplete()
}

// HasStuckClimbs returns true if any climb has failed with max attempts reached.
func (tr *ProblemRunner) HasStuckClimbs() bool {
	for _, climb := range tr.dag.Climbs() {
		if climb.Status == model.ClimbStatusFailed && climb.Attempt >= 3 {
			return true
		}
	}
	return false
}

// Cleanup kills process trees in all windows, then kills the tmux session and compresses logs.
func (tr *ProblemRunner) Cleanup() {
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

	// Clean up mail for this problem
	mailTaskDir := filepath.Join(tr.instanceDir, "mail", "problem", tr.task.ID)
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
func (tr *ProblemRunner) killPaneProcessTree(windowName string) {
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

// windowExists checks if a window exists in the problem's tmux session.
func (tr *ProblemRunner) windowExists(windowName string) bool {
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

// writeProfiles writes validation profiles to <worktreePath>/.lead/<climbID>/profiles/ for agent discovery.
func (tr *ProblemRunner) writeProfiles(worktreePath, goalID string) (map[string]string, error) {
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

// TaskID returns the problem's ID.
func (tr *ProblemRunner) TaskID() string {
	return tr.task.ID
}

// TmuxSession returns the tmux session name.
func (tr *ProblemRunner) TmuxSession() string {
	return tr.tmuxSession
}

// GatherDiffs collects git diffs from all repo worktrees.
func (tr *ProblemRunner) GatherDiffs() []climbctx.RepoDiff {
	var diffs []climbctx.RepoDiff
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

		diffs = append(diffs, climbctx.RepoDiff{
			RepoName: repoName,
			DiffStat: diffStat,
			Diff:     diff,
		})
	}
	return diffs
}

// GatherSummaries reads TOP.json from each worktree and returns climb summaries.
func (tr *ProblemRunner) GatherSummaries() []climbctx.ClimbSummary {
	var summaries []climbctx.ClimbSummary
	for _, climb := range tr.dag.Climbs() {
		summary := climbctx.ClimbSummary{
			ClimbID:     climb.ID,
			RepoName:    climb.RepoName,
			Description: climb.Description,
			Status:      string(climb.Status),
		}

		worktreePath := tr.worktrees[climb.RepoName]
		topPath := filepath.Join(worktreePath, ".lead", climb.ID, "TOP.json")
		data, err := os.ReadFile(topPath)
		if err == nil {
			var top TopJSON
			if json.Unmarshal(data, &top) == nil {
				summary.Summary = top.Summary
				summary.Notes = top.Notes
				summary.Status = top.Status
			}
		}

		summaries = append(summaries, summary)
	}
	return summaries
}

// SpawnSpotter transitions a climb to "spotting" and spawns a spotter agent in
// the same tmux window the lead used (the lead has already exited).
func (tr *ProblemRunner) SpawnSpotter(climb *model.Climb) error {
	// Mark climb as spotting in DAG and SQLite
	tr.dag.MarkSpotting(climb.ID)
	if err := tr.store.UpdateClimbStatus(climb.ID, model.ClimbStatusSpotting); err != nil {
		return fmt.Errorf("updating climb status to spotting: %w", err)
	}

	worktreePath := tr.worktrees[climb.RepoName]

	// Kill the lead's window and its process tree (dev servers, etc.)
	leadWindowName := fmt.Sprintf("%s-%s", climb.RepoName, climb.ID)
	tr.killPaneProcessTree(leadWindowName)
	time.Sleep(300 * time.Millisecond) // grace period for SIGTERM handlers
	tr.tmux.KillWindow(tr.tmuxSession, leadWindowName)

	// Create a new window for the spotter
	windowName := fmt.Sprintf("spot-%s", climb.ID)
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

	// Write profiles to .lead/<climbID>/profiles/ for agent discovery
	profiles, err := tr.writeProfiles(worktreePath, climb.ID)
	if err != nil {
		return fmt.Errorf("writing profiles for %s: %w", climb.ID, err)
	}

	// Read TOP.json for context
	topPath := filepath.Join(worktreePath, ".lead", climb.ID, "TOP.json")
	topData, readErr := os.ReadFile(topPath)
	if readErr != nil {
		topData = []byte("{}")
	}

	// Write GOAL.json for spotter
	if err := climbctx.WriteClimbJSON(worktreePath, climb.ID, climbctx.SpotterClimb{
		Role:        "spotter",
		ClimbID:     climb.ID,
		RepoName:    climb.RepoName,
		Description: climb.Description,
		WorkDir:     worktreePath,
		Profiles:    profiles,
		TopJSON:     string(topData),
	}); err != nil {
		return fmt.Errorf("writing spotter GOAL.json for %s: %w", climb.ID, err)
	}

	spotterMailAddr := fmt.Sprintf("problem/%s/spotter/%s/%s", tr.task.ID, climb.RepoName, climb.ID)

	goalJSONPath := fmt.Sprintf(".lead/%s/GOAL.json", climb.ID)

	// Spawn agent in a new window
	if err := tr.spawner.Spawn(context.Background(), lead.SpawnOpts{
		TmuxSession:        tr.tmuxSession,
		WindowName:         windowName,
		WorkDir:            worktreePath,
		AppendSystemPrompt: string(appendPrompt),
		InitialPrompt:      fmt.Sprintf("Read %s and begin validating the lead's work.", goalJSONPath),
		Env:                map[string]string{"BELAYER_MAIL_ADDRESS": spotterMailAddr},
	}); err != nil {
		return fmt.Errorf("spawning spotter for %s: %w", climb.ID, err)
	}

	if err := tr.store.InsertEvent(tr.task.ID, climb.ID, model.EventSpotterSpawned,
		fmt.Sprintf(`{"climb_id":"%s"}`, climb.ID)); err != nil {
		log.Printf("warning: failed to insert spotter_spawned event: %v", err)
	}

	tr.startedAt[climb.ID] = time.Now()

	log.Printf("spotter: spawned for climb %s (problem %s)", climb.ID, tr.task.ID)
	return nil
}

// CheckSpotResult checks for a SPOT.json file in the climb's worktree and parses it.
// Returns the result, whether one was found, and any error.
func (tr *ProblemRunner) CheckSpotResult(climb *model.Climb) (*spotter.SpotJSON, bool, error) {
	worktreePath := tr.worktrees[climb.RepoName]
	spotPath := filepath.Join(worktreePath, ".lead", climb.ID, "SPOT.json")

	data, err := os.ReadFile(spotPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("reading SPOT.json for %s: %w", climb.ID, err)
	}

	var spot spotter.SpotJSON
	if err := json.Unmarshal(data, &spot); err != nil {
		return nil, false, fmt.Errorf("parsing SPOT.json for %s: %w", climb.ID, err)
	}

	// Record the spotter verdict event
	payload, _ := json.Marshal(spot)
	if err := tr.store.InsertEvent(tr.task.ID, climb.ID, model.EventSpotterVerdict, string(payload)); err != nil {
		log.Printf("warning: failed to insert spotter_verdict event: %v", err)
	}

	// Kill the spotter tmux window and its process tree (dev servers, etc.)
	windowName := fmt.Sprintf("spot-%s", climb.ID)
	tr.killPaneProcessTree(windowName)
	time.Sleep(300 * time.Millisecond) // grace period for SIGTERM handlers
	tr.tmux.KillWindow(tr.tmuxSession, windowName)
	delete(tr.startedAt, climb.ID)

	// Remove SPOT.json so it's not picked up again
	os.Remove(spotPath)

	if spot.Pass {
		// Mark climb complete
		tr.dag.MarkComplete(climb.ID)
		if err := tr.store.UpdateClimbStatus(climb.ID, model.ClimbStatusComplete); err != nil {
			return &spot, true, fmt.Errorf("updating climb status to complete: %w", err)
		}

		if err := tr.store.InsertEvent(tr.task.ID, climb.ID, model.EventClimbCompleted,
			string(payload)); err != nil {
			log.Printf("warning: failed to insert climb_completed event: %v", err)
		}

		// Check log rotation
		tr.logMgr.CheckRotation(tr.task.ID, climb.ID)

		log.Printf("spotter: climb %s passed validation", climb.ID)
	} else {
		// Mark climb failed for retry
		tr.dag.MarkFailed(climb.ID)
		if err := tr.store.UpdateClimbStatus(climb.ID, model.ClimbStatusFailed); err != nil {
			return &spot, true, fmt.Errorf("updating climb status to failed: %w", err)
		}

		// Increment attempt for retry tracking
		if err := tr.store.IncrementClimbAttempt(climb.ID); err != nil {
			log.Printf("warning: failed to increment climb attempt: %v", err)
		}
		if dagClimb := tr.dag.Get(climb.ID); dagClimb != nil {
			dagClimb.Attempt++
		}

		if err := tr.store.InsertEvent(tr.task.ID, climb.ID, model.EventClimbFailed,
			string(payload)); err != nil {
			log.Printf("warning: failed to insert climb_failed event: %v", err)
		}

		// Remove TOP.json so the retry starts fresh
		os.Remove(filepath.Join(worktreePath, ".lead", climb.ID, "TOP.json"))

		log.Printf("spotter: climb %s failed validation with %d issues", climb.ID, len(spot.Issues))
	}

	return &spot, true, nil
}

// SpawnAnchor creates a tmux window for the anchor agent and launches it.
func (tr *ProblemRunner) SpawnAnchor() error {
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

	if err := climbctx.WriteClimbJSON(tr.problemDir, "anchor", climbctx.AnchorClimb{
		Role:        "anchor",
		ProblemSpec: tr.task.Spec,
		RepoDiffs:   tr.GatherDiffs(),
		Summaries:   tr.GatherSummaries(),
	}); err != nil {
		return fmt.Errorf("writing anchor GOAL.json: %w", err)
	}

	// Set mail address for the anchor agent
	anchorMailAddr := fmt.Sprintf("problem/%s/anchor", tr.task.ID)

	// Spawn agent
	if err := tr.spawner.Spawn(context.Background(), lead.SpawnOpts{
		TmuxSession:        tr.tmuxSession,
		WindowName:         windowName,
		WorkDir:            tr.problemDir,
		AppendSystemPrompt: string(appendPrompt),
		InitialPrompt:      "Read .lead/anchor/GOAL.json and begin cross-repo review.",
		Env:                map[string]string{"BELAYER_MAIL_ADDRESS": anchorMailAddr},
	}); err != nil {
		return fmt.Errorf("spawning anchor agent: %w", err)
	}

	tr.anchorRunning = true

	if err := tr.store.InsertEvent(tr.task.ID, "", model.EventAnchorSpawned,
		fmt.Sprintf(`{"attempt":%d}`, tr.anchorAttempt)); err != nil {
		log.Printf("warning: failed to insert anchor_spawned event: %v", err)
	}

	log.Printf("anchor: spawned for problem %s (attempt %d)", tr.task.ID, tr.anchorAttempt)
	return nil
}

// CheckAnchorVerdict checks for a VERDICT.json file and parses it.
// Returns the verdict, whether one was found, and any error.
func (tr *ProblemRunner) CheckAnchorVerdict() (*anchor.VerdictJSON, bool, error) {
	verdictPath := filepath.Join(tr.problemDir, "VERDICT.json")
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
		ProblemID: tr.task.ID,
		Attempt:   tr.anchorAttempt,
		Verdict:   verdict.Verdict,
		Output:    string(data),
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

	log.Printf("anchor: verdict for problem %s: %s", tr.task.ID, verdict.Verdict)
	return &verdict, true, nil
}

// HandleApproval creates PRs for all repos after anchor approval.
func (tr *ProblemRunner) HandleApproval() error {
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
func (tr *ProblemRunner) createPR(repoName, worktreePath string) (string, error) {
	// Push the branch
	branchName := fmt.Sprintf("belayer/problem-%s/%s", tr.task.ID, repoName)

	if _, err := tr.git.Run(worktreePath, "push", "-u", "origin", "HEAD:"+branchName); err != nil {
		return "", fmt.Errorf("pushing branch: %w", err)
	}

	// Create PR via gh CLI
	title := fmt.Sprintf("[belayer] Problem %s: %s", tr.task.ID, repoName)
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

// HandleRejection creates correction climbs for failing repos and prepares for new leads.
func (tr *ProblemRunner) HandleRejection(verdict *anchor.VerdictJSON) ([]QueuedClimb, error) {
	var correctionClimbs []model.Climb
	var queued []QueuedClimb

	for repoName, rv := range verdict.Repos {
		if rv.Status != "fail" {
			continue
		}

		// Remove old TOP.json files from the failing repo's worktree
		worktreePath, ok := tr.worktrees[repoName]
		if ok {
			for _, climb := range tr.dag.Climbs() {
				if climb.RepoName == repoName {
					os.Remove(filepath.Join(worktreePath, ".lead", climb.ID, "TOP.json"))
				}
			}
		}

		// Create correction climbs
		for i, goalDesc := range rv.Goals {
			climbID := fmt.Sprintf("%s-corr-%d-%d", repoName, tr.anchorAttempt, i+1)
			c := model.Climb{
				ID:          climbID,
				ProblemID:   tr.task.ID,
				RepoName:    repoName,
				Description: goalDesc,
				DependsOn:   []string{},
				Status:      model.ClimbStatusPending,
			}
			correctionClimbs = append(correctionClimbs, c)
			queued = append(queued, QueuedClimb{Goal: c, TaskID: tr.task.ID})
		}
	}

	if len(correctionClimbs) == 0 {
		return nil, nil
	}

	// Insert correction climbs into SQLite
	if err := tr.store.InsertClimbs(correctionClimbs); err != nil {
		return nil, fmt.Errorf("inserting correction climbs: %w", err)
	}

	// Add to DAG
	tr.dag.AddClimbs(correctionClimbs)

	log.Printf("anchor: created %d correction climbs for problem %s", len(correctionClimbs), tr.task.ID)
	return queued, nil
}

// AnchorAttempt returns the current anchor review attempt count.
func (tr *ProblemRunner) AnchorAttempt() int {
	return tr.anchorAttempt
}

// AnchorRunning returns whether the anchor is currently active.
func (tr *ProblemRunner) AnchorRunning() bool {
	return tr.anchorRunning
}

// IsSingleRepo returns true if the problem involves only one repository,
// meaning cross-repo anchor review can be skipped.
func (tr *ProblemRunner) IsSingleRepo() bool {
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
