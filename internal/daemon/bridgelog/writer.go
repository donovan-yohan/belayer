// Package bridgelog writes bridge subprocess stdout/stderr to a per-agent file.
package bridgelog

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Writer is a thread-safe appending file writer.
type Writer struct {
	mu sync.Mutex
	f  *os.File
}

// New opens path for append, creating parent dirs (0o700) and the file (0o600).
func New(path string) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	return &Writer{f: f}, nil
}

// Write appends p to the underlying file.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Write(p)
}

// Close closes the underlying file.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}

// Rotate shifts the log history: path -> path.1 -> path.2 ... -> path.keep,
// atomically dropping anything that would fall past path.keep via rename
// overwrites. Missing source files are skipped.
//
// Contract: the caller MUST ensure no *Writer is open on path when Rotate
// runs. Rotating a live fd leaks appended bytes into the archive (the kernel
// keeps writing to the renamed inode). Use RotateAndOpen for the typical
// "rotate, then reopen" pattern.
func Rotate(path string, keep int) error {
	if keep < 1 {
		return nil
	}
	for i := keep - 1; i >= 0; i-- {
		src := path
		if i > 0 {
			src = fmt.Sprintf("%s.%d", path, i)
		}
		dst := fmt.Sprintf("%s.%d", path, i+1)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	}
	return nil
}

// RotateAndOpen rotates path (as Rotate does) and then opens a fresh Writer
// on path. This is the safe composition for callers that own the file's
// lifecycle.
func RotateAndOpen(path string, keep int) (*Writer, error) {
	if err := Rotate(path, keep); err != nil {
		return nil, err
	}
	return New(path)
}

var _ io.WriteCloser = (*Writer)(nil)

// TailLines reads the last n lines from the file at path. If the file does not
// exist or cannot be read, an empty string is returned. If the file has fewer
// than n lines, all lines are returned. The result is newline-joined.
func TailLines(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	// Collect all lines via a scanner; keep only the last n.
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if len(lines) == 0 {
		return ""
	}
	start := len(lines) - n
	if start < 0 {
		start = 0
	}
	result := ""
	for i, l := range lines[start:] {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return result
}
