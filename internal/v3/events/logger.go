package events

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type Logger struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
}

func NewLogger(path string) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create event log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open event log: %w", err)
	}
	return &Logger{file: f, enc: json.NewEncoder(f)}, nil
}

func (l *Logger) Log(evt Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.enc.Encode(evt)
}

func (l *Logger) Close() error {
	return l.file.Close()
}
