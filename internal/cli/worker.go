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
// handler and workflow keeper to avoid duplicating the workflow-starting logic.
// Empty Spec is allowed when the pipeline starts with a poll node (the poll provides input).
func startWorkflow(ctx context.Context, tc client.Client, input startWorkflowInput) (*startWorkflowOutput, error) {
	ts := time.Now().UnixMilli()
	branchSlug := intake.GenerateBranchSlug(input.Spec)
	if branchSlug == "" {
		branchSlug = "poll"
	}
	branch := fmt.Sprintf("belayer/%s-%d", branchSlug, ts)
	worktreeDir := filepath.Join(input.WorkDir, ".belayer", "worktrees", fmt.Sprintf("%d", ts))
	if err := intake.CreateGitWorktree(input.WorkDir, worktreeDir, branch); err != nil {
		return nil, fmt.Errorf("worktree error: %w", err)
	}

	// Materialize the spec/design doc into the worktree so nodes can read it.
	// .belayer/.internal/ is gitignored, so it won't exist in a fresh worktree.
	if input.DesignFile != "" && input.Spec != "" {
		inputDir := filepath.Join(worktreeDir, ".belayer", ".internal", "input")
		if err := os.MkdirAll(inputDir, 0o755); err != nil {
			cleanupWorktree(context.Background(), input.WorkDir, worktreeDir, branch)
			return nil, fmt.Errorf("create worktree input dir: %w", err)
		}
		destPath := filepath.Join(inputDir, filepath.Base(input.DesignFile))
		if err := os.WriteFile(destPath, []byte(input.Spec), 0o644); err != nil {
			cleanupWorktree(context.Background(), input.WorkDir, worktreeDir, branch)
			return nil, fmt.Errorf("write spec to worktree: %w", err)
		}
		// Update to worktree-relative path so downstream nodes resolve correctly.
		input.DesignFile = filepath.Join(".belayer", ".internal", "input", filepath.Base(input.DesignFile))
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
		RepoDir:          input.WorkDir,
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

	// Resolve pipeline for workflow keeper.
	pipelineYAML, pipelineName, pipelineErr := intake.ResolvePipelineYAML(workDir)
	if pipelineErr != nil {
		log.Printf("Pipeline not found in %s: %v (workflow keeper disabled, HTTP handler will re-resolve per request)", workDir, pipelineErr)
	}
	var hasPollNodes bool
	var maxConcurrent int
	var subpipelineYAMLs map[string][]byte
	if len(pipelineYAML) > 0 {
		if cfg, err := pipeline.ParsePipeline(pipelineYAML); err == nil {
			// Check if first node has poll configuration.
			if len(cfg.Nodes) > 0 && cfg.Nodes[0].HasPoll() {
				hasPollNodes = true
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

	// Start workflow keeper if pipeline has poll nodes.
	if hasPollNodes {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go startWorkflowKeeper(ctx, c, workDir, maxConcurrent, pipelineYAML, pipelineName, subpipelineYAMLs)
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
	if hasPollNodes {
		fmt.Printf("  Intake:     poll node(s), auto-starting workflows\n")
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

// startWorkflowKeeper monitors active workflow count and auto-starts workflows
// for pipelines with poll nodes when capacity is available.
func startWorkflowKeeper(ctx context.Context, tc client.Client, workDir string, maxConcurrent int, pipelineYAML []byte, pipelineName string, subpipelineYAMLs map[string][]byte) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

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
				log.Printf("workflow keeper: count workflows failed: %v", err)
				continue
			}

			// If we have capacity, auto-start a workflow for poll nodes.
			if count.Count < int64(maxConcurrent) {
				// Start workflow with empty spec - poll node will provide input.
				out, err := startWorkflow(ctx, tc, startWorkflowInput{
					Spec:             "", // Empty spec - poll node provides input
					Source:           "poll",
					PipelineName:     pipelineName,
					PipelineYAML:     pipelineYAML,
					SubpipelineYAMLs: subpipelineYAMLs,
					WorkDir:          workDir,
				})
				if err != nil {
					log.Printf("workflow keeper: auto-start failed: %v", err)
					continue
				}
				log.Printf("workflow keeper: auto-started workflow %s", out.WorkflowID)
			}
		}
	}
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
