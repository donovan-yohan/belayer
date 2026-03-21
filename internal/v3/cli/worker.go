package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/donovan-yohan/belayer/internal/tmux"
	"github.com/donovan-yohan/belayer/internal/v3/intake"
	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/session"
	beltemporal "github.com/donovan-yohan/belayer/internal/v3/temporal"
)

const workerHTTPPort = 8780

func newWorkerCmd() *cobra.Command {
	var workDir string

	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Start the Temporal worker for pipeline execution",
		Long: `Start a long-lived Temporal worker that executes pipeline runs.

The worker registers the ClimbWorkflow and all activity implementations,
serves an HTTP API for the submit tool, and reconciles intake schedules.

Start this BEFORE running 'belayer start' or 'belayer climb'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWorker(workDir)
		},
	}

	cmd.Flags().StringVar(&workDir, "work-dir", "", "Working directory for sessions (default: current directory)")

	return cmd
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
	tm := tmux.NewRealTmux()
	spawner := session.NewTmuxSpawner(tm)
	activities := &beltemporal.Activities{Spawner: spawner}

	w := worker.New(c, beltemporal.TaskQueueName, worker.Options{})
	w.RegisterWorkflow(beltemporal.ClimbWorkflow)
	w.RegisterActivity(activities)

	// Start HTTP API server for submit/status tools.
	go startHTTPAPI(c, workDir)

	fmt.Printf("Belayer worker started\n")
	fmt.Printf("  Task queue: %s\n", beltemporal.TaskQueueName)
	fmt.Printf("  Work dir:   %s\n", workDir)
	fmt.Printf("  HTTP API:   http://127.0.0.1:%d\n", workerHTTPPort)
	fmt.Printf("\nWaiting for pipeline runs... (Ctrl+C to stop)\n")

	return w.Run(worker.InterruptCh())
}

// startHTTPAPI runs the HTTP API server for submit/status tool calls.
func startHTTPAPI(temporalClient client.Client, workDir string) {
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
		if spec.Spec == "" {
			http.Error(w, "spec is required", http.StatusBadRequest)
			return
		}

		// Resolve pipeline YAML.
		pipelineYAML, pipelineName, err := intake.ResolvePipelineYAML(workDir)
		if err != nil {
			http.Error(w, "Pipeline error: "+err.Error(), http.StatusBadRequest)
			return
		}
		if spec.PipelineName == "" {
			spec.PipelineName = pipelineName
		}
		if spec.Source == "" {
			spec.Source = "interactive"
		}
		if spec.ExternalID == "" {
			spec.ExternalID = fmt.Sprintf("submit-%d", time.Now().UnixMilli())
		}

		// Generate deterministic workflow ID and shared timestamp.
		workflowID := intake.GenerateWorkflowID(spec.PipelineName, spec.Source, spec.ExternalID)
		ts := time.Now().UnixMilli()

		// Create worktree and branch (shared timestamp prevents mismatch).
		branchSlug := intake.GenerateBranchSlug(spec.Spec)
		branch := fmt.Sprintf("belayer/%s-%d", branchSlug, ts)
		worktreeDir := fmt.Sprintf("%s/.belayer/worktrees/%d", workDir, ts)
		if err := intake.CreateGitWorktree(workDir, worktreeDir, branch); err != nil {
			http.Error(w, "Worktree error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Build ClimbInput and start workflow.
		climbInput := model.ClimbInput{
			Description:  spec.Spec,
			PipelineYAML: pipelineYAML,
			WorkDir:      worktreeDir,
			Branch:       branch,
			Repos:        spec.Repos,
		}

		opts := client.StartWorkflowOptions{
			ID:        workflowID,
			TaskQueue: beltemporal.TaskQueueName,
		}
		run, err := temporalClient.ExecuteWorkflow(r.Context(), opts, beltemporal.ClimbWorkflow, climbInput)
		if err != nil {
			http.Error(w, "Failed to start workflow: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"workflow_id": run.GetID(),
			"run_id":      run.GetRunID(),
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
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "Worker HTTP API failed: %v\n", err)
	}
}
