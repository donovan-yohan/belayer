package frameworks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstall_BuiltinFramework(t *testing.T) {
	tmpDir := t.TempDir()
	targetDir := filepath.Join(tmpDir, ".belayer")
	os.MkdirAll(targetDir, 0o755)

	err := Install("claude-tmux", targetDir, false)
	if err != nil {
		t.Fatalf("Install returned error: %v", err)
	}

	// Verify pipeline.yaml was copied.
	if _, err := os.Stat(filepath.Join(targetDir, "pipeline.yaml")); err != nil {
		t.Error("pipeline.yaml not found after install")
	}

	// Verify scripts were copied.
	if _, err := os.Stat(filepath.Join(targetDir, "scripts", "run-node.sh")); err != nil {
		t.Error("scripts/run-node.sh not found after install")
	}
}

func TestInstall_ExistingPipelineNoForce(t *testing.T) {
	tmpDir := t.TempDir()
	targetDir := filepath.Join(tmpDir, ".belayer")
	os.MkdirAll(targetDir, 0o755)
	os.WriteFile(filepath.Join(targetDir, "pipeline.yaml"), []byte("existing"), 0o644)

	err := Install("claude-tmux", targetDir, false)
	if err == nil {
		t.Fatal("expected error for existing pipeline.yaml without --force")
	}
}

func TestInstall_ExistingPipelineWithForce(t *testing.T) {
	tmpDir := t.TempDir()
	targetDir := filepath.Join(tmpDir, ".belayer")
	os.MkdirAll(targetDir, 0o755)
	os.WriteFile(filepath.Join(targetDir, "pipeline.yaml"), []byte("old"), 0o644)

	err := Install("claude-tmux", targetDir, true)
	if err != nil {
		t.Fatalf("Install with force returned error: %v", err)
	}

	// Verify it was overwritten.
	data, _ := os.ReadFile(filepath.Join(targetDir, "pipeline.yaml"))
	if string(data) == "old" {
		t.Error("pipeline.yaml was not overwritten with --force")
	}
}

func TestInstall_UnknownFramework(t *testing.T) {
	tmpDir := t.TempDir()
	err := Install("nonexistent-framework", tmpDir, false)
	if err == nil {
		t.Fatal("expected error for unknown framework")
	}
}

func TestList_ContainsClaudeTmux(t *testing.T) {
	names, err := List()
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}
	found := false
	for _, n := range names {
		if n == "claude-tmux" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("List() = %v, want claude-tmux to be present", names)
	}
}

func TestEnsureInternalDir(t *testing.T) {
	tmpDir := t.TempDir()
	err := EnsureInternalDir(tmpDir)
	if err != nil {
		t.Fatalf("EnsureInternalDir returned error: %v", err)
	}

	gitignore := filepath.Join(tmpDir, ".belayer", ".internal", ".gitignore")
	data, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if string(data) != "*\n" {
		t.Errorf(".gitignore content = %q, want %q", string(data), "*\n")
	}
}
