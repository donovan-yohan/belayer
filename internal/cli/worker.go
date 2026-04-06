package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/operatorservice/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/donovan-yohan/belayer/internal/intake"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/pipeline"
	"github.com/donovan-yohan/belayer/internal/session"
	beltemporal "github.com/donovan-yohan/belayer/internal/temporal"
)

const workerHTTPPort = 8780

func newWorkerCmd() *cobra.Command {
	var workDir string

	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Start the Temporal worker for pipeline execution",
		Long: `Start a long-lived Temporal worker that executes pipeline runs.

The worker registers the ClimbWorkflow and all activity implementations
and serves an HTTP API for the submit tool.

Start this BEFORE running 'belayer start' or 'belayer climb'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorker(workDir)
		},
	}

	cmd.Flags().StringVar(&workDir, "work-dir", "", "Working directory for sessions (default: current directory)")

	return cmd
}

// startWorkflowInput holds the parameters for starting a pipeline workflow.
type startWorkflowInput struct {
	Spec             string
	Source           string
	ExternalID       string
	DesignFile       string
	PipelineName     string
	PipelineYAML     []byte
	SubpipelineYAMLs map[string][]byte
	WorkDir          string
	Repos            []string
}

// startWorkflowOutput holds the result of starting a workflow.
type startWorkflowOutput struct {
	WorkflowID string
	RunID      string
}

// startWorkflow creates a worktree and starts a Climb workflow. Shared by the HTTP
// handler and intake poller to avoid duplicating the workflow-starting logic.
func startWorkflow(ctx context.Context, tc client.Client, input startWorkflowInput) (*startWorkflowOutput, error) {
	if input.Spec == "" {
		return nil, fmt.Errorf("spec is required")
	}

	ts := time.Now().UnixMilli()
	branchSlug := intake.GenerateBranchSlug(input.Spec)
	branch := fmt.Sprintf("belayer/%s-%d", branchSlug, ts)
	worktreeDir := filepath.Join(input.WorkDir, ".belayer", "worktrees", fmt.Sprintf("%d", ts))
	if err := intake.CreateGitWorktree(input.WorkDir, worktreeDir, branch); err != nil {
		return nil, fmt.Errorf("worktree error: %w", err)
	}

	externalID := input.ExternalID
	if externalID == "" {
		externalID = fmt.Sprintf("%d", ts)
	}
	workflowID := intake.GenerateWorkflowID(input.PipelineName, input.Source, externalID)
	climbInput := model.ClimbInput{
		Description:      input.Spec,
		DesignFile:       input.DesignFile,
		PipelineYAML:     input.PipelineYAML,
		SubpipelineYAMLs: input.SubpipelineYAMLs,
		WorkDir:          worktreeDir,
		Branch:           branch,
		Repos:            input.Repos,
	}

	opts := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: beltemporal.TaskQueueName,
	}
	run, err := tc.ExecuteWorkflow(ctx, opts, beltemporal.ClimbWorkflow, climbInput)
	if err != nil {
		// Best-effort cleanup. Use background context since the request context may
		// have been the reason for the failure (canceled/timed out).
		cleanupWorktree(context.Background(), input.WorkDir, worktreeDir, branch)
		return nil, fmt.Errorf("start workflow: %w", err)
	}

	return &startWorkflowOutput{WorkflowID: run.GetID(), RunID: run.GetRunID()}, nil
}

// cleanupWorktree removes a git worktree and its branch on best-effort basis.
func cleanupWorktree(ctx context.Context, repoDir, worktreeDir, branch string) {
	_ = os.RemoveAll(worktreeDir)
	for _, args := range [][]string{
		{"-C", repoDir, "worktree", "remove", "--force", worktreeDir},
		{"-C", repoDir, "branch", "-D", branch},
	} {
		cmd := exec.CommandContext(ctx, "git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("git cleanup %v failed: %v (%s)", args, err, strings.TrimSpace(string(out)))
		}
	}
}

// temporalProcess bundles the child process and its log file for coordinated cleanup.
type temporalProcess struct {
	cmd     *exec.Cmd
	logFile *os.File
}

// shutdown gracefully stops the Temporal dev server: SIGINT, wait with timeout, kill if needed.
func (tp *temporalProcess) shutdown() {
	if tp == nil || tp.cmd == nil || tp.cmd.Process == nil {
		return
	}
	_ = tp.cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- tp.cmd.Wait() }()
	select {
	case <-time.After(5 * time.Second):
		_ = tp.cmd.Process.Kill()
		<-done // reap after kill
	case <-done:
	}
	if tp.logFile != nil {
		tp.logFile.Close()
	}
}

// ensureTemporal checks if Temporal is running and starts it if not.
// Returns a temporalProcess for cleanup, or nil if Temporal was already running.
func ensureTemporal(workDir string) (*temporalProcess, error) {
	// Probe localhost (not 127.0.0.1) to match the SDK's default dial target,
	// which may resolve to IPv6 ::1 on some systems.
	conn, err := net.DialTimeout("tcp", "localhost:7233", 2*time.Second)
	if err == nil {
		conn.Close()
		return nil, nil // already running
	}

	temporalPath, err := exec.LookPath("temporal")
	if err != nil {
		return nil, fmt.Errorf("Temporal CLI not found.\n  Install: brew install temporal\n  Or: https://docs.temporal.io/cli")
	}

	logDir := filepath.Join(workDir, ".belayer", ".internal")
	os.MkdirAll(logDir, 0o755)
	logFile, err := os.Create(filepath.Join(logDir, "temporal.log"))
	if err != nil {
		return nil, fmt.Errorf("create temporal log: %w", err)
	}

	cmd := exec.Command(temporalPath, "server", "start-dev")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("start Temporal: %w", err)
	}

	for i := 0; i < 30; i++ {
		time.Sleep(500 * time.Millisecond)
		if conn, err := net.DialTimeout("tcp", "localhost:7233", 500*time.Millisecond); err == nil {
			conn.Close()
			return &temporalProcess{cmd: cmd, logFile: logFile}, nil
		}
	}
	// Timeout: kill, reap, close log.
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	logFile.Close()
	return nil, fmt.Errorf("Temporal failed to start within 15s. Check %s/temporal.log", logDir)
}

func runWorker(workDir string) error {
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
	}

	// Auto-start Temporal if not running.
	tp, err := ensureTemporal(workDir)
	if err != nil {
		return err
	}
	if tp != nil {
		defer tp.shutdown()
		fmt.Println("Started Temporal dev server (auto)")
	}

	c, err := client.Dial(client.Options{})
	if err != nil {
		return fmt.Errorf("cannot connect to Temporal: %w", err)
	}
	defer c.Close()

	// Register search attributes for Temporal Web UI observability (best-effort).
	registerSearchAttributes(c)

	// Wire real providers into the activities.
	spawner := &session.ExecSpawner{}
	activities := &beltemporal.Activities{Spawner: spawner}

	w := worker.New(c, beltemporal.TaskQueueName, worker.Options{})
	w.RegisterWorkflow(beltemporal.ClimbWorkflow)
	w.RegisterActivity(activities)

	// Resolve pipeline for intake poller.
	pipelineYAML, pipelineName, pipelineErr := intake.ResolvePipelineYAML(workDir)
	if pipelineErr != nil {
		log.Printf("Pipeline not found in %s: %v (intake poller disabled, HTTP handler will re-resolve per request)", workDir, pipelineErr)
	}
	var intakeChecks []string
	var maxConcurrent int
	var subpipelineYAMLs map[string][]byte
	if len(pipelineYAML) > 0 {
		if cfg, err := pipeline.ParsePipeline(pipelineYAML); err == nil {
			for _, ic := range cfg.Intake {
				if ic.Type == "trigger" && ic.Check != "" {
					intakeChecks = append(intakeChecks, ic.Check)
				}
			}
			maxConcurrent = cfg.Safety.MaxConcurrentRuns
			if maxConcurrent == 0 {
				maxConcurrent = 3
			}
			// Pre-resolve subpipeline YAMLs for router nodes (reproducibility).
			if resolved, err := pipeline.ResolveSubpipelineYAMLs(cfg, workDir); err != nil {
				log.Fatalf("failed to resolve router subpipelines: %v", err)
			} else {
				subpipelineYAMLs = resolved
			}
		}
	}

	// Start intake poller if triggers exist.
	if len(intakeChecks) > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go startIntakePoller(ctx, c, workDir, intakeChecks, maxConcurrent, pipelineYAML, pipelineName, subpipelineYAMLs)
	}

	// Start HTTP API server for submit/status tools.
	httpErrCh := make(chan error, 1)
	go func() {
		httpErrCh <- startHTTPAPI(c, workDir, pipelineYAML, pipelineName, subpipelineYAMLs)
	}()

	// Fail fast if the HTTP API can't bind (e.g., port already in use).
	select {
	case err := <-httpErrCh:
		return fmt.Errorf("HTTP API failed to start: %w", err)
	case <-time.After(100 * time.Millisecond):
		// HTTP server started successfully.
	}

	fmt.Printf("Belayer worker started\n")
	fmt.Printf("  Task queue: %s\n", beltemporal.TaskQueueName)
	fmt.Printf("  Work dir:   %s\n", workDir)
	fmt.Printf("  HTTP API:   http://127.0.0.1:%d\n", workerHTTPPort)
	if len(intakeChecks) > 0 {
		fmt.Printf("  Intake:     %d trigger(s) polling every 30s\n", len(intakeChecks))
	}
	fmt.Printf("\nWaiting for pipeline runs... (Ctrl+C to stop)\n")

	return w.Run(worker.InterruptCh())
}

// startHTTPAPI runs the HTTP API server for submit/status tool calls.
func startHTTPAPI(temporalClient client.Client, workDir string, pipelineYAML []byte, pipelineName string, subpipelineYAMLs map[string][]byte) error {
	mux := http.NewServeMux()

	// POST /start — accepts SubmitSpec JSON, starts a ClimbWorkflow, returns workflow ID.
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var spec intake.SubmitSpec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if spec.Source == "" {
			spec.Source = "interactive"
		}
		if err := spec.Validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resolvedYAML := pipelineYAML
		resolvedName := pipelineName
		resolvedSubYAMLs := subpipelineYAMLs
		if len(resolvedYAML) == 0 {
			var err error
			resolvedYAML, resolvedName, err = intake.ResolvePipelineYAML(workDir)
			if err != nil {
				http.Error(w, "Pipeline error: "+err.Error(), http.StatusBadRequest)
				return
			}
			// Re-resolve subpipeline YAMLs from the freshly parsed pipeline.
			if cfg, err := pipeline.ParsePipeline(resolvedYAML); err == nil {
				if subs, err := pipeline.ResolveSubpipelineYAMLs(cfg, workDir); err != nil {
					http.Error(w, "Subpipeline error: "+err.Error(), http.StatusBadRequest)
					return
				} else {
					resolvedSubYAMLs = subs
				}
			}
		}
		if spec.PipelineName == "" {
			spec.PipelineName = resolvedName
		}

		out, err := startWorkflow(r.Context(), temporalClient, startWorkflowInput{
			Spec:             spec.Spec,
			Source:           spec.Source,
			ExternalID:       spec.ExternalID,
			DesignFile:       spec.Metadata["design_file"],
			PipelineName:     spec.PipelineName,
			PipelineYAML:     resolvedYAML,
			SubpipelineYAMLs: resolvedSubYAMLs,
			WorkDir:          workDir,
			Repos:            spec.Repos,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"workflow_id": out.WorkflowID,
			"run_id":      out.RunID,
		})
	})

	// GET /status — returns worker status.
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"note":   "Use 'belayer status' CLI for detailed workflow listing",
		})
	})

	server := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", workerHTTPPort),
		Handler: mux,
	}
	return server.ListenAndServe()
}

// startIntakePoller polls intake trigger scripts and starts workflows when
// APPROVED design docs are found. Consume-after-confirm: the doc is only
// marked consumed after the workflow starts successfully.
func startIntakePoller(ctx context.Context, tc client.Client, workDir string, checks []string, maxConcurrent int, pipelineYAML []byte, pipelineName string, subpipelineYAMLs map[string][]byte) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	failures := make(map[int]int)  // consecutive failure count per check
	const maxConsecutiveFailures = 5

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check active workflow count using Temporal visibility.
			count, err := tc.CountWorkflow(ctx, &workflowservice.CountWorkflowExecutionsRequest{
				Query: fmt.Sprintf("TaskQueue='%s' AND ExecutionStatus='Running'", beltemporal.TaskQueueName),
			})
			if err != nil {
				log.Printf("intake poller: count workflows failed: %v", err)
				continue
			}
			if count.Count >= int64(maxConcurrent) {
				continue
			}

			for i, check := range checks {
				if failures[i] >= maxConsecutiveFailures {
					continue // disabled after repeated failures
				}
				artifactPath, err := runIntakeCheck(ctx, workDir, check)
				if err != nil {
					failures[i]++
					if failures[i] >= maxConsecutiveFailures {
						log.Printf("intake poller: check %q disabled after %d consecutive failures: %v", check, failures[i], err)
					} else {
						log.Printf("intake poller: check %q failed (%d/%d): %v", check, failures[i], maxConsecutiveFailures, err)
					}
					continue
				}
				failures[i] = 0 // reset on success
				if artifactPath == "" {
					continue // No approved doc found.
				}

				// Read the design doc content.
				specData, err := os.ReadFile(artifactPath)
				if err != nil {
					log.Printf("intake poller: read artifact %q: %v", artifactPath, err)
					continue
				}

				// Start workflow (consume-after-confirm).
				// Use artifact basename as ExternalID for idempotent workflow IDs.
				// If the same doc triggers twice (crash between start and consume),
				// Temporal rejects the duplicate workflow ID.
				out, err := startWorkflow(ctx, tc, startWorkflowInput{
					Spec:             string(specData),
					Source:           "trigger",
					ExternalID:       filepath.Base(artifactPath),
					DesignFile:       artifactPath,
					PipelineName:     pipelineName,
					PipelineYAML:     pipelineYAML,
					SubpipelineYAMLs: subpipelineYAMLs,
					WorkDir:          workDir,
				})
				if err != nil {
					log.Printf("intake poller: start workflow failed: %v", err)
					continue
				}

				// Mark consumed AFTER workflow confirmed.
				markConsumed(artifactPath)
				log.Printf("intake poller: started workflow %s for %s", out.WorkflowID, filepath.Base(artifactPath))
			}
		}
	}
}

// runIntakeCheck executes a trigger check script and returns the artifact path
// if the check found something. Returns ("", nil) if nothing was found.
func runIntakeCheck(ctx context.Context, workDir, check string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", check)
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		// Exit code 1 = nothing found (normal). Other errors = script problem.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "", nil
		}
		return "", fmt.Errorf("check script failed: %w", err)
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", nil
	}
	return path, nil
}

// markConsumed appends the artifact basename to the .consumed file so it won't
// be re-triggered. This is called AFTER workflow start is confirmed.
func markConsumed(artifactPath string) {
	consumedFile := filepath.Join(filepath.Dir(artifactPath), ".consumed")
	basename := filepath.Base(artifactPath)
	f, err := os.OpenFile(consumedFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("intake poller: mark consumed failed: %v", err)
		return
	}
	defer f.Close()
	fmt.Fprintln(f, basename)
}

// registerSearchAttributes registers custom search attributes with the Temporal
// dev server for Web UI observability. Best-effort: silently skips on error
// (attribute may already exist, or server doesn't support the operator API).
func registerSearchAttributes(c client.Client) {
	// Skip for non-localhost Temporal (Cloud requires UI-based registration).
	addr := os.Getenv("TEMPORAL_ADDRESS")
	if addr != "" && !strings.Contains(addr, "localhost") && !strings.Contains(addr, "127.0.0.1") {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = c.OperatorService().AddSearchAttributes(ctx, &operatorservice.AddSearchAttributesRequest{
		Namespace: "default",
		SearchAttributes: map[string]enumspb.IndexedValueType{
			"CurrentNode": enumspb.INDEXED_VALUE_TYPE_TEXT,
		},
	})
}
