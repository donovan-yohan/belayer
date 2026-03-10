package belayer

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

// Config holds configuration for the belayer daemon.
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

// Belayer is the daemon that polls SQLite for pending problems and manages their lifecycle.
type Belayer struct {
	config     Config
	belayerCfg *belayerconfig.Config
	store      *store.Store
	tmux       tmux.TmuxManager
	logMgr     *logmgr.LogManager
	spawner    lead.AgentSpawner

	// Config directories for prompt/profile resolution.
	globalConfigDir   string
	instanceConfigDir string

	problems    map[string]*ProblemRunner // problemID -> runner
	leadQueue   []QueuedClimb             // FIFO queue
	activeLeads int
}

// New creates a new Belayer with the given configuration.
func New(cfg Config, bcfg *belayerconfig.Config, globalCfgDir, instanceCfgDir string, db *sql.DB, tm tmux.TmuxManager, sp lead.AgentSpawner) *Belayer {
	return &Belayer{
		config:            cfg,
		belayerCfg:        bcfg,
		globalConfigDir:   globalCfgDir,
		instanceConfigDir: instanceCfgDir,
		store:             store.New(db),
		tmux:              tm,
		logMgr:            logmgr.New(cfg.InstanceDir + "/logs"),
		spawner:           sp,
		problems:          make(map[string]*ProblemRunner),
	}
}

// Run starts the setter event loop. It blocks until the context is cancelled.
func (s *Belayer) Run(ctx context.Context) error {
	log.Printf("setter: starting for instance %q (max-leads=%d, poll=%s, stale=%s)",
		s.config.InstanceName, s.config.MaxLeads, s.config.PollInterval, s.config.StaleTimeout)

	// Crash recovery: resume any running/reviewing problems
	if err := s.recover(); err != nil {
		log.Printf("setter: recovery error: %v", err)
	}

	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("setter: shutting down")
			for taskID, runner := range s.problems {
				log.Printf("setter: cleaning up problem %s", taskID)
				runner.Cleanup()
			}
			log.Printf("setter: cleaned up %d problem(s)", len(s.problems))
			return ctx.Err()
		case <-ticker.C:
			if err := s.tick(); err != nil {
				log.Printf("setter: tick error: %v", err)
			}
		}
	}
}

// tick performs one iteration of the event loop.
func (s *Belayer) tick() error {
	// 1. Poll for new pending problems
	if err := s.pollPendingProblems(); err != nil {
		return fmt.Errorf("polling pending problems: %w", err)
	}

	// 2. Process each active problem
	for taskID, runner := range s.problems {
		// Handle problem based on current status
		taskStatus := runner.task.Status

		if taskStatus == model.ProblemStatusRunning {
			// Check for completed climbs (may transition to spotting if validation enabled)
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

			// Check repo-level spotter results for SPOT.json.
			// Spotters do not occupy active lead slots, so resolvedCount is not
			// subtracted from activeLeads.
			_, spotReady, spotRetry, spotErr := runner.CheckRepoSpotResults()
			if spotErr != nil {
				log.Printf("setter: error checking spotting climbs for %s: %v", taskID, spotErr)
			} else {
				s.leadQueue = append(s.leadQueue, spotReady...)
				s.leadQueue = append(s.leadQueue, spotRetry...)
			}

			// Check for stale climbs
			retryClimbs, err := runner.CheckStaleClimbs(s.config.StaleTimeout)
			if err != nil {
				log.Printf("setter: error checking stale climbs for %s: %v", taskID, err)
				continue
			}
			s.leadQueue = append(s.leadQueue, retryClimbs...)

			// Check if all climbs are complete -> transition to reviewing
			if runner.AllClimbsComplete() {
				log.Printf("setter: all climbs complete for problem %s — transitioning to reviewing", taskID)
				if err := s.store.UpdateProblemStatus(taskID, model.ProblemStatusReviewing); err != nil {
					log.Printf("setter: error updating problem status: %v", err)
				}
				runner.task.Status = model.ProblemStatusReviewing
				// Anchor will be spawned on next tick when we handle reviewing
				continue
			}

			// Check if problem is stuck (climbs failed at max attempts)
			if runner.HasStuckClimbs() {
				log.Printf("setter: problem %s has stuck climbs — marking stuck", taskID)
				if err := s.store.UpdateProblemStatus(taskID, model.ProblemStatusStuck); err != nil {
					log.Printf("setter: error updating problem status: %v", err)
				}
				runner.Cleanup()
				delete(s.problems, taskID)
				continue
			}
		}

		if taskStatus == model.ProblemStatusReviewing {
			// Skip anchor review for single-repo problems — no cross-repo alignment to check
			if runner.IsSingleRepo() {
				log.Printf("setter: single-repo problem %s — skipping anchor, creating PR", taskID)
				if err := runner.HandleApproval(); err != nil {
					log.Printf("setter: error creating PRs for %s: %v", taskID, err)
				}
				if err := s.store.UpdateProblemStatus(taskID, model.ProblemStatusComplete); err != nil {
					log.Printf("setter: error completing problem: %v", err)
				}
				runner.Cleanup()
				delete(s.problems, taskID)
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
				if err := s.store.UpdateProblemStatus(taskID, model.ProblemStatusComplete); err != nil {
					log.Printf("setter: error completing problem: %v", err)
				}
				runner.Cleanup()
				delete(s.problems, taskID)
				continue
			}

			// Verdict is reject
			if runner.AnchorAttempt() >= 2 {
				log.Printf("setter: problem %s stuck after %d anchor reviews", taskID, runner.AnchorAttempt())
				if err := s.store.UpdateProblemStatus(taskID, model.ProblemStatusStuck); err != nil {
					log.Printf("setter: error marking problem stuck: %v", err)
				}
				runner.Cleanup()
				delete(s.problems, taskID)
				continue
			}

			// Create correction climbs and go back to running
			correctionClimbs, err := runner.HandleRejection(verdict)
			if err != nil {
				log.Printf("setter: error handling rejection for %s: %v", taskID, err)
				continue
			}

			if err := s.store.UpdateProblemStatus(taskID, model.ProblemStatusRunning); err != nil {
				log.Printf("setter: error updating problem status: %v", err)
			}
			runner.task.Status = model.ProblemStatusRunning
			s.leadQueue = append(s.leadQueue, correctionClimbs...)
			log.Printf("setter: problem %s back to running with %d correction climbs", taskID, len(correctionClimbs))
		}
	}

	// 3. Process lead queue
	s.processLeadQueue()

	return nil
}

// pollPendingProblems picks up new pending problems and initializes them.
func (s *Belayer) pollPendingProblems() error {
	pending, err := s.store.GetPendingProblems(s.config.InstanceName)
	if err != nil {
		return err
	}

	for i := range pending {
		task := &pending[i]
		if _, exists := s.problems[task.ID]; exists {
			continue
		}

		log.Printf("setter: initializing problem %s", task.ID)

		runner := NewProblemRunner(task, s.config.InstanceDir, s.globalConfigDir, s.instanceConfigDir, s.store, s.tmux, s.logMgr, s.spawner)
		readyClimbs, err := runner.Init()
		if err != nil {
			log.Printf("setter: error initializing problem %s: %v", task.ID, err)
			s.store.UpdateProblemStatus(task.ID, model.ProblemStatusStuck)
			continue
		}

		s.problems[task.ID] = runner
		s.leadQueue = append(s.leadQueue, readyClimbs...)
	}

	return nil
}

// processLeadQueue spawns leads from the queue up to maxLeads.
func (s *Belayer) processLeadQueue() {
	for len(s.leadQueue) > 0 && s.activeLeads < s.config.MaxLeads {
		queued := s.leadQueue[0]
		s.leadQueue = s.leadQueue[1:]

		runner, ok := s.problems[queued.TaskID]
		if !ok {
			continue // problem was cleaned up
		}

		if err := runner.SpawnClimb(queued); err != nil {
			log.Printf("setter: error spawning climb %s: %v", queued.Goal.ID, err)
			continue
		}

		s.activeLeads++
		log.Printf("setter: spawned climb %s (active leads: %d/%d)", queued.Goal.ID, s.activeLeads, s.config.MaxLeads)
	}
}

// recover attempts to resume problems that were running when the setter last crashed.
func (s *Belayer) recover() error {
	active, err := s.store.GetActiveProblems(s.config.InstanceName)
	if err != nil {
		return fmt.Errorf("getting active problems: %w", err)
	}

	for i := range active {
		task := &active[i]
		log.Printf("setter: recovering problem %s (status=%s)", task.ID, task.Status)

		runner := NewProblemRunner(task, s.config.InstanceDir, s.globalConfigDir, s.instanceConfigDir, s.store, s.tmux, s.logMgr, s.spawner)

		// Load climbs and build DAG (skip worktree creation since they should exist)
		climbs, err := s.store.GetClimbsForProblem(task.ID)
		if err != nil {
			log.Printf("setter: error loading climbs for %s: %v", task.ID, err)
			continue
		}

		runner.dag = BuildDAG(climbs)
		runner.tmuxSession = fmt.Sprintf("belayer-problem-%s", task.ID)
		runner.problemDir = fmt.Sprintf("%s/tasks/%s", s.config.InstanceDir, task.ID)

		// Populate worktrees map
		repos := make(map[string]bool)
		for _, climb := range climbs {
			repos[climb.RepoName] = true
		}
		for repoName := range repos {
			worktreePath := fmt.Sprintf("%s/tasks/%s/%s", s.config.InstanceDir, task.ID, repoName)
			runner.worktrees[repoName] = worktreePath
		}

		// Check for TOP.json files that completed while we were down
		if _, _, err := runner.CheckCompletions(); err != nil {
			log.Printf("setter: error checking completions during recovery for %s: %v", task.ID, err)
		}

		s.problems[task.ID] = runner

		// Queue any climbs that are ready (pending with deps met)
		readyClimbs := runner.dag.ReadyClimbs()
		for _, climb := range readyClimbs {
			s.leadQueue = append(s.leadQueue, QueuedClimb{Goal: climb, TaskID: task.ID})
		}
	}

	if len(active) > 0 {
		log.Printf("setter: recovered %d problem(s)", len(active))
	}

	return nil
}

// GoalCount returns the count of leads being managed for a climb ID check.
func (s *Belayer) GoalCount(climbID string) int {
	return s.activeLeads
}

// parseClimbsJSON parses the climbs_json field of a problem into a ClimbsFile.
func parseClimbsJSON(climbsJSON string) (*model.ClimbsFile, error) {
	var cf model.ClimbsFile
	if err := json.Unmarshal([]byte(climbsJSON), &cf); err != nil {
		return nil, fmt.Errorf("parsing climbs JSON: %w", err)
	}
	return &cf, nil
}
