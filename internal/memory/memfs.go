package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MemFS manages the markdown file layer that is authoritative over SQLite.
// Files are stored under baseDir/{repo}/core.md and baseDir/{repo}/archival/{topic}.md.
type MemFS struct {
	baseDir string // e.g., ".belayer/learnings"
}

// NewMemFS creates a MemFS rooted at baseDir.
func NewMemFS(baseDir string) *MemFS {
	return &MemFS{baseDir: baseDir}
}

// WriteCoreFile writes/overwrites the core.md file for a repo.
// Format: each entry is "## {Key}\n{Value}\n\n"
func (m *MemFS) WriteCoreFile(repo string, entries []CoreEntry) error {
	dir := filepath.Join(m.baseDir, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("memfs: mkdir %q: %w", dir, err)
	}

	var sb strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&sb, "## %s\n%s\n\n", e.Key, e.Value)
	}

	path := filepath.Join(dir, "core.md")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("memfs: write core.md for repo %q: %w", repo, err)
	}
	return nil
}

// ReadCoreFile reads and parses core.md for a repo.
// Returns parsed entries. Returns an empty (non-nil) slice if the file doesn't exist.
func (m *MemFS) ReadCoreFile(repo string) ([]CoreEntry, error) {
	path := filepath.Join(m.baseDir, repo, "core.md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []CoreEntry{}, nil
		}
		return nil, fmt.Errorf("memfs: read core.md for repo %q: %w", repo, err)
	}

	return parseCoreFile(string(data)), nil
}

// parseCoreFile parses the core.md format: "## {Key}\n{Value}\n\n"
func parseCoreFile(content string) []CoreEntry {
	var entries []CoreEntry
	lines := strings.Split(content, "\n")

	var currentKey string
	var valueLines []string

	flush := func() {
		if currentKey == "" {
			return
		}
		value := strings.TrimSpace(strings.Join(valueLines, "\n"))
		entries = append(entries, CoreEntry{
			Key:   currentKey,
			Value: value,
		})
		currentKey = ""
		valueLines = nil
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			currentKey = strings.TrimPrefix(line, "## ")
		} else {
			if currentKey != "" {
				valueLines = append(valueLines, line)
			}
		}
	}
	flush()

	return entries
}

// WriteArchivalFile appends an archival learning to a topic file.
// Appends to baseDir/{repo}/archival/{topic}.md with a provenance header.
func (m *MemFS) WriteArchivalFile(repo, topic string, entry ArchivalEntry) error {
	dir := filepath.Join(m.baseDir, repo, "archival")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("memfs: mkdir archival dir for repo %q: %w", repo, err)
	}

	// Derive title from first line of content.
	title := firstLine(entry.Content)
	if title == "" {
		title = "Entry"
	}

	date := entry.CreatedAt.Format("2006-01-02")
	if entry.CreatedAt.IsZero() {
		date = time.Now().Format("2006-01-02")
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s\n", title)
	fmt.Fprintf(&sb, "*Session: %s | Source: %s | Date: %s*\n", entry.SessionID, entry.Source, date)
	if entry.Tags != "" {
		fmt.Fprintf(&sb, "*Tags: %s*\n", entry.Tags)
	}
	sb.WriteString("\n")
	sb.WriteString(entry.Content)
	sb.WriteString("\n\n---\n")

	path := filepath.Join(dir, topic+".md")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("memfs: open archival file %q: %w", path, err)
	}
	defer f.Close()

	if _, err := f.WriteString(sb.String()); err != nil {
		return fmt.Errorf("memfs: write archival file %q: %w", path, err)
	}
	return nil
}

// ReadArchivalFiles reads all archival markdown files for a repo.
// Returns all parsed entries across all topic files.
func (m *MemFS) ReadArchivalFiles(repo string) ([]ArchivalEntry, error) {
	dir := filepath.Join(m.baseDir, repo, "archival")
	infos, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []ArchivalEntry{}, nil
		}
		return nil, fmt.Errorf("memfs: read archival dir for repo %q: %w", repo, err)
	}

	var all []ArchivalEntry
	for _, info := range infos {
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, info.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("memfs: read archival file %q: %w", path, err)
		}
		entries := parseArchivalFile(string(data))
		all = append(all, entries...)
	}

	if all == nil {
		all = []ArchivalEntry{}
	}
	return all, nil
}

// parseArchivalFile parses the archival markdown format.
// Each entry is delimited by "---" and starts with "## {Title}".
func parseArchivalFile(content string) []ArchivalEntry {
	var entries []ArchivalEntry

	// Split on "---" separator lines.
	blocks := strings.Split(content, "\n---\n")
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		entry := parseArchivalBlock(block)
		if entry.Content != "" {
			entries = append(entries, entry)
		}
	}
	return entries
}

// parseArchivalBlock parses a single archival entry block.
func parseArchivalBlock(block string) ArchivalEntry {
	var entry ArchivalEntry
	lines := strings.Split(block, "\n")

	var contentLines []string
	inContent := false

	for i, line := range lines {
		if i == 0 && strings.HasPrefix(line, "## ") {
			// Title line — skip (derived from content).
			continue
		}

		// Provenance line: *Session: {id} | Source: {src} | Date: {date}*
		if strings.HasPrefix(line, "*Session:") && strings.HasSuffix(line, "*") {
			inner := strings.TrimPrefix(line, "*")
			inner = strings.TrimSuffix(inner, "*")
			parts := strings.Split(inner, " | ")
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "Session: ") {
					entry.SessionID = strings.TrimPrefix(part, "Session: ")
				} else if strings.HasPrefix(part, "Source: ") {
					entry.Source = strings.TrimPrefix(part, "Source: ")
				} else if strings.HasPrefix(part, "Date: ") {
					dateStr := strings.TrimPrefix(part, "Date: ")
					t, err := time.Parse("2006-01-02", dateStr)
					if err == nil {
						entry.CreatedAt = t
					}
				}
			}
			continue
		}

		// Tags line: *Tags: {tags}*
		if strings.HasPrefix(line, "*Tags:") && strings.HasSuffix(line, "*") {
			inner := strings.TrimPrefix(line, "*Tags: ")
			inner = strings.TrimSuffix(inner, "*")
			entry.Tags = strings.TrimSpace(inner)
			continue
		}

		// Blank line between header and content.
		if !inContent && line == "" {
			inContent = true
			continue
		}

		if inContent {
			contentLines = append(contentLines, line)
		}
	}

	entry.Content = strings.TrimSpace(strings.Join(contentLines, "\n"))
	return entry
}

// ListRepos returns all repo directory names under baseDir.
func (m *MemFS) ListRepos() ([]string, error) {
	infos, err := os.ReadDir(m.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("memfs: list repos in %q: %w", m.baseDir, err)
	}

	var repos []string
	for _, info := range infos {
		if info.IsDir() {
			repos = append(repos, info.Name())
		}
	}
	if repos == nil {
		repos = []string{}
	}
	return repos, nil
}

// firstLine returns the first non-empty line of s.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
