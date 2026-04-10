package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AgentMemory manages per-agent persistent memory stored as markdown files.
// Files live at {baseDir}/{agentName}/memory/system/{filename}.
type AgentMemory struct {
	baseDir string // e.g., ".belayer/agents"
}

// NewAgentMemory creates a manager rooted at baseDir.
func NewAgentMemory(baseDir string) *AgentMemory {
	return &AgentMemory{baseDir: baseDir}
}

// systemDir returns the path to an agent's system memory directory.
func (m *AgentMemory) systemDir(agentName string) string {
	return filepath.Join(m.baseDir, agentName, "memory", "system")
}

// EnsureDir creates the memory directory structure for an agent.
func (m *AgentMemory) EnsureDir(agentName string) error {
	dir := m.systemDir(agentName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("agent memory: ensure dir for %q: %w", agentName, err)
	}
	return nil
}

// WriteFile writes/overwrites a specific memory file for an agent.
// Creates the directory structure if it doesn't exist.
func (m *AgentMemory) WriteFile(agentName, filename, content string) error {
	if err := m.EnsureDir(agentName); err != nil {
		return err
	}
	path := filepath.Join(m.systemDir(agentName), filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("agent memory: write file %q for agent %q: %w", filename, agentName, err)
	}
	return nil
}

// ReadFile reads a specific memory file for an agent.
func (m *AgentMemory) ReadFile(agentName, filename string) (string, error) {
	path := filepath.Join(m.systemDir(agentName), filename)
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("agent memory: read file %q for agent %q: %w", filename, agentName, err)
	}
	return string(data), nil
}

// AppendToFile appends content to a memory file (creates if doesn't exist).
// A blank line separator is added between the existing content and the new content.
func (m *AgentMemory) AppendToFile(agentName, filename, content string) error {
	if err := m.EnsureDir(agentName); err != nil {
		return err
	}
	path := filepath.Join(m.systemDir(agentName), filename)

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("agent memory: append to file %q for agent %q: %w", filename, agentName, err)
	}

	var buf strings.Builder
	if len(existing) > 0 {
		buf.Write(existing)
		buf.WriteString("\n")
	}
	buf.WriteString(content)

	if err := os.WriteFile(path, []byte(buf.String()), 0644); err != nil {
		return fmt.Errorf("agent memory: append to file %q for agent %q: %w", filename, agentName, err)
	}
	return nil
}

// ListFiles returns all memory filenames for an agent.
// Returns an empty slice if the directory doesn't exist.
func (m *AgentMemory) ListFiles(agentName string) ([]string, error) {
	dir := m.systemDir(agentName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("agent memory: list files for agent %q: %w", agentName, err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	return files, nil
}

// ReadAll reads all markdown files from {baseDir}/{agentName}/memory/system/
// and concatenates them into a single string for prompt injection.
// Each file is prefixed with a section header using the filename without the .md extension.
// Returns empty string if the directory doesn't exist.
func (m *AgentMemory) ReadAll(agentName string) (string, error) {
	files, err := m.ListFiles(agentName)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "", nil
	}

	var buf strings.Builder
	for _, filename := range files {
		content, err := m.ReadFile(agentName, filename)
		if err != nil {
			return "", err
		}
		name := strings.TrimSuffix(filename, ".md")
		buf.WriteString("### ")
		buf.WriteString(name)
		buf.WriteString("\n\n")
		buf.WriteString(content)
		buf.WriteString("\n")
	}
	return buf.String(), nil
}
