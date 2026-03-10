package mail

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MailMessage represents a mail message returned by FileStore.List.
type MailMessage struct {
	ID          string
	Title       string
	Description string
}

// FileStore implements mail storage using the filesystem.
// Messages are JSON files in per-address directories with unread/ and read/ subdirectories.
type FileStore struct {
	dir string // base mail directory (e.g., <instanceDir>/mail)
}

// NewFileStore creates a FileStore rooted at the given directory.
func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

// fileMessage is the on-disk JSON format for a mail message.
type fileMessage struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	From        string `json:"from,omitempty"`
	To          string `json:"to"`
	MsgType     string `json:"msg_type"`
}

// Create writes a message as a JSON file in the recipient's unread/ directory.
func (f *FileStore) Create(title, description string, labels map[string]string) error {
	to := labels["to"]
	if to == "" {
		return fmt.Errorf("missing 'to' label")
	}

	unreadDir := filepath.Join(f.dir, to, "unread")
	if err := os.MkdirAll(unreadDir, 0o755); err != nil {
		return fmt.Errorf("creating unread dir: %w", err)
	}

	msg := fileMessage{
		Title:       title,
		Description: description,
		From:        labels["from"],
		To:          to,
		MsgType:     labels["msg-type"],
	}

	data, err := json.MarshalIndent(msg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}

	filename := fmt.Sprintf("%d-%s.json", time.Now().UnixNano(), sanitizeFilename(labels["msg-type"]))
	return os.WriteFile(filepath.Join(unreadDir, filename), data, 0o644)
}

// List returns unread messages for the given address.
func (f *FileStore) List(address string) ([]MailMessage, error) {
	unreadDir := filepath.Join(f.dir, address, "unread")
	entries, err := os.ReadDir(unreadDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading unread dir: %w", err)
	}

	var messages []MailMessage
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(unreadDir, entry.Name()))
		if err != nil {
			continue // skip unreadable files
		}
		var msg fileMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue // skip malformed files
		}
		messages = append(messages, MailMessage{
			ID:          entry.Name(),
			Title:       msg.Title,
			Description: msg.Description,
		})
	}
	return messages, nil
}

// Close moves a message from unread/ to read/ for the given address.
// The id is the filename (e.g., "1741234567-done.json").
func (f *FileStore) Close(id string) error {
	// Walk all address directories to find the file
	return filepath.WalkDir(f.dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if !d.IsDir() {
			return nil
		}
		if filepath.Base(path) != "unread" {
			return nil
		}

		src := filepath.Join(path, id)
		if _, statErr := os.Stat(src); statErr != nil {
			return nil // not in this directory
		}

		// Found it — move to read/
		readDir := filepath.Join(filepath.Dir(path), "read")
		if mkErr := os.MkdirAll(readDir, 0o755); mkErr != nil {
			return fmt.Errorf("creating read dir: %w", mkErr)
		}
		dst := filepath.Join(readDir, id)
		if mvErr := os.Rename(src, dst); mvErr != nil {
			return fmt.Errorf("moving %s to read: %w", id, mvErr)
		}
		return filepath.SkipAll // done
	})
}

// sanitizeFilename replaces characters unsafe for filenames.
func sanitizeFilename(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' {
			return '-'
		}
		return r
	}, s)
}
