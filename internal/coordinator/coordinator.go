package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/donovan-yohan/belayer/internal/lead"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/repo"
)

// LeadRunner abstracts lead execution for testing.
type LeadRunner interface {
	Run(ctx context.Context, cfg lead.RunConfig) (*lead.RunResult, error)
}

// WorktreeCreator abstracts worktree creation for testing.
type WorktreeCreator interface {
	CreateWorktree(instanceDir, taskID, repoName string) (string, error)
}

// DiffCollector abstracts git diff collection for testing.
type DiffCollector interface {
	CollectDiff(worktreePath string) (string, error)
	CollectDiffStat(worktreePath string) (string, error)
}

// PRCreator abstracts PR creation for testing.
type PRCreator interface {
	PushAndCreatePR(worktreePath, title, body string) (prURL string, err error)
}

// CoordinatorConfig holds configuration for the coordinator engine.
type CoordinatorConfig struct {
	PollInterval         time.Duration
	MaxLeadRetries       int
	BaseRetryDelay       time.Duration
	MaxRetryDelay        time.Duration
	AgenticModel         string
	RepoNames            []string // Available repo names from the instance
	MaxAlignmentAttempts int
}

// DefaultConfig returns a CoordinatorConfig with sensible defaults.
func DefaultConfig() CoordinatorConfig {
	return CoordinatorConfig{
		PollInterval:         2 * time.Second,
		MaxLeadRetries:       3,
		BaseRetryDelay:       5 * time.Second,
		MaxRetryDelay:        5 * time.Minute,
		AgenticModel:         "claude-sonnet-4-6",
		MaxAlignmentAttempts: 2,
	}
}

// Coordinator is the central orchestration engine.
// It drives tasks through their lifecycle by polling SQLite and invoking
// agentic nodes for judgment calls.
type Coordinator struct {
	store          *Store
	leadRunner     LeadRunner
	worktrees      WorktreeCreator
	diffCollector  DiffCollector
	prCreator      PRCreator
	instanceDir    string
	instanceID     string
	config         CoordinatorConfig
	retries        *RetryScheduler
	activeLeads    map[string]context.CancelFunc // leadID -> cancel
	mu             sync.Mutex
	cancel         context.CancelFunc
	done           chan struct{}
}

// NewCoordinator creates a new coordinator engine.
func NewCoordinator(
	store *Store,
	leadRunner LeadRunner,
	worktrees WorktreeCreator,
	instanceDir string,
	instanceID string,
	cfg CoordinatorConfig,
	opts ...CoordinatorOption,
) *Coordinator {
	c := &Coordinator{
		store:       store,
		leadRunner:  leadRunner,
		worktrees:   worktrees,
		instanceDir: instanceDir,
		instanceID:  instanceID,
		config:      cfg,
		retries:     NewRetryScheduler(cfg.BaseRetryDelay, cfg.MaxRetryDelay, cfg.MaxLeadRetries),
		activeLeads: make(map[string]context.CancelFunc),
		done:        make(chan struct{}),
	}
	for _, opt := range opts {
		opt(c)
	}
	// Set defaults for optional interfaces
	if c.diffCollector == nil {
		c.diffCollector = &defaultDiffCollector{}
	}
	if c.prCreator == nil {
		c.prCreator = &defaultPRCreator{}
	}
	return c
}

// CoordinatorOption configures optional coordinator dependencies.
type CoordinatorOption func(*Coordinator)

// WithDiffCollector sets a custom diff collector.
func WithDiffCollector(dc DiffCollector) CoordinatorOption {
	return func(c *Coordinator) { c.diffCollector = dc }
}

// WithPRCreator sets a custom PR creator.
func WithPRCreator(pr PRCreator) CoordinatorOption {
	return func(c *Coordinator) { c.prCreator = pr }
}

// Start launches the coordinator polling loop. It blocks until ctx is cancelled.
func (c *Coordinator) Start(ctx context.Context) error {
	ctx, c.cancel = context.WithCancel(ctx)
	ticker := time.NewTicker(c.config.PollInterval)
	defer ticker.Stop()
	defer close(c.done)

	// Run one tick immediately
	c.processTick(ctx)

	for {
		select {
		case <-ctx.Done():
			c.shutdown()
			return ctx.Err()
		case <-ticker.C:
			c.processTick(ctx)
		}
	}
}

// Stop cancels the coordinator and waits for shutdown.
func (c *Coordinator) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	<-c.done
}

// processTick runs one cycle of the coordinator state machine.
func (c *Coordinator) processTick(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	// Process tasks by status
	c.processPendingTasks(ctx)
	c.processRunningTasks(ctx)
	c.processAligningTasks(ctx)
	c.processRetries(ctx)
}

// processPendingTasks picks up pending tasks and starts decomposition.
func (c *Coordinator) processPendingTasks(ctx context.Context) {
	tasks, err := c.store.GetTasksByStatus(model.TaskStatusPending)
	if err != nil {
		log.Printf("coordinator: error getting pending tasks: %v", err)
		return
	}

	for _, task := range tasks {
		if ctx.Err() != nil {
			return
		}
		c.startDecomposition(ctx, task)
	}
}

// processRunningTasks checks lead progress for running tasks.
func (c *Coordinator) processRunningTasks(ctx context.Context) {
	tasks, err := c.store.GetTasksByStatus(model.TaskStatusRunning)
	if err != nil {
		log.Printf("coordinator: error getting running tasks: %v", err)
		return
	}

	for _, task := range tasks {
		if ctx.Err() != nil {
			return
		}
		c.checkLeadProgress(ctx, task)
	}
}

// processAligningTasks checks alignment results.
func (c *Coordinator) processAligningTasks(ctx context.Context) {
	tasks, err := c.store.GetTasksByStatus(model.TaskStatusAligning)
	if err != nil {
		log.Printf("coordinator: error getting aligning tasks: %v", err)
		return
	}

	for _, task := range tasks {
		if ctx.Err() != nil {
			return
		}
		// Alignment is already triggered when transitioning to aligning.
		// If still in aligning state, the agentic node is running or already
		// finished. Check for completion is handled by the alignment goroutine.
		_ = task
	}
}

// processRetries handles leads that are scheduled for retry.
func (c *Coordinator) processRetries(ctx context.Context) {
	readyLeads := c.retries.ReadyLeads()
	for _, leadID := range readyLeads {
		if ctx.Err() != nil {
			return
		}
		c.retryLead(ctx, leadID)
	}
}

// DecompositionOutput is the expected JSON structure from the decomposition agentic node.
type DecompositionOutput struct {
	Repos []DecompositionRepo `json:"repos"`
}

// DecompositionRepo is one repo in the decomposition output.
type DecompositionRepo struct {
	Name string `json:"name"`
	Spec string `json:"spec"`
}

// SufficiencyOutput is the expected JSON structure from the sufficiency agentic node.
type SufficiencyOutput struct {
	Sufficient bool     `json:"sufficient"`
	Gaps       []string `json:"gaps"`
}

// AlignmentOutput is the expected JSON structure from the alignment agentic node.
type AlignmentOutput struct {
	Pass            bool                 `json:"pass"`
	Feedback        string               `json:"feedback"`
	Criteria        []AlignmentCriterion `json:"criteria,omitempty"`
	MisalignedRepos []string             `json:"misaligned_repos,omitempty"`
}

// AlignmentCriterion is an individual pass/fail check in the alignment review.
type AlignmentCriterion struct {
	Name    string `json:"name"`
	Pass    bool   `json:"pass"`
	Details string `json:"details"`
}

// StuckOutput is the expected JSON structure from the stuck analysis agentic node.
type StuckOutput struct {
	Diagnosis   string `json:"diagnosis"`
	Recovery    string `json:"recovery"`
	ShouldRetry bool   `json:"should_retry"`
}

// startDecomposition runs sufficiency check (if not pre-checked) + decomposition for a pending task.
func (c *Coordinator) startDecomposition(ctx context.Context, task model.Task) {
	// Transition to decomposing
	if err := c.store.UpdateTaskStatus(task.ID, model.TaskStatusDecomposing); err != nil {
		log.Printf("coordinator: error updating task %s to decomposing: %v", task.ID, err)
		return
	}

	// Skip sufficiency check if already done at intake (CLI level)
	if !task.SufficiencyChecked {
		suffNode := NewAgenticNode(c.store, model.AgenticSufficiency, c.config.AgenticModel)
		suffPrompt := fmt.Sprintf(
			"You are a task sufficiency checker. Evaluate whether this task has enough context to be decomposed into per-repo implementation specs.\n\nTask description: %s\n\nRespond with JSON: {\"sufficient\": true/false, \"gaps\": [\"list of missing context\"]}",
			task.Description,
		)

		suffResult, err := suffNode.Execute(ctx, task.ID, suffPrompt)
		if err != nil {
			log.Printf("coordinator: sufficiency check failed for task %s: %v", task.ID, err)
			_ = c.store.UpdateTaskStatus(task.ID, model.TaskStatusFailed)
			return
		}

		var suffOutput SufficiencyOutput
		if err := json.Unmarshal([]byte(suffResult.Raw), &suffOutput); err != nil {
			log.Printf("coordinator: error parsing sufficiency output for task %s: %v", task.ID, err)
		}

		if !suffOutput.Sufficient && len(suffOutput.Gaps) > 0 {
			log.Printf("coordinator: task %s has gaps: %v (proceeding with decomposition anyway)", task.ID, suffOutput.Gaps)
		}
	} else {
		log.Printf("coordinator: task %s already passed sufficiency check at intake, skipping", task.ID)
	}

	// Build repo list for instance-aware decomposition
	repoList := "not specified"
	if len(c.config.RepoNames) > 0 {
		repoList = strings.Join(c.config.RepoNames, ", ")
	}

	// Run decomposition with instance-aware prompt
	decompNode := NewAgenticNode(c.store, model.AgenticDecomposition, c.config.AgenticModel)
	decompPrompt := fmt.Sprintf(
		`You are a task decomposer for a multi-repo coding orchestrator. Break this task into per-repo implementation specs. You MUST only use repos from the available list. Not all repos need to be included — only those relevant to the task.

Available repos: %s

Task description: %s

Respond with JSON: {"repos": [{"name": "repo-name", "spec": "detailed implementation spec for this repo"}]}`,
		repoList, task.Description,
	)

	decompResult, err := decompNode.Execute(ctx, task.ID, decompPrompt)
	if err != nil {
		log.Printf("coordinator: decomposition failed for task %s: %v", task.ID, err)
		_ = c.store.UpdateTaskStatus(task.ID, model.TaskStatusFailed)
		return
	}

	var decompOutput DecompositionOutput
	if err := json.Unmarshal([]byte(decompResult.Raw), &decompOutput); err != nil {
		log.Printf("coordinator: error parsing decomposition output for task %s: %v", task.ID, err)
		_ = c.store.UpdateTaskStatus(task.ID, model.TaskStatusFailed)
		return
	}

	if len(decompOutput.Repos) == 0 {
		log.Printf("coordinator: decomposition produced no repos for task %s", task.ID)
		_ = c.store.UpdateTaskStatus(task.ID, model.TaskStatusFailed)
		return
	}

	// Emit decomposition event
	_ = c.insertEvent(task.ID, "", model.EventTaskDecomposed,
		fmt.Sprintf(`{"repo_count":%d}`, len(decompOutput.Repos)))

	// Create task repos, worktrees, and leads
	c.spawnLeads(ctx, task, decompOutput.Repos)
}

// spawnLeads creates task repos, worktrees, and lead processes for a decomposed task.
func (c *Coordinator) spawnLeads(ctx context.Context, task model.Task, repos []DecompositionRepo) {
	for _, repoSpec := range repos {
		taskRepoID := fmt.Sprintf("tr-%s-%s-%d", task.ID, repoSpec.Name, time.Now().UnixNano())

		// Create worktree
		worktreePath, err := c.worktrees.CreateWorktree(c.instanceDir, task.ID, repoSpec.Name)
		if err != nil {
			log.Printf("coordinator: error creating worktree for %s/%s: %v", task.ID, repoSpec.Name, err)
			_ = c.store.UpdateTaskStatus(task.ID, model.TaskStatusFailed)
			return
		}

		// Insert task repo
		tr := &model.TaskRepo{
			ID:           taskRepoID,
			TaskID:       task.ID,
			RepoName:     repoSpec.Name,
			Spec:         repoSpec.Spec,
			WorktreePath: worktreePath,
		}
		if err := c.store.InsertTaskRepo(tr); err != nil {
			log.Printf("coordinator: error inserting task repo %s: %v", taskRepoID, err)
			_ = c.store.UpdateTaskStatus(task.ID, model.TaskStatusFailed)
			return
		}

		// Insert lead
		leadID := fmt.Sprintf("lead-%s-%d", taskRepoID, time.Now().UnixNano())
		l := &model.Lead{
			ID:         leadID,
			TaskRepoID: taskRepoID,
			Status:     model.LeadStatusPending,
			Attempt:    0,
		}
		if err := c.store.InsertLead(l); err != nil {
			log.Printf("coordinator: error inserting lead %s: %v", leadID, err)
			_ = c.store.UpdateTaskStatus(task.ID, model.TaskStatusFailed)
			return
		}

		// Spawn lead goroutine
		c.spawnLeadGoroutine(ctx, task.ID, leadID, worktreePath, repoSpec.Spec)
	}

	// Transition task to running
	if err := c.store.UpdateTaskStatus(task.ID, model.TaskStatusRunning); err != nil {
		log.Printf("coordinator: error updating task %s to running: %v", task.ID, err)
	}
}

// spawnLeadGoroutine launches a lead in a goroutine with tracking.
func (c *Coordinator) spawnLeadGoroutine(ctx context.Context, taskID, leadID, worktreePath, spec string) {
	leadCtx, cancel := context.WithCancel(ctx)

	c.mu.Lock()
	c.activeLeads[leadID] = cancel
	c.mu.Unlock()

	go func() {
		defer func() {
			c.mu.Lock()
			delete(c.activeLeads, leadID)
			c.mu.Unlock()
		}()

		cfg := lead.DefaultRunConfig(
			worktreePath,
			leadID,
			taskID,
			spec,
			[]model.GoalSpec{{Index: 0, Description: spec}},
		)

		_, err := c.leadRunner.Run(leadCtx, cfg)
		if err != nil {
			log.Printf("coordinator: lead %s finished with error: %v", leadID, err)
		}
	}()
}

// checkLeadProgress checks all leads for a running task and drives transitions.
func (c *Coordinator) checkLeadProgress(ctx context.Context, task model.Task) {
	leads, err := c.store.GetLeadsForTask(task.ID)
	if err != nil {
		log.Printf("coordinator: error getting leads for task %s: %v", task.ID, err)
		return
	}

	if len(leads) == 0 {
		return
	}

	allComplete := true
	anyFailed := false
	anyStuck := false

	for _, l := range leads {
		switch l.Status {
		case model.LeadStatusComplete:
			// OK
		case model.LeadStatusFailed:
			anyFailed = true
			allComplete = false
			// Schedule retry if eligible
			if !c.retries.Has(l.ID) {
				if !c.retries.Schedule(l.ID, l.Attempt) {
					log.Printf("coordinator: lead %s exceeded max retries", l.ID)
				}
			}
		case model.LeadStatusStuck:
			anyStuck = true
			allComplete = false
			c.handleStuckLead(ctx, task, l)
		default:
			// Running or pending
			allComplete = false
		}
	}

	if allComplete {
		c.startAlignment(ctx, task)
		return
	}

	// If all non-complete leads have permanently failed (retries exhausted), fail the task
	if anyFailed && !anyStuck {
		allExhausted := true
		for _, l := range leads {
			if l.Status == model.LeadStatusFailed && c.retries.Has(l.ID) {
				allExhausted = false
				break
			}
			if l.Status == model.LeadStatusRunning || l.Status == model.LeadStatusPending {
				allExhausted = false
				break
			}
		}
		if allExhausted {
			log.Printf("coordinator: all leads for task %s have failed, marking task as failed", task.ID)
			_ = c.store.UpdateTaskStatus(task.ID, model.TaskStatusFailed)
		}
	}
}

// handleStuckLead runs stuck analysis and decides whether to retry.
func (c *Coordinator) handleStuckLead(ctx context.Context, task model.Task, l model.Lead) {
	// Don't re-analyze if already scheduled for retry
	if c.retries.Has(l.ID) {
		return
	}

	stuckNode := NewAgenticNode(c.store, model.AgenticStuckAnalysis, c.config.AgenticModel)
	stuckPrompt := fmt.Sprintf(
		"A lead execution loop is stuck. Analyze the situation and suggest recovery.\n\nLead ID: %s\nStatus: %s\nAttempt: %d\nOutput: %s\n\nRespond with JSON: {\"diagnosis\": \"...\", \"recovery\": \"...\", \"should_retry\": true/false}",
		l.ID, l.Status, l.Attempt, l.Output,
	)

	result, err := stuckNode.Execute(ctx, task.ID, stuckPrompt)
	if err != nil {
		log.Printf("coordinator: stuck analysis failed for lead %s: %v", l.ID, err)
		// Schedule retry anyway
		c.retries.Schedule(l.ID, l.Attempt)
		return
	}

	var stuckOutput StuckOutput
	if err := json.Unmarshal([]byte(result.Raw), &stuckOutput); err != nil {
		log.Printf("coordinator: error parsing stuck analysis for lead %s: %v", l.ID, err)
		c.retries.Schedule(l.ID, l.Attempt)
		return
	}

	if stuckOutput.ShouldRetry {
		c.retries.Schedule(l.ID, l.Attempt)
	} else {
		log.Printf("coordinator: stuck analysis recommends no retry for lead %s: %s", l.ID, stuckOutput.Diagnosis)
	}
}

// retryLead creates a new lead for a failed/stuck lead's task repo.
func (c *Coordinator) retryLead(ctx context.Context, leadID string) {
	c.retries.Remove(leadID)

	// Look up the original lead to find its task repo
	leads, err := c.store.GetLeadsForTaskByLeadID(leadID)
	if err != nil || leads == nil {
		log.Printf("coordinator: error looking up lead %s for retry: %v", leadID, err)
		return
	}

	origLead := leads
	taskRepos, err := c.store.GetTaskRepoByID(origLead.TaskRepoID)
	if err != nil {
		log.Printf("coordinator: error looking up task repo for lead %s: %v", leadID, err)
		return
	}

	// Create new lead with incremented attempt
	newLeadID := fmt.Sprintf("lead-%s-%d", origLead.TaskRepoID, time.Now().UnixNano())
	newLead := &model.Lead{
		ID:         newLeadID,
		TaskRepoID: origLead.TaskRepoID,
		Status:     model.LeadStatusPending,
		Attempt:    origLead.Attempt + 1,
	}
	if err := c.store.InsertLead(newLead); err != nil {
		log.Printf("coordinator: error inserting retry lead %s: %v", newLeadID, err)
		return
	}

	c.spawnLeadGoroutine(ctx, taskRepos.TaskID, newLeadID, taskRepos.WorktreePath, taskRepos.Spec)
}

// maxDiffSize is the maximum combined diff size in bytes before falling back to stat-only.
const maxDiffSize = 50 * 1024

// startAlignment triggers the alignment agentic node when all leads are complete.
func (c *Coordinator) startAlignment(ctx context.Context, task model.Task) {
	if err := c.store.UpdateTaskStatus(task.ID, model.TaskStatusAligning); err != nil {
		log.Printf("coordinator: error updating task %s to aligning: %v", task.ID, err)
		return
	}

	_ = c.insertEvent(task.ID, "", model.EventAlignmentStarted, "{}")

	// Run alignment in a goroutine to not block the tick
	go func() {
		// Check alignment attempt count
		attempts, err := c.store.CountAlignmentAttempts(task.ID)
		if err != nil {
			log.Printf("coordinator: error counting alignment attempts for task %s: %v", task.ID, err)
		}

		maxAttempts := c.config.MaxAlignmentAttempts
		if maxAttempts <= 0 {
			maxAttempts = 2
		}

		if attempts > maxAttempts {
			log.Printf("coordinator: task %s exceeded max alignment attempts (%d), marking as failed", task.ID, maxAttempts)
			_ = c.store.UpdateTaskStatus(task.ID, model.TaskStatusFailed)
			_ = c.insertEvent(task.ID, "", model.EventAlignmentFailed,
				fmt.Sprintf(`{"error":"exceeded max alignment attempts (%d)"}`, maxAttempts))
			return
		}

		alignNode := NewAgenticNode(c.store, model.AgenticAlignment, c.config.AgenticModel)

		// Build context about what was done in each repo
		taskRepos, err := c.store.GetTaskReposForTask(task.ID)
		if err != nil {
			log.Printf("coordinator: error getting task repos for alignment: %v", err)
			_ = c.store.UpdateTaskStatus(task.ID, model.TaskStatusFailed)
			return
		}

		// Collect diffs and stats from each repo
		repoSummary := ""
		totalDiffSize := 0
		repoDiffs := make(map[string]string)
		repoStats := make(map[string]string)

		for _, tr := range taskRepos {
			diff, err := c.diffCollector.CollectDiff(tr.WorktreePath)
			if err != nil {
				log.Printf("coordinator: error collecting diff for %s: %v", tr.RepoName, err)
				diff = "(diff unavailable)"
			}
			repoDiffs[tr.RepoName] = diff
			totalDiffSize += len(diff)

			stat, err := c.diffCollector.CollectDiffStat(tr.WorktreePath)
			if err != nil {
				log.Printf("coordinator: error collecting diff stat for %s: %v", tr.RepoName, err)
				stat = "(stat unavailable)"
			}
			repoStats[tr.RepoName] = stat
		}

		// Build the prompt with diffs (or stats if too large)
		useDiffs := totalDiffSize <= maxDiffSize
		for _, tr := range taskRepos {
			repoSummary += fmt.Sprintf("## Repo: %s\nSpec: %s\n\n", tr.RepoName, tr.Spec)
			if useDiffs {
				repoSummary += fmt.Sprintf("### Diff:\n```\n%s\n```\n\n", repoDiffs[tr.RepoName])
			}
			repoSummary += fmt.Sprintf("### Diff Stats:\n```\n%s\n```\n\n", repoStats[tr.RepoName])
		}

		diffNote := ""
		if !useDiffs {
			diffNote = "\nNote: Full diffs were too large and have been omitted. Only diff stats are included.\n"
		}

		alignPrompt := fmt.Sprintf(
			`Review cross-repo alignment for this task. Check that implementations across repos are consistent.
%s
Task: %s

%s
Evaluate these criteria:
1. API contract consistency (endpoints, request/response shapes match across repos)
2. Shared type compatibility (data models, enums, constants are consistent)
3. Feature parity (all repos implement their assigned part of the task)
4. Integration points (service URLs, event names, message formats align)

Respond with JSON:
{
  "pass": true/false,
  "feedback": "overall feedback",
  "criteria": [{"name": "criterion_name", "pass": true/false, "details": "explanation"}],
  "misaligned_repos": ["repo-name-1"]
}

Only include misaligned_repos if pass is false. List only the repos that need fixes.`,
			diffNote, task.Description, repoSummary,
		)

		result, err := alignNode.Execute(ctx, task.ID, alignPrompt)
		if err != nil {
			log.Printf("coordinator: alignment check failed for task %s: %v", task.ID, err)
			_ = c.store.UpdateTaskStatus(task.ID, model.TaskStatusFailed)
			_ = c.insertEvent(task.ID, "", model.EventAlignmentFailed, fmt.Sprintf(`{"error":%q}`, err.Error()))
			return
		}

		var alignOutput AlignmentOutput
		if err := json.Unmarshal([]byte(result.Raw), &alignOutput); err != nil {
			log.Printf("coordinator: error parsing alignment output for task %s: %v", task.ID, err)
			_ = c.store.UpdateTaskStatus(task.ID, model.TaskStatusFailed)
			return
		}

		if alignOutput.Pass {
			// Create PRs for each repo
			c.createPRs(ctx, task, taskRepos)
			_ = c.store.UpdateTaskStatus(task.ID, model.TaskStatusComplete)
			_ = c.insertEvent(task.ID, "", model.EventAlignmentPassed, `{"pass":true}`)
		} else {
			log.Printf("coordinator: alignment failed for task %s: %s", task.ID, alignOutput.Feedback)
			_ = c.insertEvent(task.ID, "", model.EventAlignmentFailed,
				fmt.Sprintf(`{"pass":false,"feedback":%q}`, alignOutput.Feedback))

			// Attempt re-dispatch if under max attempts
			if attempts < maxAttempts {
				c.redispatchMisaligned(ctx, task, taskRepos, alignOutput)
			} else {
				log.Printf("coordinator: max alignment attempts reached for task %s, marking as failed", task.ID)
				_ = c.store.UpdateTaskStatus(task.ID, model.TaskStatusFailed)
			}
		}
	}()
}

// createPRs pushes branches and creates PRs for each repo in the task.
func (c *Coordinator) createPRs(ctx context.Context, task model.Task, taskRepos []model.TaskRepo) {
	prURLs := make(map[string]string)
	for _, tr := range taskRepos {
		prURL, err := c.prCreator.PushAndCreatePR(
			tr.WorktreePath,
			fmt.Sprintf("[belayer] %s (%s)", task.Description, tr.RepoName),
			fmt.Sprintf("Automated changes for task %s\n\nSpec:\n%s", task.ID, tr.Spec),
		)
		if err != nil {
			log.Printf("coordinator: PR creation failed for %s: %v", tr.RepoName, err)
			prURLs[tr.RepoName] = fmt.Sprintf("error: %s", err.Error())
		} else {
			prURLs[tr.RepoName] = prURL
		}
	}

	// Record PR URLs as event
	prPayload, _ := json.Marshal(prURLs)
	_ = c.insertEvent(task.ID, "", model.EventPRsCreated, string(prPayload))
}

// redispatchMisaligned creates new leads for repos that failed alignment.
func (c *Coordinator) redispatchMisaligned(ctx context.Context, task model.Task, taskRepos []model.TaskRepo, alignOutput AlignmentOutput) {
	// Build set of misaligned repo names
	misaligned := make(map[string]bool)
	for _, name := range alignOutput.MisalignedRepos {
		misaligned[name] = true
	}

	// If no specific repos listed, re-dispatch all
	if len(misaligned) == 0 {
		for _, tr := range taskRepos {
			misaligned[tr.RepoName] = true
		}
	}

	for _, tr := range taskRepos {
		if !misaligned[tr.RepoName] {
			continue
		}

		// Create new lead with alignment feedback appended to spec
		enhancedSpec := fmt.Sprintf("%s\n\n--- ALIGNMENT FEEDBACK ---\nThe previous implementation failed alignment review.\nFeedback: %s\n\nPlease fix the issues identified above.",
			tr.Spec, alignOutput.Feedback)

		newLeadID := fmt.Sprintf("lead-%s-%d", tr.ID, time.Now().UnixNano())
		newLead := &model.Lead{
			ID:         newLeadID,
			TaskRepoID: tr.ID,
			Status:     model.LeadStatusPending,
			Attempt:    0,
		}
		if err := c.store.InsertLead(newLead); err != nil {
			log.Printf("coordinator: error inserting re-dispatch lead %s: %v", newLeadID, err)
			continue
		}

		c.spawnLeadGoroutine(ctx, task.ID, newLeadID, tr.WorktreePath, enhancedSpec)
	}

	// Transition task back to running
	if err := c.store.UpdateTaskStatus(task.ID, model.TaskStatusRunning); err != nil {
		log.Printf("coordinator: error updating task %s back to running: %v", task.ID, err)
	}
}

// defaultDiffCollector uses the repo package for git operations.
type defaultDiffCollector struct{}

func (d *defaultDiffCollector) CollectDiff(worktreePath string) (string, error) {
	return repo.WorktreeDiff(worktreePath)
}

func (d *defaultDiffCollector) CollectDiffStat(worktreePath string) (string, error) {
	return repo.WorktreeDiffStat(worktreePath)
}

// defaultPRCreator uses the repo package for push and PR creation.
type defaultPRCreator struct{}

func (p *defaultPRCreator) PushAndCreatePR(worktreePath, title, body string) (string, error) {
	if err := repo.PushBranch(worktreePath); err != nil {
		return "", fmt.Errorf("pushing branch: %w", err)
	}
	return repo.CreatePR(worktreePath, title, body)
}

// shutdown cancels all active leads.
func (c *Coordinator) shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for leadID, cancel := range c.activeLeads {
		log.Printf("coordinator: cancelling lead %s", leadID)
		cancel()
	}
}

// insertEvent is a helper to record events in the database.
func (c *Coordinator) insertEvent(taskID, leadID string, eventType model.EventType, payload string) error {
	id := fmt.Sprintf("evt-%s-%d", taskID, time.Now().UnixNano())
	query := `INSERT INTO events (id, task_id, lead_id, type, payload, created_at) VALUES (?, ?, ?, ?, ?, ?)`
	_, err := c.store.db.Exec(query, id, taskID, leadID, string(eventType), payload, time.Now().UTC())
	return err
}

// ActiveLeadCount returns the number of currently running leads (for monitoring).
func (c *Coordinator) ActiveLeadCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.activeLeads)
}
