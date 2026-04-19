package trace

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// TestAppend_WritesAtOffset verifies that Fragment offsets and lengths are
// correct across two sequential appends to the same (session, agent).
func TestAppend_WritesAtOffset(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	f1, err := w.Append("sess1", "agentA", []byte("one"))
	if err != nil {
		t.Fatalf("Append one: %v", err)
	}
	if f1.Offset != 0 {
		t.Errorf("f1.Offset = %d, want 0", f1.Offset)
	}
	if f1.Length != 3 {
		t.Errorf("f1.Length = %d, want 3", f1.Length)
	}

	f2, err := w.Append("sess1", "agentA", []byte("two"))
	if err != nil {
		t.Fatalf("Append two: %v", err)
	}
	// "one\n" = 4 bytes, so offset is 4
	if f2.Offset != 4 {
		t.Errorf("f2.Offset = %d, want 4", f2.Offset)
	}
	if f2.Length != 3 {
		t.Errorf("f2.Length = %d, want 3", f2.Length)
	}

	// Verify both fragments point to the same file
	if f1.Path != f2.Path {
		t.Errorf("f1.Path=%q != f2.Path=%q", f1.Path, f2.Path)
	}

	// Read the file and verify content
	data, err := os.ReadFile(f1.Path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "one\ntwo\n"
	if string(data) != want {
		t.Errorf("file content = %q, want %q", string(data), want)
	}
}

// TestAppend_MultiAgentIsolation verifies that two different agents under the
// same session write to separate directories and both start at offset 0.
func TestAppend_MultiAgentIsolation(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	fa, err := w.Append("sess1", "agentA", []byte("hello"))
	if err != nil {
		t.Fatalf("Append agentA: %v", err)
	}
	fb, err := w.Append("sess1", "agentB", []byte("world"))
	if err != nil {
		t.Fatalf("Append agentB: %v", err)
	}

	if fa.Path == fb.Path {
		t.Errorf("agentA and agentB share the same path %q, want separate paths", fa.Path)
	}
	if fa.Offset != 0 {
		t.Errorf("agentA Offset = %d, want 0", fa.Offset)
	}
	if fb.Offset != 0 {
		t.Errorf("agentB Offset = %d, want 0", fb.Offset)
	}

	// Verify they live in different directories
	dirA := filepath.Dir(fa.Path)
	dirB := filepath.Dir(fb.Path)
	if dirA == dirB {
		t.Errorf("agentA and agentB share dir %q, want separate dirs", dirA)
	}
}

// TestRotate_AtThreshold verifies that once a fragment reaches the rotation
// threshold, the next Append goes to a new fragment (index 0002) at offset 0.
// The internal rotateThreshold variable is lowered via SetRotateThresholdForTest.
func TestRotate_AtThreshold(t *testing.T) {
	dir := t.TempDir()

	// Lower the threshold so we don't need to write 128 MB.
	oldThreshold := SetRotateThresholdForTest(1024)
	defer SetRotateThresholdForTest(oldThreshold)

	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	// Write enough data to meet the threshold (1024 bytes payload → triggers rotation on next Append).
	bigPayload := bytes.Repeat([]byte("x"), 1024)
	f1, err := w.Append("sess1", "agentA", bigPayload)
	if err != nil {
		t.Fatalf("Append big: %v", err)
	}
	if f1.Offset != 0 {
		t.Errorf("f1.Offset = %d, want 0", f1.Offset)
	}

	// This append should trigger rotation: current fragment is >= threshold.
	f2, err := w.Append("sess1", "agentA", []byte("small"))
	if err != nil {
		t.Fatalf("Append small: %v", err)
	}
	if f2.Offset != 0 {
		t.Errorf("f2.Offset = %d, want 0 (new fragment)", f2.Offset)
	}
	if f1.Path == f2.Path {
		t.Errorf("expected different fragment paths after rotation, got same: %q", f1.Path)
	}

	// The second fragment path should contain "0002"
	base2 := filepath.Base(f2.Path)
	if base2 != "0002.jsonl" {
		t.Errorf("second fragment base = %q, want 0002.jsonl", base2)
	}
}

// TestCloseAgent_CompressesFragments verifies that CloseAgent seals the
// active fragment, produces a .zst file, removes the original .jsonl, and
// that the .zst decompresses to the expected content.
func TestCloseAgent_CompressesFragments(t *testing.T) {
	dir := t.TempDir()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	payload := []byte("hello world")
	frag, err := w.Append("sess1", "agentA", payload)
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	if err := w.CloseAgent("sess1", "agentA"); err != nil {
		t.Fatalf("CloseAgent: %v", err)
	}

	// Original .jsonl should be gone.
	if _, err := os.Stat(frag.Path); !os.IsNotExist(err) {
		t.Errorf("expected %q to be removed after CloseAgent, but Stat returned: %v", frag.Path, err)
	}

	// .zst file should exist.
	zstPath := frag.Path + ".zst"
	if _, err := os.Stat(zstPath); err != nil {
		t.Fatalf("expected %q to exist, but Stat returned: %v", zstPath, err)
	}

	// Decompress and verify content.
	f, err := os.Open(zstPath)
	if err != nil {
		t.Fatalf("Open zst: %v", err)
	}
	defer f.Close()

	dec, err := zstd.NewReader(f)
	if err != nil {
		t.Fatalf("zstd.NewReader: %v", err)
	}
	defer dec.Close()

	got, err := io.ReadAll(dec)
	if err != nil {
		t.Fatalf("ReadAll zstd: %v", err)
	}

	want := append(payload, '\n')
	if !bytes.Equal(got, want) {
		t.Errorf("decompressed = %q, want %q", got, want)
	}
}

// TestCrashRecovery_TruncatesWhenNoNewlineIn64KB verifies that when a fragment
// file contains a single partial record larger than 64 KB (i.e. no newline in
// the first lookback window), the recovery loop scans all the way to BOF and
// truncates the entire file to 0, then the newly appended payload is the only
// content.
func TestCrashRecovery_TruncatesWhenNoNewlineIn64KB(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "sess", "agent")
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		t.Fatal(err)
	}
	fragPath := filepath.Join(agentDir, "0001.jsonl")

	// Write 200KB of non-newline bytes — a 200KB partial record.
	junk := bytes.Repeat([]byte("x"), 200*1024)
	if err := os.WriteFile(fragPath, junk, 0o600); err != nil {
		t.Fatal(err)
	}

	w, err := NewWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })

	// Trigger recovery by appending something.
	_, err = w.Append("sess", "agent", []byte(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(fragPath)
	if err != nil {
		t.Fatal(err)
	}
	// After recovery, file contains only the newly appended payload + newline.
	// The 200KB of junk must be GONE, not preserved.
	wantMaxSize := int64(64) // {"ok":true}\n is 12 bytes — generous upper bound
	if info.Size() > wantMaxSize {
		t.Errorf("file still holds %d bytes; partial tail not fully truncated", info.Size())
	}
}

// TestCrashRecovery_TruncatesPartialTail verifies that when an existing
// fragment file has a partial (unterminated) last record, opening a Writer
// and appending strips the partial record and appends cleanly after the last
// complete record.
func TestCrashRecovery_TruncatesPartialTail(t *testing.T) {
	dir := t.TempDir()

	// Manually pre-create the fragment directory and file simulating a crash.
	agentDir := filepath.Join(dir, "sess1", "agentA")
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	fragPath := filepath.Join(agentDir, "0001.jsonl")
	// "one\ntwo" — "two" has no trailing newline (partial tail).
	if err := os.WriteFile(fragPath, []byte("one\ntwo"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	w, err := NewWriter(dir)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	defer w.Close()

	_, err = w.Append("sess1", "agentA", []byte("three"))
	if err != nil {
		t.Fatalf("Append three: %v", err)
	}

	data, err := os.ReadFile(fragPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := "one\nthree\n"
	if string(data) != want {
		t.Errorf("file content = %q, want %q", string(data), want)
	}
}
