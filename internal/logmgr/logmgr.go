package logmgr

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LogManager handles log lifecycle for the setter daemon, including rotation,
// compression, and cleanup of per-goal log files.
type LogManager struct {
	logsDir        string // base directory for logs (e.g., <cragDir>/logs)
	maxGoalLogSize int64  // per-goal log size limit in bytes (default 10MB)
	maxCragSize    int64  // total crag log size limit in bytes (default 500MB)
	retentionDays   int    // days to keep compressed logs (default 7)
}

// LogStats holds disk usage statistics for the log directory.
type LogStats struct {
	TotalSize int64            // total bytes across all logs
	TaskSizes map[string]int64 // taskID -> total bytes
	FileCount int              // total number of log files
}

// New creates a LogManager rooted at the given logs directory.
func New(logsDir string) *LogManager {
	return &LogManager{
		logsDir:         logsDir,
		maxGoalLogSize:  10 * 1024 * 1024,  // 10MB
		maxCragSize: 500 * 1024 * 1024, // 500MB
		retentionDays:   7,
	}
}

// LogPath returns the path for a goal's log file: <logsDir>/<taskID>/<goalID>.log
func (lm *LogManager) LogPath(taskID, goalID string) string {
	return filepath.Join(lm.logsDir, taskID, goalID+".log")
}

// EnsureDir creates the task log directory if it doesn't exist.
func (lm *LogManager) EnsureDir(taskID string) error {
	dir := filepath.Join(lm.logsDir, taskID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating log directory %s: %w", dir, err)
	}
	return nil
}

// CheckRotation checks if the goal's log file exceeds maxGoalLogSize.
// If so, it truncates the first half and prepends a marker line.
func (lm *LogManager) CheckRotation(taskID, goalID string) error {
	path := lm.LogPath(taskID, goalID)

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat log file %s: %w", path, err)
	}

	if info.Size() <= lm.maxGoalLogSize {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading log file %s: %w", path, err)
	}

	// Keep the second half of the file.
	half := len(data) / 2
	kept := data[half:]

	marker := fmt.Sprintf("[truncated at %s]\n", time.Now().UTC().Format(time.RFC3339))
	truncated := append([]byte(marker), kept...)

	if err := os.WriteFile(path, truncated, 0o644); err != nil {
		return fmt.Errorf("writing truncated log file %s: %w", path, err)
	}

	return nil
}

// CompressTaskLogs compresses all .log files in the task directory to .log.gz
// and removes the original .log files.
func (lm *LogManager) CompressTaskLogs(taskID string) error {
	dir := filepath.Join(lm.logsDir, taskID)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading task log directory %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}

		logPath := filepath.Join(dir, entry.Name())
		gzPath := logPath + ".gz"

		if err := compressFile(logPath, gzPath); err != nil {
			return fmt.Errorf("compressing %s: %w", logPath, err)
		}

		if err := os.Remove(logPath); err != nil {
			return fmt.Errorf("removing original log %s: %w", logPath, err)
		}
	}

	return nil
}

// compressFile gzip-compresses src into dst.
func compressFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source: %w", err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("creating gzip file: %w", err)
	}
	defer out.Close()

	gw := gzip.NewWriter(out)
	defer gw.Close()

	if _, err := io.Copy(gw, in); err != nil {
		return fmt.Errorf("writing gzip data: %w", err)
	}

	return gw.Close()
}

// Cleanup removes compressed log files older than retentionDays, removes empty
// task directories, and removes oldest task directories if total size exceeds
// maxInstanceSize.
func (lm *LogManager) Cleanup() error {
	cutoff := time.Now().Add(-time.Duration(lm.retentionDays) * 24 * time.Hour)

	taskDirs, err := os.ReadDir(lm.logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading logs directory %s: %w", lm.logsDir, err)
	}

	// Phase 1: Remove old compressed files.
	for _, td := range taskDirs {
		if !td.IsDir() {
			continue
		}
		taskDir := filepath.Join(lm.logsDir, td.Name())
		entries, err := os.ReadDir(taskDir)
		if err != nil {
			return fmt.Errorf("reading task directory %s: %w", taskDir, err)
		}

		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log.gz") {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf("getting info for %s: %w", entry.Name(), err)
			}
			if info.ModTime().Before(cutoff) {
				path := filepath.Join(taskDir, entry.Name())
				if err := os.Remove(path); err != nil {
					return fmt.Errorf("removing old compressed log %s: %w", path, err)
				}
			}
		}
	}

	// Phase 2: Remove empty task directories.
	taskDirs, err = os.ReadDir(lm.logsDir)
	if err != nil {
		return fmt.Errorf("re-reading logs directory: %w", err)
	}

	for _, td := range taskDirs {
		if !td.IsDir() {
			continue
		}
		taskDir := filepath.Join(lm.logsDir, td.Name())
		entries, err := os.ReadDir(taskDir)
		if err != nil {
			return fmt.Errorf("reading task directory %s: %w", taskDir, err)
		}
		if len(entries) == 0 {
			if err := os.Remove(taskDir); err != nil {
				return fmt.Errorf("removing empty task directory %s: %w", taskDir, err)
			}
		}
	}

	// Phase 3: Check total crag size and remove oldest task directories if over limit.
	if err := lm.enforceCragSize(); err != nil {
		return fmt.Errorf("enforcing crag size limit: %w", err)
	}

	return nil
}

// taskDirInfo holds metadata about a task directory for size enforcement.
type taskDirInfo struct {
	name    string
	path    string
	size    int64
	modTime time.Time
}

// enforceCragSize removes the oldest task directories until total size is
// within maxCragSize.
func (lm *LogManager) enforceCragSize() error {
	taskDirs, err := os.ReadDir(lm.logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading logs directory: %w", err)
	}

	var dirs []taskDirInfo
	var totalSize int64

	for _, td := range taskDirs {
		if !td.IsDir() {
			continue
		}
		taskDir := filepath.Join(lm.logsDir, td.Name())
		size, latestMod, err := dirSizeAndLatest(taskDir)
		if err != nil {
			return fmt.Errorf("calculating size of %s: %w", taskDir, err)
		}
		dirs = append(dirs, taskDirInfo{
			name:    td.Name(),
			path:    taskDir,
			size:    size,
			modTime: latestMod,
		})
		totalSize += size
	}

	if totalSize <= lm.maxCragSize {
		return nil
	}

	// Sort oldest first.
	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].modTime.Before(dirs[j].modTime)
	})

	for _, d := range dirs {
		if totalSize <= lm.maxCragSize {
			break
		}
		if err := os.RemoveAll(d.path); err != nil {
			return fmt.Errorf("removing task directory %s: %w", d.path, err)
		}
		totalSize -= d.size
	}

	return nil
}

// dirSizeAndLatest returns the total size of all files in a directory and the
// latest modification time among them.
func dirSizeAndLatest(dir string) (int64, time.Time, error) {
	var total int64
	var latest time.Time

	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, latest, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return 0, latest, err
		}
		total += info.Size()
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}

	return total, latest, nil
}

// Stats returns disk usage statistics for the logs directory.
func (lm *LogManager) Stats() (LogStats, error) {
	stats := LogStats{
		TaskSizes: make(map[string]int64),
	}

	taskDirs, err := os.ReadDir(lm.logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return stats, nil
		}
		return stats, fmt.Errorf("reading logs directory %s: %w", lm.logsDir, err)
	}

	for _, td := range taskDirs {
		if !td.IsDir() {
			continue
		}
		taskDir := filepath.Join(lm.logsDir, td.Name())
		entries, err := os.ReadDir(taskDir)
		if err != nil {
			return stats, fmt.Errorf("reading task directory %s: %w", taskDir, err)
		}

		var taskSize int64
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				return stats, fmt.Errorf("getting info for %s: %w", entry.Name(), err)
			}
			taskSize += info.Size()
			stats.FileCount++
		}
		stats.TaskSizes[td.Name()] = taskSize
		stats.TotalSize += taskSize
	}

	return stats, nil
}
