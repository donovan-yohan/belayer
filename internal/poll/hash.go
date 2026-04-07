package poll

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

type HashTracker struct {
	filePath string
	seen     map[string]bool
}

func NewHashTracker(dir, nodeName string) (*HashTracker, error) {
	filePath := filepath.Join(dir, "poll-hashes", nodeName)
	tracker := &HashTracker{
		filePath: filePath,
		seen:     make(map[string]bool),
	}

	data, err := os.ReadFile(filePath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read hash file: %w", err)
	}

	if len(data) > 0 {
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			line := scanner.Text()
			if line != "" {
				tracker.seen[line] = true
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("scan hash file: %w", err)
		}
	}

	return tracker, nil
}

func (t *HashTracker) Contains(hash string) bool {
	return t.seen[hash]
}

func (t *HashTracker) Add(hash string) error {
	if t.seen[hash] {
		return nil
	}

	dir := filepath.Dir(t.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create hash dir: %w", err)
	}

	f, err := os.OpenFile(t.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open hash file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(hash + "\n"); err != nil {
		return fmt.Errorf("write hash: %w", err)
	}

	t.seen[hash] = true
	return nil
}

func ComputeHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
