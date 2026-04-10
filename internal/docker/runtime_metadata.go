package docker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/donovan-yohan/belayer/internal/agent"
)

// RuntimeMetadata captures session runtime configuration that later CLI/daemon
// commands need after session start has finished.
type RuntimeMetadata struct {
	SessionID          string                          `json:"session_id"`
	SessionName        string                          `json:"session_name,omitempty"`
	Template           string                          `json:"template,omitempty"`
	Runtime            string                          `json:"runtime,omitempty"`
	Environment        string                          `json:"environment,omitempty"`
	EnvironmentPath    string                          `json:"environment_path,omitempty"`
	BelayerDir         string                          `json:"belayer_dir,omitempty"`
	WorkspaceRoot      string                          `json:"workspace_root,omitempty"`
	SandboxDir         string                          `json:"sandbox_dir,omitempty"`
	SandboxComposeFile string                          `json:"sandbox_compose_file,omitempty"`
	Workbench          *WorkbenchConfigSpec            `json:"workbench,omitempty"`
	WorkbenchDir       string                          `json:"workbench_dir,omitempty"`
	WorkbenchCompose   string                          `json:"workbench_compose_file,omitempty"`
	Tools              []agent.ToolSpec                `json:"tools,omitempty"`
	RepoWorktrees      map[string]string               `json:"repo_worktrees,omitempty"`
	AgentWorktrees     map[string]string               `json:"agent_worktrees,omitempty"`
	Agents             map[string]AgentRuntimeMetadata `json:"agents,omitempty"`
}

// AgentRuntimeMetadata describes a configured agent in the runtime metadata.
type AgentRuntimeMetadata struct {
	Name        string `json:"name"`
	Repo        string `json:"repo,omitempty"`
	Tier        string `json:"tier,omitempty"`
	SandboxName string `json:"sandbox_name,omitempty"`
}

// MetadataPath returns the canonical metadata file path for a sandbox dir.
func MetadataPath(sandboxDir string) string {
	return filepath.Join(sandboxDir, "runtime.json")
}

// WriteRuntimeMetadata writes runtime metadata to the sandbox directory.
func WriteRuntimeMetadata(sandboxDir string, meta RuntimeMetadata) error {
	if err := os.MkdirAll(sandboxDir, 0o700); err != nil {
		return fmt.Errorf("docker: create sandbox dir for metadata: %w", err)
	}
	if meta.SandboxDir == "" {
		meta.SandboxDir = sandboxDir
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("docker: marshal runtime metadata: %w", err)
	}
	if err := os.WriteFile(MetadataPath(sandboxDir), data, 0o600); err != nil {
		return fmt.Errorf("docker: write runtime metadata: %w", err)
	}
	return nil
}

// LoadRuntimeMetadata reads runtime metadata from the sandbox directory.
func LoadRuntimeMetadata(sandboxDir string) (RuntimeMetadata, error) {
	data, err := os.ReadFile(MetadataPath(sandboxDir))
	if err != nil {
		return RuntimeMetadata{}, fmt.Errorf("docker: read runtime metadata: %w", err)
	}
	var meta RuntimeMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return RuntimeMetadata{}, fmt.Errorf("docker: parse runtime metadata: %w", err)
	}
	if meta.SandboxDir == "" {
		meta.SandboxDir = sandboxDir
	}
	return meta, nil
}
