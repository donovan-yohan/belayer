package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
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
	Spec         string
	Source       string
	DesignFile   string
	PipelineName string
	PipelineYAML []byte
	WorkDir      string
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
	worktreeDir := fmt.Sprintf("%s/.belayer/worktrees/%d", input.WorkDir, ts)
	if err := intake.CreateGitWorktree(input.WorkDir, worktreeDir, branch); err != nil {
		return nil, fmt.Errorf("worktree error: %w", err)
	}

	workflowID := intake.GenerateWorkflowID(input.PipelineName, input.Source, fmt.Sprintf("%d", ts))
	climbInput := model.ClimbInput{
		Description:  input.Spec,
		DesignFile:   input.DesignFile,
		PipelineYAML: input.PipelineYAML,
		WorkDir:      worktreeDir,
		Branch:       branch,
	}

	opts := client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: beltemporal.TaskQueueName,
	}
	run, err := tc.ExecuteWorkflow(ctx, opts, beltemporal.ClimbWorkflow, climbInput)
	if err != nil {
		return nil, fmt.Errorf("start workflow: %w", err)
	}

	return &startWorkflowOutput{WorkflowID: run.GetID(), RunID: run.GetRunID()}, nil
}

func runWorker(workDir string) error {
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
	}

	c, err := client.Dial(client.Options{})
	if err != nil {
		return fmt.Errorf("cannot connect to Temporal. Run 'belayer temporal start' first.\n\nError: %w", err)
	}
	defer c.Close()

	// Wire real providers into the activities.
	spawner := &session.ExecSpawner{}
	activities := &beltemporal.Activities{Spawner: spawner}

	w := worker.New(c, beltemporal.TaskQueueName, worker.Options{})
	w.RegisterWorkflow(beltemporal.ClimbWorkflow)
	w.RegisterActivity(activities)

	// Resolve pipeline for intake poller.
	pipelineYAML, pipelineName, _ := intake.ResolvePipelineYAML(workDir)
	var intakeChecks []string
	var maxConcurrent int
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
		}
	}

	// Start intake poller if triggers exist.
	if len(intakeChecks) > 0 {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go startIntakePoller(ctx, c, workDir, intakeChecks, maxConcurrent, pipelineYAML, pipelineName)
	}

	// Start HTTP API server for submit/status tools.
	httpErrCh := make(chan error, 1)
	go func() {
		httpErrCh <- startHTTPAPI(c, workDir, pipelineYAML, pipelineName)
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
func startHTTPAPI(temporalClient client.Client, workDir string, pipelineYAML []byte, pipelineName string) error {
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
		if len(resolvedYAML) == 0 {
			var err error
			resolvedYAML, resolvedName, err = intake.ResolvePipelineYAML(workDir)
			if err != nil {
				http.Error(w, "Pipeline error: "+err.Error(), http.StatusBadRequest)
				return
			}
		}
		if spec.PipelineName == "" {
			spec.PipelineName = resolvedName
		}

		out, err := startWorkflow(r.Context(), temporalClient, startWorkflowInput{
			Spec:         spec.Spec,
			Source:       spec.Source,
			DesignFile:   spec.Metadata["design_file"],
			PipelineName: spec.PipelineName,
			PipelineYAML: resolvedYAML,
			WorkDir:      workDir,
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
func startIntakePoller(ctx context.Context, tc client.Client, workDir string, checks []string, maxConcurrent int, pipelineYAML []byte, pipelineName string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	disabled := make(map[int]bool)

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
				if disabled[i] {
					continue
				}
				artifactPath, err := runIntakeCheck(workDir, check)
				if err != nil {
					log.Printf("intake poller: check %q disabled: %v", check, err)
					disabled[i] = true
					continue
				}
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
				out, err := startWorkflow(ctx, tc, startWorkflowInput{
					Spec:         string(specData),
					Source:       "trigger",
					DesignFile:   artifactPath,
					PipelineName: pipelineName,
					PipelineYAML: pipelineYAML,
					WorkDir:      workDir,
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
func runIntakeCheck(workDir, check string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "sh", "-c", check)
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
