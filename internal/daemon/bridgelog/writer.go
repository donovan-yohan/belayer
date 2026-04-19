// Package bridgelog writes bridge subprocess stdout/stderr to a per-agent file.
package bridgelog

import (
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

// Rotate renames path -> path.1 -> path.2 ... dropping anything past .keep.
// Missing source files are skipped without error.
func Rotate(path string, keep int) error {
	if keep < 1 {
		return nil
	}
	// Drop the oldest if at or past the keep limit.
	oldest := fmt.Sprintf("%s.%d", path, keep)
	if _, err := os.Stat(oldest); err == nil {
		if err := os.Remove(oldest); err != nil {
			return err
		}
	}
	// Shift: .log.(keep-1) -> .log.keep, ..., .log.1 -> .log.2, .log -> .log.1
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

var _ io.WriteCloser = (*Writer)(nil)
