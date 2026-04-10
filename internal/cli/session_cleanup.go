package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

type sessionCleanupPaths struct {
	SandboxDir       string
	LocalWorktreeDir string
}

func buildSessionCleanupPaths(homeDir, tempDir, sessionID, sessionName string) sessionCleanupPaths {
	return sessionCleanupPaths{
		SandboxDir:       filepath.Join(homeDir, ".belayer", "sandboxes", sessionID),
		LocalWorktreeDir: filepath.Join(tempDir, "belayer-worktrees", sessionName),
	}
}

func cleanupSessionArtifacts(paths sessionCleanupPaths, stdout, stderr io.Writer) error {
	var errs []error

	if err := stopDockerComposeIfPresent(paths.SandboxDir, stdout, stderr); err != nil {
		errs = append(errs, err)
	}

	for _, warning := range cleanupWorktrees(paths.SandboxDir) {
		errs = append(errs, warning)
	}
	if err := removeDirIfPresent(paths.SandboxDir); err != nil {
		errs = append(errs, fmt.Errorf("remove sandbox dir: %w", err))
	}

	for _, warning := range cleanupWorktrees(paths.LocalWorktreeDir) {
		errs = append(errs, warning)
	}
	if err := removeDirIfPresent(paths.LocalWorktreeDir); err != nil {
		errs = append(errs, fmt.Errorf("remove local worktree dir: %w", err))
	}

	return errors.Join(errs...)
}

func stopDockerComposeIfPresent(sandboxDir string, stdout, stderr io.Writer) error {
	if sandboxDir == "" {
		return nil
	}

	composePath := filepath.Join(sandboxDir, "docker-compose.yml")
	if _, err := os.Stat(composePath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat compose file: %w", err)
	}

	fmt.Fprintln(stdout, "Stopping Docker sandbox...")
	stopCmd := exec.Command("docker", "compose", "-f", composePath, "down")
	stopCmd.Stdout = stdout
	stopCmd.Stderr = stderr
	if err := stopCmd.Run(); err != nil {
		return fmt.Errorf("docker compose down: %w", err)
	}

	fmt.Fprintln(stdout, "Docker sandbox stopped.")
	return nil
}

func removeDirIfPresent(path string) error {
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return os.RemoveAll(path)
}
