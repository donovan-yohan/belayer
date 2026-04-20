package bridgelog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTailLines_MissingFile(t *testing.T) {
	dir := t.TempDir()
	got := TailLines(filepath.Join(dir, "does-not-exist.log"), 50)
	if got != "(stderr log missing or empty)" {
		t.Fatalf("missing file: want sentinel, got %q", got)
	}
}

func TestTailLines_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	got := TailLines(path, 50)
	if got != "(stderr log missing or empty)" {
		t.Fatalf("empty file: want sentinel, got %q", got)
	}
}

func TestTailLines_FewerLinesThanN(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "short.log")
	lines := []string{"line1", "line2", "line3"}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := TailLines(path, 50)
	want := strings.Join(lines, "\n")
	if got != want {
		t.Fatalf("short file: got %q, want %q", got, want)
	}
}

func TestTailLines_ExactlyNLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exact.log")
	n := 5
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i+1)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := TailLines(path, n)
	want := strings.Join(lines, "\n")
	if got != want {
		t.Fatalf("exact N lines: got %q, want %q", got, want)
	}
}

func TestTailLines_MoreThanNLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.log")
	total := 200
	n := 50
	all := make([]string, total)
	for i := range all {
		all[i] = fmt.Sprintf("line%d", i+1)
	}
	if err := os.WriteFile(path, []byte(strings.Join(all, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := TailLines(path, n)
	gotLines := strings.Split(got, "\n")
	if len(gotLines) != n {
		t.Fatalf("want %d lines, got %d", n, len(gotLines))
	}
	// Last line must be the last line of the file.
	if gotLines[n-1] != fmt.Sprintf("line%d", total) {
		t.Fatalf("last line: got %q, want %q", gotLines[n-1], fmt.Sprintf("line%d", total))
	}
	// First line of the tail must be line(total-n+1).
	wantFirst := fmt.Sprintf("line%d", total-n+1)
	if gotLines[0] != wantFirst {
		t.Fatalf("first tail line: got %q, want %q", gotLines[0], wantFirst)
	}
}
