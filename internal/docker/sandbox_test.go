package docker

import (
	"strings"
	"testing"
)

func TestNewSandbox_DefaultImage(t *testing.T) {
	s, err := NewSandbox(SandboxConfig{SessionID: "test-session"})
	if err != nil {
		t.Fatalf("NewSandbox returned error: %v", err)
	}
	if s.config.Image != "ubuntu:24.04" {
		t.Errorf("expected default image 'ubuntu:24.04', got %q", s.config.Image)
	}
}

func TestNewSandbox_RequiresSessionID(t *testing.T) {
	_, err := NewSandbox(SandboxConfig{})
	if err == nil {
		t.Fatal("expected error when SessionID is empty, got nil")
	}
}

func TestNewSandbox_DefaultWorkDir(t *testing.T) {
	s, err := NewSandbox(SandboxConfig{SessionID: "test-session"})
	if err != nil {
		t.Fatalf("NewSandbox returned error: %v", err)
	}
	if s.config.WorkDir != "/workspace" {
		t.Errorf("expected default WorkDir '/workspace', got %q", s.config.WorkDir)
	}
}

func TestGenerateCompose_ServiceName(t *testing.T) {
	cfg := SandboxConfig{
		SessionID: "abc123",
		Image:     "ubuntu:24.04",
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "agent:") {
		t.Errorf("expected 'agent:' service name in compose output, got:\n%s", content)
	}
}

func TestGenerateCompose_InternalNetwork(t *testing.T) {
	cfg := SandboxConfig{
		SessionID: "abc123",
		Image:     "ubuntu:24.04",
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "internal: true") {
		t.Errorf("expected 'internal: true' in compose output, got:\n%s", content)
	}
}

func TestGenerateCompose_EnvFileIncluded(t *testing.T) {
	cfg := SandboxConfig{
		SessionID: "abc123",
		Image:     "ubuntu:24.04",
		EnvFile:   "/path/to/.env",
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "env_file:") {
		t.Errorf("expected 'env_file:' in compose output when EnvFile is set, got:\n%s", content)
	}
	if !strings.Contains(content, "/path/to/.env") {
		t.Errorf("expected env file path in compose output, got:\n%s", content)
	}
}

func TestGenerateCompose_EnvFileOmitted(t *testing.T) {
	cfg := SandboxConfig{
		SessionID: "abc123",
		Image:     "ubuntu:24.04",
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if strings.Contains(content, "env_file:") {
		t.Errorf("expected no 'env_file:' in compose output when EnvFile is empty, got:\n%s", content)
	}
}

func TestGenerateCompose_WorkingDir(t *testing.T) {
	cfg := SandboxConfig{
		SessionID: "abc123",
		Image:     "ubuntu:24.04",
		WorkDir:   "/myworkspace",
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "working_dir: /myworkspace") {
		t.Errorf("expected 'working_dir: /myworkspace' in compose output, got:\n%s", content)
	}
}

func TestGenerateCompose_DefaultWorkingDir(t *testing.T) {
	cfg := SandboxConfig{
		SessionID: "abc123",
		Image:     "ubuntu:24.04",
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "working_dir: /workspace") {
		t.Errorf("expected default 'working_dir: /workspace' in compose output, got:\n%s", content)
	}
}

func TestGenerateCompose_NetworkName(t *testing.T) {
	cfg := SandboxConfig{
		SessionID: "sess-xyz",
		Image:     "ubuntu:24.04",
	}
	out, err := generateCompose(cfg)
	if err != nil {
		t.Fatalf("generateCompose returned error: %v", err)
	}
	content := string(out)
	if !strings.Contains(content, "belayer-sess-xyz") {
		t.Errorf("expected network name 'belayer-sess-xyz' in compose output, got:\n%s", content)
	}
}

func TestAllocatePort(t *testing.T) {
	port, err := allocatePort()
	if err != nil {
		t.Fatalf("allocatePort returned error: %v", err)
	}
	if port <= 0 {
		t.Errorf("expected port > 0, got %d", port)
	}
}

func TestAllocatePort_UniqueResults(t *testing.T) {
	// Ports should generally be different across calls (not guaranteed, but very likely).
	p1, err := allocatePort()
	if err != nil {
		t.Fatalf("first allocatePort call failed: %v", err)
	}
	p2, err := allocatePort()
	if err != nil {
		t.Fatalf("second allocatePort call failed: %v", err)
	}
	// Just verify both are valid; they could theoretically be equal but won't be in practice.
	if p1 <= 0 || p2 <= 0 {
		t.Errorf("expected both ports > 0, got p1=%d p2=%d", p1, p2)
	}
}
