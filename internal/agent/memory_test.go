package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestMemory(t *testing.T) (*AgentMemory, string) {
	t.Helper()
	baseDir := t.TempDir()
	return NewAgentMemory(baseDir), baseDir
}

func TestAgentMemory_WriteFile_CreatesDirectoryAndFile(t *testing.T) {
	m, baseDir := newTestMemory(t)

	err := m.WriteFile("pilot", "codebase.md", "# Codebase\nSome notes.")
	if err != nil {
		t.Fatalf("WriteFile() unexpected error: %v", err)
	}

	expected := filepath.Join(baseDir, "pilot", "memory", "system", "codebase.md")
	data, err := os.ReadFile(expected)
	if err != nil {
		t.Fatalf("file not created at expected path %q: %v", expected, err)
	}
	if string(data) != "# Codebase\nSome notes." {
		t.Errorf("file content = %q, want %q", string(data), "# Codebase\nSome notes.")
	}
}

func TestAgentMemory_ReadFile_ReturnsContent(t *testing.T) {
	m, _ := newTestMemory(t)

	content := "# Patterns\nUse table-driven tests."
	if err := m.WriteFile("api-implementer", "patterns.md", content); err != nil {
		t.Fatalf("WriteFile() unexpected error: %v", err)
	}

	got, err := m.ReadFile("api-implementer", "patterns.md")
	if err != nil {
		t.Fatalf("ReadFile() unexpected error: %v", err)
	}
	if got != content {
		t.Errorf("ReadFile() = %q, want %q", got, content)
	}
}

func TestAgentMemory_ReadFile_NonexistentReturnsError(t *testing.T) {
	m, _ := newTestMemory(t)

	_, err := m.ReadFile("pilot", "nonexistent.md")
	if err == nil {
		t.Error("ReadFile() expected error for nonexistent file, got nil")
	}
}

func TestAgentMemory_ReadAll_ConcatenatesWithHeaders(t *testing.T) {
	m, _ := newTestMemory(t)

	if err := m.WriteFile("pilot", "coordination-patterns.md", "Use structured handoffs."); err != nil {
		t.Fatalf("WriteFile() unexpected error: %v", err)
	}
	if err := m.WriteFile("pilot", "review-priorities.md", "Security first."); err != nil {
		t.Fatalf("WriteFile() unexpected error: %v", err)
	}

	result, err := m.ReadAll("pilot")
	if err != nil {
		t.Fatalf("ReadAll() unexpected error: %v", err)
	}

	if !strings.Contains(result, "### coordination-patterns") {
		t.Errorf("ReadAll() missing header for coordination-patterns, got:\n%s", result)
	}
	if !strings.Contains(result, "### review-priorities") {
		t.Errorf("ReadAll() missing header for review-priorities, got:\n%s", result)
	}
	if !strings.Contains(result, "Use structured handoffs.") {
		t.Errorf("ReadAll() missing content for coordination-patterns, got:\n%s", result)
	}
	if !strings.Contains(result, "Security first.") {
		t.Errorf("ReadAll() missing content for review-priorities, got:\n%s", result)
	}
}

func TestAgentMemory_ReadAll_NoMemoryDirReturnsEmpty(t *testing.T) {
	m, _ := newTestMemory(t)

	result, err := m.ReadAll("nonexistent-agent")
	if err != nil {
		t.Fatalf("ReadAll() unexpected error for missing dir: %v", err)
	}
	if result != "" {
		t.Errorf("ReadAll() = %q, want empty string for missing dir", result)
	}
}

func TestAgentMemory_AppendToFile_AppendsContent(t *testing.T) {
	m, _ := newTestMemory(t)

	if err := m.WriteFile("pilot", "codebase.md", "# Initial"); err != nil {
		t.Fatalf("WriteFile() unexpected error: %v", err)
	}

	if err := m.AppendToFile("pilot", "codebase.md", "## Appended"); err != nil {
		t.Fatalf("AppendToFile() unexpected error: %v", err)
	}

	got, err := m.ReadFile("pilot", "codebase.md")
	if err != nil {
		t.Fatalf("ReadFile() unexpected error: %v", err)
	}
	if !strings.Contains(got, "# Initial") {
		t.Errorf("AppendToFile() lost original content, got: %q", got)
	}
	if !strings.Contains(got, "## Appended") {
		t.Errorf("AppendToFile() missing appended content, got: %q", got)
	}
	// Appended section must come after the original
	initialIdx := strings.Index(got, "# Initial")
	appendedIdx := strings.Index(got, "## Appended")
	if appendedIdx <= initialIdx {
		t.Errorf("AppendToFile() appended content appears before original, got: %q", got)
	}
}

func TestAgentMemory_AppendToFile_CreatesFileIfMissing(t *testing.T) {
	m, _ := newTestMemory(t)

	if err := m.AppendToFile("pilot", "new-file.md", "# Created by append"); err != nil {
		t.Fatalf("AppendToFile() unexpected error for new file: %v", err)
	}

	got, err := m.ReadFile("pilot", "new-file.md")
	if err != nil {
		t.Fatalf("ReadFile() unexpected error: %v", err)
	}
	if got != "# Created by append" {
		t.Errorf("AppendToFile() content = %q, want %q", got, "# Created by append")
	}
}

func TestAgentMemory_ListFiles_ReturnsFilenames(t *testing.T) {
	m, _ := newTestMemory(t)

	if err := m.WriteFile("api-implementer", "codebase.md", "content"); err != nil {
		t.Fatalf("WriteFile() unexpected error: %v", err)
	}
	if err := m.WriteFile("api-implementer", "patterns.md", "content"); err != nil {
		t.Fatalf("WriteFile() unexpected error: %v", err)
	}

	files, err := m.ListFiles("api-implementer")
	if err != nil {
		t.Fatalf("ListFiles() unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("ListFiles() returned %d files, want 2", len(files))
	}

	fileSet := make(map[string]bool)
	for _, f := range files {
		fileSet[f] = true
	}
	if !fileSet["codebase.md"] {
		t.Errorf("ListFiles() missing codebase.md, got: %v", files)
	}
	if !fileSet["patterns.md"] {
		t.Errorf("ListFiles() missing patterns.md, got: %v", files)
	}
}

func TestAgentMemory_ListFiles_NoMemoryDirReturnsEmpty(t *testing.T) {
	m, _ := newTestMemory(t)

	files, err := m.ListFiles("nonexistent-agent")
	if err != nil {
		t.Fatalf("ListFiles() unexpected error for missing dir: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("ListFiles() = %v, want empty slice for missing dir", files)
	}
}

func TestAgentMemory_EnsureDir_CreatesNestedDirectories(t *testing.T) {
	m, baseDir := newTestMemory(t)

	if err := m.EnsureDir("new-agent"); err != nil {
		t.Fatalf("EnsureDir() unexpected error: %v", err)
	}

	expected := filepath.Join(baseDir, "new-agent", "memory", "system")
	info, err := os.Stat(expected)
	if err != nil {
		t.Fatalf("EnsureDir() did not create directory at %q: %v", expected, err)
	}
	if !info.IsDir() {
		t.Errorf("EnsureDir() created a file, expected a directory at %q", expected)
	}
}

func TestAgentMemory_EnsureDir_IdempotentOnExistingDir(t *testing.T) {
	m, _ := newTestMemory(t)

	if err := m.EnsureDir("pilot"); err != nil {
		t.Fatalf("EnsureDir() first call unexpected error: %v", err)
	}
	if err := m.EnsureDir("pilot"); err != nil {
		t.Errorf("EnsureDir() second call unexpected error (should be idempotent): %v", err)
	}
}
