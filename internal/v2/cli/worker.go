package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/donovan-yohan/belayer/internal/tmux"
	"github.com/donovan-yohan/belayer/internal/v2/model"
	"github.com/donovan-yohan/belayer/internal/v2/pipeline"
	"github.com/donovan-yohan/belayer/internal/v2/provider"
	beltemporal "github.com/donovan-yohan/belayer/internal/v2/temporal"
)

const workerHTTPPort = 8780

func newWorkerCmd() *cobra.Command {
	var workDir string

	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Start the Temporal worker for pipeline execution",
		Long: `Start a Temporal worker that picks up pipeline runs and executes them.
The worker registers the Route workflow and all activity implementations.
It also serves an HTTP API for the submit tool.
It runs until interrupted (Ctrl+C).

Start this BEFORE running 'belayer start'.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return startWorker(workDir)
		},
	}

	cmd.Flags().StringVar(&workDir, "work-dir", "", "Working directory for sessions (default: current directory)")

	return cmd
}

func startWorker(workDir string) error {
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

	w := worker.New(c, beltemporal.TaskQueueName, worker.Options{})

	// Wire real providers into the activities.
	tm := tmux.NewRealTmux()
	channelScript, hooksDir := resolveChannelPaths()
	if channelScript != "" {
		fmt.Printf("  Channel:    %s\n", channelScript)
		fmt.Printf("  Hooks:      %s\n", hooksDir)
	}

	activities := &beltemporal.Activities{
		SessionSpawner: &workerSessionSpawner{
			claude:        provider.NewClaudeSessionSpawner(tm),
			codex:         provider.NewCodexSessionSpawner(tm),
			nextPort:      8791,
			channelScript: channelScript,
			hooksDir:      hooksDir,
		},
		ExecProvider: &provider.ExecProvider{},
		WorkDir:      workDir,
	}

	w.RegisterWorkflow(beltemporal.RouteWorkflow)
	w.RegisterActivity(activities)

	// Start HTTP API server for submit/status tools (goroutine).
	go startWorkerHTTP(c, workDir)

	fmt.Printf("Belayer worker started\n")
	fmt.Printf("  Task queue: %s\n", beltemporal.TaskQueueName)
	fmt.Printf("  Work dir:   %s\n", workDir)
	fmt.Printf("  HTTP API:   http://127.0.0.1:%d\n", workerHTTPPort)
	fmt.Printf("\nWaiting for pipeline runs... (Ctrl+C to stop)\n")

	return w.Run(worker.InterruptCh())
}

// startWorkerHTTP runs the HTTP API server for submit/status tool calls.
func startWorkerHTTP(temporalClient client.Client, workDir string) {
	mux := http.NewServeMux()

	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Spec     string   `json:"spec"`
			Repos    []string `json:"repos"`
			Pipeline string   `json:"pipeline"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.Spec == "" {
			http.Error(w, "spec is required", http.StatusBadRequest)
			return
		}

		// Parse pipeline (use default if not specified).
		var route *pipeline.Route
		var pipelineName string
		if req.Pipeline != "" {
			var err error
			route, err = pipeline.ParseRouteFile(req.Pipeline)
			if err != nil {
				http.Error(w, "Pipeline error: "+err.Error(), http.StatusBadRequest)
				return
			}
			pipelineName = route.Name
		} else {
			route, _, _ = findAndParsePipeline("")
			pipelineName = route.Name
		}

		routeJSON, _ := json.Marshal(route)
		input := model.RouteInput{
			Description: req.Spec,
			RouteJSON:   routeJSON,
		}

		opts := client.StartWorkflowOptions{
			ID:        fmt.Sprintf("belayer-route-%d", time.Now().UnixMilli()),
			TaskQueue: beltemporal.TaskQueueName,
		}

		run, err := temporalClient.ExecuteWorkflow(context.Background(), opts, beltemporal.RouteWorkflow, input)
		if err != nil {
			http.Error(w, "Failed to start workflow: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"workflow_id":   run.GetID(),
			"run_id":        run.GetRunID(),
			"pipeline_name": pipelineName,
		})
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Simple status — list active workflows.
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
			"note":   "Use belayer status CLI for detailed workflow listing",
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

// workerSessionSpawner delegates to Claude or Codex based on provider config.
type workerSessionSpawner struct {
	claude        *provider.ClaudeSessionSpawner
	codex         *provider.CodexSessionSpawner
	nextPort      int
	channelScript string
	hooksDir      string
}

func (w *workerSessionSpawner) Spawn(ctx context.Context, roleName, taskID, workDir string, input json.RawMessage) (string, error) {
	port := w.nextPort
	w.nextPort++

	opts := provider.SessionOpts{
		RoleName:      roleName,
		TaskID:        taskID,
		WorkDir:       workDir,
		InputJSON:     input,
		ChannelPort:   port,
		ObserverPort:  8790,
		ChannelScript: w.channelScript,
		HooksDir:      w.hooksDir,
	}
	info, err := w.claude.Spawn(ctx, opts)
	if err != nil {
		return "", err
	}
	return info.WindowName, nil
}
