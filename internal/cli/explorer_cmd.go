package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/donovan-yohan/belayer/internal/config"
	"github.com/donovan-yohan/belayer/internal/manage"
	"github.com/spf13/cobra"
)

type explorerWorkspaceAction string

const (
	explorerWorkspaceResume explorerWorkspaceAction = "resume"
	explorerWorkspaceFresh  explorerWorkspaceAction = "fresh"
)

var promptExplorerWorkspaceAction = readExplorerWorkspaceAction
var prepareExplorerDir = manage.PrepareExplorerDir

func newExplorerSessionCmd() *cobra.Command {
	var projectName string
	var prdPath string
	var yolo bool

	cmd := &cobra.Command{
		Use:   "explorer",
		Short: "Start an interactive explorer session for greenfield setup",
		Long:  "Launches a Claude Code session in an explorer workspace for research, decomposition, scaffold planning, and handoff into belayer problem creation.",
		RunE: func(cmd *cobra.Command, args []string) error {
			belayerDir, err := config.EnsureDir()
			if err != nil {
				return fmt.Errorf("ensuring belayer dir: %w", err)
			}

			resolvedPRDPath, err := resolveExplorerPRDPath(prdPath)
			if err != nil {
				return err
			}

			explorerDir, err := prepareExplorerWorkspace(cmd, filepath.Join(belayerDir, "explorer"), manage.ExplorerPromptData{
				Name:    projectName,
				PRDPath: resolvedPRDPath,
			})
			if err != nil {
				return err
			}

			return execClaudeSession(explorerDir, nil, yolo)
		},
	}

	cmd.Flags().StringVar(&projectName, "name", "", "Project name for the explorer workspace")
	cmd.Flags().StringVar(&prdPath, "prd", "", "Path to a PRD or requirements document to preload into the explorer session")
	cmd.Flags().BoolVar(&yolo, "yolo", false, "Skip permission prompts (passes --dangerously-skip-permissions to claude)")
	return cmd
}

func prepareExplorerWorkspace(cmd *cobra.Command, rootDir string, data manage.ExplorerPromptData) (string, error) {
	var previousWorkspaceBackup string

	if existingDir := manage.NamedExplorerWorkspaceDir(rootDir, data.Name); existingDir != "" {
		info, err := os.Stat(existingDir)
		switch {
		case err == nil:
			if !info.IsDir() {
				return "", fmt.Errorf("explorer workspace path %q exists and is not a directory", existingDir)
			}

			action, promptErr := promptExplorerWorkspaceAction(cmd.InOrStdin(), cmd.OutOrStdout(), existingDir)
			if promptErr != nil {
				return "", fmt.Errorf("choosing explorer workspace action: %w", promptErr)
			}
			if action == explorerWorkspaceFresh {
				previousWorkspaceBackup = existingDir + ".bak-" + time.Now().UTC().Format("20060102-150405.000000000")
				if err := os.Rename(existingDir, previousWorkspaceBackup); err != nil {
					return "", fmt.Errorf("parking existing explorer workspace %q: %w", existingDir, err)
				}
			}
		case os.IsNotExist(err):
			// No existing named workspace to resolve.
		default:
			return "", fmt.Errorf("checking explorer workspace %q: %w", existingDir, err)
		}
	}

	explorerDir, err := prepareExplorerDir(rootDir, data)
	if err != nil {
		if previousWorkspaceBackup != "" {
			existingDir := manage.NamedExplorerWorkspaceDir(rootDir, data.Name)
			if restoreErr := restoreExplorerWorkspace(existingDir, previousWorkspaceBackup); restoreErr != nil {
				return "", fmt.Errorf("preparing explorer workspace: %w (also failed to restore the previous workspace: %v)", err, restoreErr)
			}
		}
		return "", fmt.Errorf("preparing explorer workspace: %w", err)
	}

	if previousWorkspaceBackup != "" {
		if err := os.RemoveAll(previousWorkspaceBackup); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to remove previous explorer workspace backup %s: %v\n", previousWorkspaceBackup, err)
		}
	}

	return explorerDir, nil
}

func restoreExplorerWorkspace(workspaceDir string, backupDir string) error {
	if err := os.RemoveAll(workspaceDir); err != nil {
		return fmt.Errorf("removing partial replacement workspace: %w", err)
	}
	if err := os.Rename(backupDir, workspaceDir); err != nil {
		return fmt.Errorf("restoring previous workspace: %w", err)
	}
	return nil
}

func readExplorerWorkspaceAction(in io.Reader, out io.Writer, workspaceDir string) (explorerWorkspaceAction, error) {
	reader := bufio.NewReader(in)

	fmt.Fprintf(out, "Explorer workspace already exists at %s\n", workspaceDir)
	fmt.Fprint(out, "Resume existing session or start fresh? [resume/fresh] (default: resume): ")

	for {
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("reading workspace choice: %w", err)
		}

		switch strings.ToLower(strings.TrimSpace(line)) {
		case "", "r", "resume":
			return explorerWorkspaceResume, nil
		case "f", "fresh", "start fresh", "start-fresh":
			return explorerWorkspaceFresh, nil
		}

		if errors.Is(err, io.EOF) {
			return "", io.ErrUnexpectedEOF
		}

		fmt.Fprint(out, "Enter 'resume' or 'fresh': ")
	}
}

func resolveExplorerPRDPath(prdPath string) (string, error) {
	prdPath = strings.TrimSpace(prdPath)
	if prdPath == "" {
		return "", nil
	}

	absolutePath, err := filepath.Abs(prdPath)
	if err != nil {
		return "", fmt.Errorf("resolving PRD path %q: %w", prdPath, err)
	}

	info, err := os.Stat(absolutePath)
	if err != nil {
		return "", fmt.Errorf("checking PRD path %q: %w", prdPath, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("PRD path %q is a directory", prdPath)
	}

	return absolutePath, nil
}
