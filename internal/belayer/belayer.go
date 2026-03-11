package belayer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/belayerconfig"
	"github.com/donovan-yohan/belayer/internal/instance"
	"github.com/donovan-yohan/belayer/internal/lead"
	"github.com/donovan-yohan/belayer/internal/logmgr"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/review"
	"github.com/donovan-yohan/belayer/internal/scm"
	ghscm "github.com/donovan-yohan/belayer/internal/scm/github"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/donovan-yohan/belayer/internal/tmux"
	"github.com/donovan-yohan/belayer/internal/tracker"
	ghtracker "github.com/donovan-yohan/belayer/internal/tracker/github"
	jiratracker "github.com/donovan-yohan/belayer/internal/tracker/jira"
)

// Config holds configuration for the belayer daemon.
type Config struct {
	CragName     string
	CragDir      string
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
	globalConfigDir string
	cragConfigDir   string

	// Tracker sync state.
	tracker      tracker.Tracker
	lastSyncAt   time.Time
	syncInterval time.Duration

	// SCM provider and PR monitoring state.
	scm            scm.SCMProvider
	lastPRPollAt   time.Time
	prPollInterval time.Duration
	reviewCfg      belayerconfig.ReviewConfig

	problems    map[string]*ProblemRunner // problemID -> runner
	leadQueue   []QueuedClimb             // FIFO queue
	activeLeads int
}

// New creates a new Belayer with the given configuration.
func New(cfg Config, bcfg *belayerconfig.Config, globalCfgDir, cragCfgDir string, db *sql.DB, tm tmux.TmuxManager, sp lead.AgentSpawner) *Belayer {
	return &Belayer{
		config:          cfg,
		belayerCfg:      bcfg,
		globalConfigDir: globalCfgDir,
		cragConfigDir:   cragCfgDir,
		store:           store.New(db),
		tmux:            tm,
		logMgr:          logmgr.New(cfg.CragDir + "/logs"),
		spawner:         sp,
		problems:        make(map[string]*ProblemRunner),
	}
}

// Run starts the belayer event loop. It blocks until the context is cancelled.
func (s *Belayer) Run(ctx context.Context) error {
	log.Printf("belayer: starting for crag %q (max-leads=%d, poll=%s, stale=%s)",
		s.config.CragName, s.config.MaxLeads, s.config.PollInterval, s.config.StaleTimeout)

	if err := s.initTracker(); err != nil {
		log.Printf("belayer: tracker init error: %v", err)
	}

	s.initSCM()

	// Crash recovery: resume any running/reviewing problems
	if err := s.recover(); err != nil {
		log.Printf("belayer: recovery error: %v", err)
	}

	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("belayer: shutting down")
			for taskID, runner := range s.problems {
				log.Printf("belayer: cleaning up problem %s", taskID)
				runner.Cleanup()
			}
			log.Printf("belayer: cleaned up %d problem(s)", len(s.problems))
			return ctx.Err()
		case <-ticker.C:
			if err := s.tick(ctx); err != nil {
				log.Printf("belayer: tick error: %v", err)
			}
		}
	}
}

// tick performs one iteration of the event loop.
func (s *Belayer) tick(ctx context.Context) error {
	// 0. Sync tracker issues.
	s.syncTracker(ctx)

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
				log.Printf("belayer: error checking completions for %s: %v", taskID, err)
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
				log.Printf("belayer: error checking spotting climbs for %s: %v", taskID, spotErr)
			} else {
				s.leadQueue = append(s.leadQueue, spotReady...)
				s.leadQueue = append(s.leadQueue, spotRetry...)
			}

			// Check for stale climbs
			retryClimbs, err := runner.CheckStaleClimbs(s.config.StaleTimeout)
			if err != nil {
				log.Printf("belayer: error checking stale climbs for %s: %v", taskID, err)
				continue
			}
			s.leadQueue = append(s.leadQueue, retryClimbs...)

			// Check if all climbs are complete -> transition to reviewing
			if runner.AllClimbsComplete() {
				if runner.IsFullyFlashed() {
					log.Printf("belayer: problem %s was FULLY FLASHED! Every repo topped first try.", taskID)
				}
				log.Printf("belayer: all climbs complete for problem %s — transitioning to reviewing", taskID)
				if err := s.store.UpdateProblemStatus(taskID, model.ProblemStatusReviewing); err != nil {
					log.Printf("belayer: error updating problem status: %v", err)
				}
				runner.task.Status = model.ProblemStatusReviewing
				// Anchor will be spawned on next tick when we handle reviewing
				continue
			}

			// Check if problem is stuck (climbs failed at max attempts)
			if runner.HasStuckClimbs() {
				log.Printf("belayer: problem %s has stuck climbs — marking stuck", taskID)
				if err := s.store.UpdateProblemStatus(taskID, model.ProblemStatusStuck); err != nil {
					log.Printf("belayer: error updating problem status: %v", err)
				}
				runner.Cleanup()
				delete(s.problems, taskID)
				continue
			}
		}

		if taskStatus == model.ProblemStatusReviewing {
			// Skip anchor review for single-repo problems — no cross-repo alignment to check
			if runner.IsSingleRepo() {
				log.Printf("belayer: single-repo problem %s — skipping anchor, creating PR", taskID)
				if err := runner.HandleApproval(ctx); err != nil {
					log.Printf("belayer: error creating PRs for %s: %v", taskID, err)
				}
				runner.Cleanup()
				delete(s.problems, taskID)
				continue
			}

			// Multi-repo: spawn anchor for cross-repo alignment review
			if !runner.AnchorRunning() {
				if err := runner.SpawnAnchor(); err != nil {
					log.Printf("belayer: error spawning anchor for %s: %v", taskID, err)
					continue
				}
				continue
			}

			// Check for anchor verdict
			verdict, found, err := runner.CheckAnchorVerdict()
			if err != nil {
				log.Printf("belayer: error checking anchor verdict for %s: %v", taskID, err)
				continue
			}
			if !found {
				continue
			}

			if verdict.Verdict == "approve" {
				// Create PRs for all repos
				if err := runner.HandleApproval(ctx); err != nil {
					log.Printf("belayer: error creating PRs for %s: %v", taskID, err)
				}
				runner.Cleanup()
				delete(s.problems, taskID)
				continue
			}

			// Verdict is reject
			if runner.AnchorAttempt() >= 2 {
				log.Printf("belayer: problem %s stuck after %d anchor reviews", taskID, runner.AnchorAttempt())
				if err := s.store.UpdateProblemStatus(taskID, model.ProblemStatusStuck); err != nil {
					log.Printf("belayer: error marking problem stuck: %v", err)
				}
				runner.Cleanup()
				delete(s.problems, taskID)
				continue
			}

			// Create correction climbs and go back to running
			correctionClimbs, err := runner.HandleRejection(verdict)
			if err != nil {
				log.Printf("belayer: error handling rejection for %s: %v", taskID, err)
				continue
			}

			if err := s.store.UpdateProblemStatus(taskID, model.ProblemStatusRunning); err != nil {
				log.Printf("belayer: error updating problem status: %v", err)
			}
			runner.task.Status = model.ProblemStatusRunning
			s.leadQueue = append(s.leadQueue, correctionClimbs...)
			log.Printf("belayer: problem %s back to running with %d correction climbs", taskID, len(correctionClimbs))
		}
	}

	// 3. Monitor open pull requests.
	s.monitorPRs(ctx)

	// 4. Handle imported problems — transition to enriching (spec assembly)
	for _, runner := range s.problems {
		if runner.task.Status == model.ProblemStatusImported {
			log.Printf("belayer: problem %s imported, transitioning to enriching", runner.task.ID)
			if err := s.store.UpdateProblemStatus(runner.task.ID, model.ProblemStatusEnriching); err != nil {
				log.Printf("belayer: error transitioning problem %s to enriching: %v", runner.task.ID, err)
			}
			// TODO: spawn spec assembly agentic node (future work)
		}
	}

	// 5. Process lead queue
	s.processLeadQueue()

	return nil
}

// pollPendingProblems picks up new pending problems and initializes them.
func (s *Belayer) pollPendingProblems() error {
	pending, err := s.store.GetPendingProblems(s.config.CragName)
	if err != nil {
		return err
	}

	for i := range pending {
		task := &pending[i]
		if _, exists := s.problems[task.ID]; exists {
			continue
		}

		log.Printf("belayer: initializing problem %s", task.ID)

		var prCfg belayerconfig.PRConfig
		if s.belayerCfg != nil {
			prCfg = s.belayerCfg.PR
		}
		runner := NewProblemRunner(task, s.config.CragDir, s.globalConfigDir, s.cragConfigDir, s.store, s.tmux, s.logMgr, s.spawner, s.scm, prCfg)
		readyClimbs, err := runner.Init()
		if err != nil {
			log.Printf("belayer: error initializing problem %s: %v", task.ID, err)
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
			log.Printf("belayer: error spawning climb %s: %v", queued.Climb.ID, err)
			continue
		}

		s.activeLeads++
		log.Printf("belayer: spawned climb %s (active leads: %d/%d)", queued.Climb.ID, s.activeLeads, s.config.MaxLeads)
	}
}

// recover attempts to resume problems that were running when the setter last crashed.
func (s *Belayer) recover() error {
	active, err := s.store.GetActiveProblems(s.config.CragName)
	if err != nil {
		return fmt.Errorf("getting active problems: %w", err)
	}

	for i := range active {
		task := &active[i]
		log.Printf("belayer: recovering problem %s (status=%s)", task.ID, task.Status)

		var prCfg belayerconfig.PRConfig
		if s.belayerCfg != nil {
			prCfg = s.belayerCfg.PR
		}
		runner := NewProblemRunner(task, s.config.CragDir, s.globalConfigDir, s.cragConfigDir, s.store, s.tmux, s.logMgr, s.spawner, s.scm, prCfg)

		// Load climbs and build DAG (skip worktree creation since they should exist)
		climbs, err := s.store.GetClimbsForProblem(task.ID)
		if err != nil {
			log.Printf("belayer: error loading climbs for %s: %v", task.ID, err)
			continue
		}

		runner.dag = BuildDAG(climbs)
		runner.tmuxSession = fmt.Sprintf("belayer-problem-%s", task.ID)
		runner.problemDir = filepath.Join(s.config.CragDir, "tasks", task.ID)

		// Populate worktrees map
		repos := make(map[string]bool)
		for _, climb := range climbs {
			repos[climb.RepoName] = true
		}
		for repoName := range repos {
			runner.worktrees[repoName] = filepath.Join(s.config.CragDir, "tasks", task.ID, repoName)
		}

		// Check for TOP.json files that completed while we were down
		if _, _, err := runner.CheckCompletions(); err != nil {
			log.Printf("belayer: error checking completions during recovery for %s: %v", task.ID, err)
		}

		// Restore spotter tracking state from SQLite events and SPOT.json files.
		// Count spotter_spawned events per repo to restore attempt counts.
		events, evtErr := s.store.GetEventsForProblem(task.ID)
		if evtErr != nil {
			log.Printf("belayer: error loading events for recovery of %s: %v", task.ID, evtErr)
		} else {
			for _, evt := range events {
				if evt.Type == model.EventSpotterSpawned {
					// Payload: {"repo":"<name>","attempt":<n>}
					// We count spawned events per repo to determine attempt count.
					// Parse the repo name from payload using simple string extraction.
					repoName := extractJSONStringField(evt.Payload, "repo")
					if repoName != "" {
						runner.repoSpotterAttempts[repoName]++
					}
				}
			}
		}

		// Determine which repos still have an active spotter (no SPOT.json yet but all climbs topped).
		for _, repoName := range runner.dag.UniqueRepos() {
			if !runner.dag.AllClimbsForRepoTopped(repoName) {
				continue
			}
			// If we have attempt records, a spotter was previously launched for this repo.
			if runner.repoSpotterAttempts[repoName] > 0 {
				// Check if SPOT.json already exists — if so, CheckRepoSpotResults will handle it.
				worktreePath := runner.worktrees[repoName]
				spotPath := filepath.Join(worktreePath, ".lead", "spotter-"+repoName, "SPOT.json")
				if _, statErr := os.Stat(spotPath); statErr == nil {
					// SPOT.json present: mark activated so CheckRepoSpotResults picks it up.
					runner.repoSpotterActivated[repoName] = true
				}
				// If no SPOT.json, the spotter may have been running when we crashed.
				// Re-activate it so the daemon waits for or retries the spotter.
				// We mark activated so the poll loop doesn't re-spawn a duplicate.
				runner.repoSpotterActivated[repoName] = true
			}
		}

		s.problems[task.ID] = runner

		// Queue any climbs that are ready (pending with deps met)
		readyClimbs := runner.dag.ReadyClimbs()
		for _, climb := range readyClimbs {
			s.leadQueue = append(s.leadQueue, QueuedClimb{Climb: climb, TaskID: task.ID})
		}

		if task.Status == model.ProblemStatusPRMonitoring {
			s.recoverPRMonitoring(task)
		}
	}

	if len(active) > 0 {
		log.Printf("belayer: recovered %d problem(s)", len(active))
	}

	return nil
}

// recoverPRMonitoring restores PR monitoring state for a problem after a crash.
func (s *Belayer) recoverPRMonitoring(problem *model.Problem) {
	prs, err := s.store.ListPullRequestsForProblem(problem.ID)
	if err != nil {
		log.Printf("recover: failed to load PRs for problem %s: %v", problem.ID, err)
		return
	}
	log.Printf("recover: restored %d monitored PRs for problem %s", len(prs), problem.ID)
}

// initTracker initialises the tracker from config. It is a no-op when no
// provider is configured.
func (s *Belayer) initTracker() error {
	if s.belayerCfg == nil || s.belayerCfg.Tracker.Provider == "" {
		return nil
	}

	intervalStr := s.belayerCfg.Tracker.SyncInterval
	if intervalStr == "" {
		intervalStr = "5m"
	}
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		return fmt.Errorf("parsing tracker sync_interval %q: %w", intervalStr, err)
	}
	s.syncInterval = interval

	switch s.belayerCfg.Tracker.Provider {
	case "github":
		cragConfig, _, err := instance.Load(s.config.CragName)
		if err != nil {
			return fmt.Errorf("loading crag for tracker: %w", err)
		}
		if len(cragConfig.Repos) == 0 {
			return fmt.Errorf("no repos in crag for github tracker")
		}
		barePath := filepath.Join(s.config.CragDir, cragConfig.Repos[0].BarePath)
		s.tracker = ghtracker.New(barePath)
	case "jira":
		token := os.Getenv("JIRA_API_TOKEN")
		s.tracker = jiratracker.New(
			s.belayerCfg.Tracker.Jira.BaseURL,
			s.belayerCfg.Tracker.Jira.Project,
			token,
		)
	default:
		return fmt.Errorf("unknown tracker provider: %q", s.belayerCfg.Tracker.Provider)
	}

	log.Printf("belayer: tracker initialised (provider=%s, sync_interval=%s)", s.belayerCfg.Tracker.Provider, s.syncInterval)
	return nil
}

// syncTracker fetches issues from the configured tracker and upserts them into
// the local SQLite store. It is a no-op when no tracker is configured or the
// sync interval has not elapsed.
func (s *Belayer) syncTracker(ctx context.Context) {
	if s.tracker == nil || s.belayerCfg == nil {
		return
	}
	if !s.lastSyncAt.IsZero() && time.Since(s.lastSyncAt) < s.syncInterval {
		return
	}

	filter := model.IssueFilter{}
	if s.belayerCfg.Tracker.Label != "" {
		filter.Labels = []string{s.belayerCfg.Tracker.Label}
	}

	issues, err := s.tracker.ListIssues(ctx, filter)
	if err != nil {
		log.Printf("belayer: tracker sync error: %v", err)
		return
	}

	now := time.Now().UTC()
	for _, issue := range issues {
		commentsJSON, _ := json.Marshal(issue.Comments)
		labelsJSON, _ := json.Marshal(issue.Labels)
		rawJSON, _ := json.Marshal(issue.Raw)

		ti := &model.TrackerIssue{
			ID:           issue.ID,
			Provider:     s.belayerCfg.Tracker.Provider,
			Title:        issue.Title,
			Body:         issue.Body,
			CommentsJSON: string(commentsJSON),
			LabelsJSON:   string(labelsJSON),
			Priority:     issue.Priority,
			Assignee:     issue.Assignee,
			URL:          issue.URL,
			RawJSON:      string(rawJSON),
			SyncedAt:     now,
		}
		if err := s.store.InsertTrackerIssue(ti); err != nil {
			log.Printf("belayer: error inserting tracker issue %s: %v", issue.ID, err)
		}
	}

	s.lastSyncAt = now
	log.Printf("belayer: synced %d tracker issues", len(issues))
}

// initSCM initialises the SCM provider from config. Always uses GitHub for now.
func (s *Belayer) initSCM() {
	if s.belayerCfg != nil {
		s.reviewCfg = s.belayerCfg.Review
	}
	s.scm = ghscm.New()

	intervalStr := s.reviewCfg.PollInterval
	if intervalStr == "" {
		intervalStr = "60s"
	}
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		log.Printf("belayer: invalid review poll_interval %q, defaulting to 60s", intervalStr)
		interval = 60 * time.Second
	}
	s.prPollInterval = interval
	log.Printf("belayer: SCM provider initialised (pr_poll_interval=%s)", s.prPollInterval)
}

// monitorPRs polls open pull requests and reacts to CI and review events.
func (s *Belayer) monitorPRs(ctx context.Context) {
	if s.scm == nil || s.belayerCfg == nil {
		return
	}
	if !s.lastPRPollAt.IsZero() && time.Since(s.lastPRPollAt) < s.prPollInterval {
		return
	}

	prs, err := s.store.ListMonitoredPullRequests(s.config.CragName)
	if err != nil {
		log.Printf("belayer: error listing monitored PRs: %v", err)
		return
	}

	maxFix := s.reviewCfg.CIFixAttempts
	autoMerge := s.reviewCfg.AutoMerge

	for i := range prs {
		pr := &prs[i]

		curr, err := s.scm.GetPRStatus(ctx, "", pr.PRNumber)
		if err != nil {
			log.Printf("belayer: error getting PR status for PR #%d (%s): %v", pr.PRNumber, pr.RepoName, err)
			continue
		}

		// Build previous status from stored fields.
		prev := &model.PRStatus{
			CIStatus: pr.CIStatus,
			State:    pr.State,
		}

		var since time.Time
		if pr.LastPolledAt != nil {
			since = *pr.LastPolledAt
		}
		activity, err := s.scm.GetNewActivity(ctx, "", pr.PRNumber, since)
		if err != nil {
			log.Printf("belayer: error getting PR activity for PR #%d: %v", pr.PRNumber, err)
			activity = nil
		}

		events := review.ClassifyActivity(prev, curr, activity)
		for _, evt := range events {
			action := review.DecideReaction(evt, pr, maxFix, autoMerge)
			s.executeReaction(ctx, pr, evt, action)
		}

		// Update stored PR status.
		if err := s.store.UpdatePullRequestCI(pr.ID, curr.CIStatus, pr.CIFixCount); err != nil {
			log.Printf("belayer: error updating PR CI status for PR #%d: %v", pr.PRNumber, err)
		}
		currReviewState := review.HighestReviewState(curr.Reviews)
		if currReviewState != pr.ReviewStatus {
			if err := s.store.UpdatePullRequestReview(pr.ID, currReviewState); err != nil {
				log.Printf("belayer: error updating PR review status for PR #%d: %v", pr.PRNumber, err)
			}
		}
		if curr.State != pr.State {
			if err := s.store.UpdatePullRequestState(pr.ID, curr.State); err != nil {
				log.Printf("belayer: error updating PR state for PR #%d: %v", pr.PRNumber, err)
			}
		}
	}

	s.lastPRPollAt = time.Now()
}

// executeReaction acts on a classified PR event.
func (s *Belayer) executeReaction(ctx context.Context, pr *model.PullRequest, evt review.ReactionEvent, action string) {
	reaction := &model.PRReaction{
		PRID:        pr.ID,
		TriggerType: string(evt.Type),
		ActionTaken: action,
	}

	switch action {
	case "lead_dispatched":
		if evt.Type == review.EventCIFailed {
			pr.CIFixCount++
			if err := s.store.UpdatePullRequestCI(pr.ID, pr.CIStatus, pr.CIFixCount); err != nil {
				log.Printf("belayer: error incrementing CI fix count for PR #%d: %v", pr.PRNumber, err)
			}
			if err := s.store.InsertEvent(pr.ProblemID, "", model.EventCIFixDispatched, fmt.Sprintf(`{"pr_number":%d,"fix_count":%d}`, pr.PRNumber, pr.CIFixCount)); err != nil {
				log.Printf("belayer: error inserting ci_fix_dispatched event: %v", err)
			}
			log.Printf("belayer: CI fix dispatched for PR #%d (attempt %d/%d)", pr.PRNumber, pr.CIFixCount, s.reviewCfg.CIFixAttempts)
		} else if evt.Type == review.EventChangesRequested {
			if err := s.store.UpdateProblemStatus(pr.ProblemID, model.ProblemStatusReviewReacting); err != nil {
				log.Printf("belayer: error updating problem status for review reaction: %v", err)
			}
			if err := s.store.InsertEvent(pr.ProblemID, "", model.EventReviewReactionDispatched, fmt.Sprintf(`{"pr_number":%d}`, pr.PRNumber)); err != nil {
				log.Printf("belayer: error inserting review_reaction_dispatched event: %v", err)
			}
			log.Printf("belayer: changes requested on PR #%d — problem %s set to review_reacting", pr.PRNumber, pr.ProblemID)
		}

	case "comment_replied":
		if err := s.store.InsertEvent(pr.ProblemID, "", model.EventReviewCommentReceived, fmt.Sprintf(`{"pr_number":%d}`, pr.PRNumber)); err != nil {
			log.Printf("belayer: error inserting review_comment_received event: %v", err)
		}
		log.Printf("belayer: new comment on PR #%d (full reply deferred)", pr.PRNumber)

	case "marked_stuck":
		if err := s.store.UpdateProblemStatus(pr.ProblemID, model.ProblemStatusStuck); err != nil {
			log.Printf("belayer: error marking problem stuck: %v", err)
		}
		if err := s.store.InsertEvent(pr.ProblemID, "", model.EventCIFixExhausted, fmt.Sprintf(`{"pr_number":%d}`, pr.PRNumber)); err != nil {
			log.Printf("belayer: error inserting ci_fix_exhausted event: %v", err)
		}
		log.Printf("belayer: PR #%d CI fix attempts exhausted — problem %s stuck", pr.PRNumber, pr.ProblemID)

	case "merge_attempted":
		if err := s.scm.Merge(ctx, "", pr.PRNumber); err != nil {
			log.Printf("belayer: error merging PR #%d: %v", pr.PRNumber, err)
		} else {
			if err := s.store.UpdatePullRequestState(pr.ID, "merged"); err != nil {
				log.Printf("belayer: error updating PR state to merged: %v", err)
			}
			if err := s.store.InsertEvent(pr.ProblemID, "", model.EventPRMerged, fmt.Sprintf(`{"pr_number":%d}`, pr.PRNumber)); err != nil {
				log.Printf("belayer: error inserting pr_merged event: %v", err)
			}
			log.Printf("belayer: PR #%d merged", pr.PRNumber)
		}

	case "status_merged":
		if err := s.store.UpdatePullRequestState(pr.ID, "merged"); err != nil {
			log.Printf("belayer: error updating PR state to merged: %v", err)
		}
		if err := s.store.InsertEvent(pr.ProblemID, "", model.EventPRMerged, fmt.Sprintf(`{"pr_number":%d}`, pr.PRNumber)); err != nil {
			log.Printf("belayer: error inserting pr_merged event: %v", err)
		}
		s.checkAllPRsMerged(pr.ProblemID)
		log.Printf("belayer: PR #%d detected as merged", pr.PRNumber)

	case "status_closed":
		if err := s.store.UpdatePullRequestState(pr.ID, "closed"); err != nil {
			log.Printf("belayer: error updating PR state to closed: %v", err)
		}
		if err := s.store.UpdateProblemStatus(pr.ProblemID, model.ProblemStatusClosed); err != nil {
			log.Printf("belayer: error updating problem status to closed: %v", err)
		}
		if err := s.store.InsertEvent(pr.ProblemID, "", model.EventPRClosed, fmt.Sprintf(`{"pr_number":%d}`, pr.PRNumber)); err != nil {
			log.Printf("belayer: error inserting pr_closed event: %v", err)
		}
		log.Printf("belayer: PR #%d closed — problem %s closed", pr.PRNumber, pr.ProblemID)
	}

	if err := s.store.InsertPRReaction(reaction); err != nil {
		log.Printf("belayer: error inserting PR reaction: %v", err)
	}
}

// checkAllPRsMerged checks if all PRs for a problem are merged, and if so
// transitions the problem to merged status.
func (s *Belayer) checkAllPRsMerged(problemID string) {
	prs, err := s.store.ListPullRequestsForProblem(problemID)
	if err != nil {
		log.Printf("belayer: error listing PRs for problem %s: %v", problemID, err)
		return
	}
	for _, pr := range prs {
		if pr.State != "merged" {
			return
		}
	}
	if err := s.store.UpdateProblemStatus(problemID, model.ProblemStatusMerged); err != nil {
		log.Printf("belayer: error updating problem %s to merged: %v", problemID, err)
		return
	}
	if err := s.store.InsertEvent(problemID, "", model.EventPRMerged, `{"all_merged":true}`); err != nil {
		log.Printf("belayer: error inserting all_merged event: %v", err)
	}
	log.Printf("belayer: all PRs merged for problem %s — problem marked merged", problemID)
}

// extractJSONStringField extracts a string value from a simple JSON payload
// by searching for `"key":"value"` without a full JSON parse.
// Returns empty string if not found.
func extractJSONStringField(payload, key string) string {
	needle := `"` + key + `":"`
	idx := strings.Index(payload, needle)
	if idx < 0 {
		return ""
	}
	start := idx + len(needle)
	end := strings.Index(payload[start:], `"`)
	if end < 0 {
		return ""
	}
	return payload[start : start+end]
}
