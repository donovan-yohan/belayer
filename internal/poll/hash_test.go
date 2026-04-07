package poll

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComputeHash(t *testing.T) {
	got1 := ComputeHash([]byte("same input"))
	got2 := ComputeHash([]byte("same input"))
	if got1 != got2 {
		t.Fatalf("hashes differ: %q vs %q", got1, got2)
	}
	if got1 == ComputeHash([]byte("different input")) {
		t.Fatal("expected different input to produce different hash")
	}
}

func TestNewHashTracker_LoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "poll-hashes", "node-a")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	want := []string{"hash-1", "hash-2"}
	if err := os.WriteFile(path, []byte(strings.Join(want, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tracker, err := NewHashTracker(dir, "node-a")
	if err != nil {
		t.Fatalf("NewHashTracker: %v", err)
	}
	for _, hash := range want {
		if !tracker.Contains(hash) {
			t.Fatalf("expected tracker to contain %q", hash)
		}
	}
}

func TestHashTracker_Contains(t *testing.T) {
	tracker, err := NewHashTracker(t.TempDir(), "node-a")
	if err != nil {
		t.Fatalf("NewHashTracker: %v", err)
	}
	if tracker.Contains("missing") {
		t.Fatal("expected missing hash to be absent")
	}
	if err := tracker.Add("present"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !tracker.Contains("present") {
		t.Fatal("expected added hash to be present")
	}
}

func TestHashTracker_Add(t *testing.T) {
	dir := t.TempDir()
	tracker, err := NewHashTracker(dir, "node-a")
	if err != nil {
		t.Fatalf("NewHashTracker: %v", err)
	}
	if err := tracker.Add("hash-xyz"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !tracker.Contains("hash-xyz") {
		t.Fatal("expected tracker to contain added hash")
	}
	data, err := os.ReadFile(filepath.Join(dir, "poll-hashes", "node-a"))
	if err != nil {
		t.Fatalf("read hash file: %v", err)
	}
	if got := strings.TrimSpace(string(data)); got != "hash-xyz" {
		t.Fatalf("hash file = %q, want %q", got, "hash-xyz")
	}
}

func TestHashTracker_Persistence(t *testing.T) {
	dir := t.TempDir()
	tracker1, err := NewHashTracker(dir, "node-a")
	if err != nil {
		t.Fatalf("NewHashTracker: %v", err)
	}
	if err := tracker1.Add("hash-123"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	tracker2, err := NewHashTracker(dir, "node-a")
	if err != nil {
		t.Fatalf("NewHashTracker: %v", err)
	}
	if !tracker2.Contains("hash-123") {
		t.Fatal("expected second tracker to load persisted hash")
	}
}
