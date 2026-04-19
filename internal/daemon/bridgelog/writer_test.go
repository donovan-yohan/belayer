package bridgelog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriter_AppendsToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bridge.sup.log")
	w, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	n, err := w.Write([]byte("hello\n"))
	if err != nil || n != 6 {
		t.Fatalf("write: n=%d err=%v", n, err)
	}
	if _, err := w.Write([]byte("world\n")); err != nil {
		t.Fatalf("second write: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello\nworld\n" {
		t.Fatalf("content: %q", b)
	}
}

func TestRotate_KeepsLastN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bridge.sup.log")
	for i := 0; i < 5; i++ {
		w, err := New(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte{byte('a' + i)}); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := Rotate(path, 3); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// After 5 rotations with keep=3, at most .log.1, .log.2, .log.3 exist
	// (the base .log is renamed to .log.1 each iteration; a fresh .log is only
	// created on the next New() call — which we don't do here). Expect <=3.
	if len(entries) > 3 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("want <=3 entries, got %d: %v", len(entries), names)
	}
	// .log.4 must not exist
	if _, err := os.Stat(path + ".4"); !os.IsNotExist(err) {
		t.Fatalf(".log.4 should be gone, err=%v", err)
	}
}

func TestRotate_NoFileNoError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.log")
	if err := Rotate(path, 3); err != nil {
		t.Fatalf("rotate on missing: %v", err)
	}
}
