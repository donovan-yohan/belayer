package setter

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/donovan-yohan/belayer/internal/belayerconfig"
	"github.com/donovan-yohan/belayer/internal/lead"
	"github.com/donovan-yohan/belayer/internal/logmgr"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/donovan-yohan/belayer/internal/tmux"
)

// Config holds configuration for the setter daemon.
type Config struct {
	InstanceName string
	InstanceDir  string
	MaxLeads     int
	PollInterval time.Duration
	StaleTimeout time.Duration
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxLeads:     8,
		PollInterval: 5 * time.Second,
		StaleTimeout: 30 * time.Minute,
	}
}

// Setter is the daemon that polls SQLite for pending tasks and manages their lifecycle.
type Setter struct {
	config     Config
	belayerCfg *belayerconfig.Config
	store      *store.Store
	tmux       tmux.TmuxManager
	logMgr     *logmgr.LogManager
	spawner    lead.AgentSpawner

	// Config directories for prompt/profile resolution.
	globalConfigDir   string
	instanceConfigDir string

	tasks       map[string]*TaskRunner // taskID -> runner
	leadQueue   []QueuedGoal           // FIFO queue
	activeLeads int
}

// New creates a new Setter with the given configuration.
func New(cfg Config, bcfg *belayerconfig.Config, globalCfgDir, instanceCfgDir string, db *sql.DB, tm tmux.TmuxManager, sp lead.AgentSpawner) *Setter {
	return &Setter{
		config:            cfg,
		belayerCfg:        bcfg,
		globalConfigDir:   globalCfgDir,
		instanceConfigDir: instanceCfgDir,
		store:             store.New(db),
		tmux:              tm,
		logMgr:            logmgr.New(cfg.InstanceDir + "/logs"),
		spawner:           sp,
		tasks:             make(map[string]*TaskRunner),
	}
}

// Run starts the setter event loop. It blocks until the context is cancelled.
func (s *Setter) Run(ctx context.Context) error {
	log.Printf("setter: starting for instance %q (max-leads=%d, poll=%s, stale=%s)",
		s.config.InstanceName, s.config.MaxLeads, s.config.PollInterval, s.config.StaleTimeout)

	// Crash recovery: resume any running/reviewing tasks
	if err := s.recover(); err != nil {
		log.Printf("setter: recovery error: %v", err)
	}

	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("setter: shutting down")
			for taskID, runner := range s.tasks {
				log.Printf("setter: cleaning up task %s", taskID)
				runner.Cleanup()
			}
			log.Printf("setter: cleaned up %d task(s)", len(s.tasks))
			return ctx.Err()
		case <-ticker.C:
			if err := s.tick(); err != nil {
				log.Printf("setter: tick error: %v", err)
			}
		}
	}
}

// tick performs one iteration of the event loop.
func (s *Setter) tick() error {
	// 1. Poll for new pending tasks
	if err := s.pollPendingTasks(); err != nil {
		return fmt.Errorf("polling pending tasks: %w", err)
	}

	// 2. Process each active task
	for taskID, runner := range s.tasks {
		// Handle task based on current status
		taskStatus := runner.task.Status

		if taskStatus == model.TaskStatusRunning {
			// Check for completed goals (may transition to spotting if validation enabled)
			newlyReady, completedCount, err := runner.CheckCompletions()
			if err != nil {
				log.Printf("setter: error checking completions for %s: %v", taskID, err)
				continue
			}
			s.activeLeads -= completedCount
			if s.activeLeads < 0 {
				s.activeLeads = 0
			}
			s.leadQueue = append(s.leadQueue, newlyReady...)

			// Check spotting goals for SPOT.json results
			spotResolved, spotReady, spotRetry, spotErr := runner.CheckSpottingGoals()
			if spotErr != nil {
				log.Printf("setter: error checking spotting goals for %s: %v", taskID, spotErr)
			} else {
				s.activeLeads -= spotResolved
				if s.activeLeads < 0 {
					s.activeLeads = 0
				}
				s.leadQueue = append(s.leadQueue, spotReady...)
				s.leadQueue = append(s.leadQueue, spotRetry...)
			}

			// Check for stale goals
			retryGoals, err := runner.CheckStaleGoals(s.config.StaleTimeout)
			if err != nil {
				log.Printf("setter: error checking stale goals for %s: %v", taskID, err)
				continue
			}
			s.leadQueue = append(s.leadQueue, retryGoals...)

			// Check if all goals are complete -> transition to reviewing
			if runner.AllGoalsComplete() {
				log.Printf("setter: all goals complete for task %s — transitioning to reviewing", taskID)
				if err := s.store.UpdateTaskStatus(taskID, model.TaskStatusReviewing); err != nil {
					log.Printf("setter: error updating task status: %v", err)
				}
				runner.task.Status = model.TaskStatusReviewing
				// Anchor will be spawned on next tick when we handle reviewing
				continue
			}

			// Check if task is stuck (goals failed at max attempts)
			if runner.HasStuckGoals() {
				log.Printf("setter: task %s has stuck goals — marking stuck", taskID)
				if err := s.store.UpdateTaskStatus(taskID, model.TaskStatusStuck); err != nil {
					log.Printf("setter: error updating task status: %v", err)
				}
				runner.Cleanup()
				delete(s.tasks, taskID)
				continue
			}
		}

		if taskStatus == model.TaskStatusReviewing {
			// Skip anchor review for single-repo tasks — no cross-repo alignment to check
			if runner.IsSingleRepo() {
				log.Printf("setter: single-repo task %s — skipping anchor, creating PR", taskID)
				if err := runner.HandleApproval(); err != nil {
					log.Printf("setter: error creating PRs for %s: %v", taskID, err)
				}
				if err := s.store.UpdateTaskStatus(taskID, model.TaskStatusComplete); err != nil {
					log.Printf("setter: error completing task: %v", err)
				}
				runner.Cleanup()
				delete(s.tasks, taskID)
				continue
			}

			// Multi-repo: spawn anchor for cross-repo alignment review
			if !runner.AnchorRunning() {
				if err := runner.SpawnAnchor(); err != nil {
					log.Printf("setter: error spawning anchor for %s: %v", taskID, err)
					continue
				}
				continue
			}

			// Check for anchor verdict
			verdict, found, err := runner.CheckAnchorVerdict()
			if err != nil {
				log.Printf("setter: error checking anchor verdict for %s: %v", taskID, err)
				continue
			}
			if !found {
				continue
			}

			if verdict.Verdict == "approve" {
				// Create PRs for all repos
				if err := runner.HandleApproval(); err != nil {
					log.Printf("setter: error creating PRs for %s: %v", taskID, err)
				}
				if err := s.store.UpdateTaskStatus(taskID, model.TaskStatusComplete); err != nil {
					log.Printf("setter: error completing task: %v", err)
				}
				runner.Cleanup()
				delete(s.tasks, taskID)
				continue
			}

			// Verdict is reject
			if runner.AnchorAttempt() >= 2 {
				log.Printf("setter: task %s stuck after %d anchor reviews", taskID, runner.AnchorAttempt())
				if err := s.store.UpdateTaskStatus(taskID, model.TaskStatusStuck); err != nil {
					log.Printf("setter: error marking task stuck: %v", err)
				}
				runner.Cleanup()
				delete(s.tasks, taskID)
				continue
			}

			// Create correction goals and go back to running
			correctionGoals, err := runner.HandleRejection(verdict)
			if err != nil {
				log.Printf("setter: error handling rejection for %s: %v", taskID, err)
				continue
			}

			if err := s.store.UpdateTaskStatus(taskID, model.TaskStatusRunning); err != nil {
				log.Printf("setter: error updating task status: %v", err)
			}
			runner.task.Status = model.TaskStatusRunning
			s.leadQueue = append(s.leadQueue, correctionGoals...)
			log.Printf("setter: task %s back to running with %d correction goals", taskID, len(correctionGoals))
		}
	}

	// 3. Process lead queue
	s.processLeadQueue()

	return nil
}

// pollPendingTasks picks up new pending tasks and initializes them.
func (s *Setter) pollPendingTasks() error {
	pending, err := s.store.GetPendingTasks(s.config.InstanceName)
	if err != nil {
		return err
	}

	for i := range pending {
		task := &pending[i]
		if _, exists := s.tasks[task.ID]; exists {
			continue
		}

		log.Printf("setter: initializing task %s", task.ID)

		runner := NewTaskRunner(task, s.config.InstanceDir, s.globalConfigDir, s.instanceConfigDir, s.store, s.tmux, s.logMgr, s.spawner)
		readyGoals, err := runner.Init()
		if err != nil {
			log.Printf("setter: error initializing task %s: %v", task.ID, err)
			s.store.UpdateTaskStatus(task.ID, model.TaskStatusStuck)
			continue
		}

		s.tasks[task.ID] = runner
		s.leadQueue = append(s.leadQueue, readyGoals...)
	}

	return nil
}

// processLeadQueue spawns leads from the queue up to maxLeads.
func (s *Setter) processLeadQueue() {
	for len(s.leadQueue) > 0 && s.activeLeads < s.config.MaxLeads {
		queued := s.leadQueue[0]
		s.leadQueue = s.leadQueue[1:]

		runner, ok := s.tasks[queued.TaskID]
		if !ok {
			continue // task was cleaned up
		}

		if err := runner.SpawnGoal(queued); err != nil {
			log.Printf("setter: error spawning goal %s: %v", queued.Goal.ID, err)
			continue
		}

		s.activeLeads++
		log.Printf("setter: spawned goal %s (active leads: %d/%d)", queued.Goal.ID, s.activeLeads, s.config.MaxLeads)
	}
}

// recover attempts to resume tasks that were running when the setter last crashed.
func (s *Setter) recover() error {
	active, err := s.store.GetActiveTasks(s.config.InstanceName)
	if err != nil {
		return fmt.Errorf("getting active tasks: %w", err)
	}

	for i := range active {
		task := &active[i]
		log.Printf("setter: recovering task %s (status=%s)", task.ID, task.Status)

		runner := NewTaskRunner(task, s.config.InstanceDir, s.globalConfigDir, s.instanceConfigDir, s.store, s.tmux, s.logMgr, s.spawner)

		// Load goals and build DAG (skip worktree creation since they should exist)
		goals, err := s.store.GetGoalsForTask(task.ID)
		if err != nil {
			log.Printf("setter: error loading goals for %s: %v", task.ID, err)
			continue
		}

		runner.dag = BuildDAG(goals)
		runner.tmuxSession = fmt.Sprintf("belayer-task-%s", task.ID)
		runner.taskDir = fmt.Sprintf("%s/tasks/%s", s.config.InstanceDir, task.ID)

		// Populate worktrees map
		repos := make(map[string]bool)
		for _, g := range goals {
			repos[g.RepoName] = true
		}
		for repoName := range repos {
			worktreePath := fmt.Sprintf("%s/tasks/%s/%s", s.config.InstanceDir, task.ID, repoName)
			runner.worktrees[repoName] = worktreePath
		}

		// Check for DONE.json files that completed while we were down
		if _, _, err := runner.CheckCompletions(); err != nil {
			log.Printf("setter: error checking completions during recovery for %s: %v", task.ID, err)
		}

		s.tasks[task.ID] = runner

		// Queue any goals that are ready (pending with deps met)
		readyGoals := runner.dag.ReadyGoals()
		for _, g := range readyGoals {
			s.leadQueue = append(s.leadQueue, QueuedGoal{Goal: g, TaskID: task.ID})
		}
	}

	if len(active) > 0 {
		log.Printf("setter: recovered %d task(s)", len(active))
	}

	return nil
}

// GoalCount returns the count of leads being managed for a goal ID check.
func (s *Setter) GoalCount(goalID string) int {
	return s.activeLeads
}

// parseGoalsJSON parses the goals_json field of a task into a GoalsFile.
func parseGoalsJSON(goalsJSON string) (*model.GoalsFile, error) {
	var gf model.GoalsFile
	if err := json.Unmarshal([]byte(goalsJSON), &gf); err != nil {
		return nil, fmt.Errorf("parsing goals JSON: %w", err)
	}
	return &gf, nil
}
