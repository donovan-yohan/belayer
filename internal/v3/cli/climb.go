package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	"github.com/donovan-yohan/belayer/internal/tmux"
	"github.com/donovan-yohan/belayer/internal/v3/events"
	"github.com/donovan-yohan/belayer/internal/v3/model"
	"github.com/donovan-yohan/belayer/internal/v3/pipeline"
	"github.com/donovan-yohan/belayer/internal/v3/session"
	beltemporal "github.com/donovan-yohan/belayer/internal/v3/temporal"
)

// NewClimbCmd returns the `belayer climb` cobra command.
func NewClimbCmd() *cobra.Command {
	var fileFlag, promptFlag, nodeFlag, inputFlag string
	var detach bool

	cmd := &cobra.Command{
		Use:   "climb [description]",
		Short: "Start a pipeline climb (pipeline entry point)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}

			// 1. Resolve input.
			designFile, description, err := resolveClimbInput(fileFlag, promptFlag, args)
			if err != nil {
				return fmt.Errorf("resolve input: %w", err)
			}

			// 2. Resolve pipeline YAML.
			pipelineYAML, pipelineName, err := resolvePipelineYAML(cwd)
			if err != nil {
				return fmt.Errorf("resolve pipeline: %w", err)
			}

			// 3. Connect to Temporal.
			tc, err := client.Dial(client.Options{})
			if err != nil {
				return fmt.Errorf("connect to Temporal: %w", err)
			}
			defer tc.Close()

			// 4. Generate workflow ID and create git branch.
			workflowID := fmt.Sprintf("climb-%d", time.Now().UnixMilli())
			branch := fmt.Sprintf("belayer/%s", workflowID)
			if err := createGitBranch(cwd, branch); err != nil {
				return fmt.Errorf("create git branch: %w", err)
			}

			// 5. Start in-process worker.
			tm := tmux.NewRealTmux()
			spawner := session.NewTmuxSpawner(tm)
			activities := &beltemporal.Activities{Spawner: spawner}

			w := worker.New(tc, beltemporal.TaskQueueName, worker.Options{})
			w.RegisterWorkflow(beltemporal.ClimbWorkflow)
			w.RegisterActivity(activities)
			if err := w.Start(); err != nil {
				return fmt.Errorf("start Temporal worker: %w", err)
			}
			defer w.Stop()

			// 6. Initialize event logger.
			eventsPath := filepath.Join(cwd, ".belayer", "runs", workflowID, "events.jsonl")
			logger, err := events.NewLogger(eventsPath)
			if err != nil {
				return fmt.Errorf("init event logger: %w", err)
			}
			defer logger.Close()

			inputDesc := description
			if designFile != "" {
				inputDesc = designFile
			}
			if err := logger.Log(events.PipelineStarted(workflowID, pipelineName, inputDesc)); err != nil {
				return fmt.Errorf("log pipeline started: %w", err)
			}

			// 7. Build and start workflow.
			climbInput := model.ClimbInput{
				Description:  description,
				DesignFile:   designFile,
				PipelineYAML: pipelineYAML,
				FromNode:     nodeFlag,
				InputPath:    inputFlag,
				WorkDir:      cwd,
				Branch:       branch,
			}

			run, err := tc.ExecuteWorkflow(
				cmd.Context(),
				client.StartWorkflowOptions{
					ID:        workflowID,
					TaskQueue: beltemporal.TaskQueueName,
				},
				beltemporal.ClimbWorkflow,
				climbInput,
			)
			if err != nil {
				return fmt.Errorf("start workflow: %w", err)
			}

			fmt.Printf("climb started: workflow=%s branch=%s\n", run.GetID(), branch)

			if detach {
				fmt.Printf("running in background (--detach). Run ID: %s\n", run.GetRunID())
				return nil
			}

			// 8. Block until workflow completes.
			var output model.ClimbOutput
			if err := run.Get(cmd.Context(), &output); err != nil {
				return fmt.Errorf("workflow error: %w", err)
			}

			// 9. Print result.
			switch output.Status {
			case model.ClimbCompleted:
				fmt.Printf("climb completed: branch=%s\n", output.Branch)
			case model.ClimbFailed:
				fmt.Printf("climb failed: %s (branch=%s)\n", output.Message, output.Branch)
			default:
				fmt.Printf("climb finished with status=%s branch=%s\n", output.Status, output.Branch)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&fileFlag, "file", "", "Design doc file path")
	cmd.Flags().StringVar(&promptFlag, "prompt", "", "Text prompt as pipeline input")
	cmd.Flags().StringVar(&nodeFlag, "node", "", "Resume from this node")
	cmd.Flags().StringVar(&inputFlag, "input", "", "Input artifact path for --node")
	cmd.Flags().BoolVar(&detach, "detach", false, "Non-blocking mode — return immediately after starting")

	return cmd
}

// resolveClimbInput returns (designFile, description) from the provided flags/args/stdin.
// Priority: --file > --prompt > args > stdin.
func resolveClimbInput(fileFlag, promptFlag string, args []string) (designFile, description string, err error) {
	if fileFlag != "" {
		data, err := os.ReadFile(fileFlag)
		if err != nil {
			return "", "", fmt.Errorf("read design file %q: %w", fileFlag, err)
		}
		return fileFlag, strings.TrimSpace(string(data)), nil
	}

	if promptFlag != "" {
		return "", promptFlag, nil
	}

	if len(args) > 0 {
		return "", strings.Join(args, " "), nil
	}

	// Try stdin.
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", "", fmt.Errorf("read stdin: %w", err)
		}
		text := strings.TrimSpace(string(data))
		if text != "" {
			return "", text, nil
		}
	}

	return "", "", fmt.Errorf("no input provided: use --file, --prompt, positional args, or stdin")
}

// resolvePipelineYAML returns pipeline YAML bytes and a name by checking known locations,
// falling back to the built-in default.
func resolvePipelineYAML(cwd string) ([]byte, string, error) {
	candidates := []string{
		filepath.Join(cwd, "belayer-pipeline.yaml"),
		filepath.Join(cwd, ".belayer", "pipeline.yaml"),
	}

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		cfg, err := pipeline.ParsePipeline(data)
		if err != nil {
			return nil, "", fmt.Errorf("parse pipeline %q: %w", path, err)
		}
		return data, cfg.Name, nil
	}

	// Built-in default.
	cfg, err := pipeline.ParsePipeline([]byte(pipeline.DefaultPipelineYAML))
	if err != nil {
		return nil, "", fmt.Errorf("parse default pipeline: %w", err)
	}
	return []byte(pipeline.DefaultPipelineYAML), cfg.Name, nil
}

// createGitBranch creates and checks out a new git branch in workDir.
func createGitBranch(workDir, branch string) error {
	cmd := exec.Command("git", "checkout", "-b", branch)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git checkout -b %s: %s: %w", branch, strings.TrimSpace(string(out)), err)
	}
	return nil
}
