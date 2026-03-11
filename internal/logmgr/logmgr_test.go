package logmgr

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogPath(t *testing.T) {
	lm := New("/tmp/logs")

	got := lm.LogPath("task-1", "goal-a")
	want := filepath.Join("/tmp/logs", "task-1", "goal-a.log")
	assert.Equal(t, want, got)
}

func TestEnsureDir(t *testing.T) {
	dir := t.TempDir()
	lm := New(dir)

	err := lm.EnsureDir("task-1")
	require.NoError(t, err)

	taskDir := filepath.Join(dir, "task-1")
	info, err := os.Stat(taskDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestCheckRotation_SmallFile(t *testing.T) {
	dir := t.TempDir()
	lm := New(dir)
	lm.maxGoalLogSize = 1024 // 1KB limit for testing

	taskDir := filepath.Join(dir, "task-1")
	require.NoError(t, os.MkdirAll(taskDir, 0o755))

	logPath := lm.LogPath("task-1", "goal-a")
	content := []byte("small log content\n")
	require.NoError(t, os.WriteFile(logPath, content, 0o644))

	err := lm.CheckRotation("task-1", "goal-a")
	require.NoError(t, err)

	// File should be unchanged.
	got, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

func TestCheckRotation_OversizedFile(t *testing.T) {
	dir := t.TempDir()
	lm := New(dir)
	lm.maxGoalLogSize = 100 // 100 byte limit for testing

	taskDir := filepath.Join(dir, "task-1")
	require.NoError(t, os.MkdirAll(taskDir, 0o755))

	logPath := lm.LogPath("task-1", "goal-a")
	// Create a file larger than 100 bytes.
	content := strings.Repeat("x", 200)
	require.NoError(t, os.WriteFile(logPath, []byte(content), 0o644))

	err := lm.CheckRotation("task-1", "goal-a")
	require.NoError(t, err)

	got, err := os.ReadFile(logPath)
	require.NoError(t, err)

	// Should start with the truncation marker.
	assert.True(t, strings.HasPrefix(string(got), "[truncated at "))

	// Should contain the second half of the original content.
	assert.True(t, strings.Contains(string(got), strings.Repeat("x", 100)))

	// Should be smaller than the original.
	assert.Less(t, len(got), 200)
}

func TestCheckRotation_NonexistentFile(t *testing.T) {
	dir := t.TempDir()
	lm := New(dir)

	// Should not error on a file that doesn't exist.
	err := lm.CheckRotation("task-1", "goal-nonexistent")
	require.NoError(t, err)
}

func TestCompressTaskLogs(t *testing.T) {
	dir := t.TempDir()
	lm := New(dir)

	taskDir := filepath.Join(dir, "task-1")
	require.NoError(t, os.MkdirAll(taskDir, 0o755))

	// Create two log files.
	content1 := []byte("log content one\n")
	content2 := []byte("log content two\n")
	require.NoError(t, os.WriteFile(filepath.Join(taskDir, "goal-a.log"), content1, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(taskDir, "goal-b.log"), content2, 0o644))

	err := lm.CompressTaskLogs("task-1")
	require.NoError(t, err)

	// .log files should be removed.
	_, err = os.Stat(filepath.Join(taskDir, "goal-a.log"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(taskDir, "goal-b.log"))
	assert.True(t, os.IsNotExist(err))

	// .log.gz files should exist and contain the original content.
	for _, tc := range []struct {
		name    string
		content []byte
	}{
		{"goal-a.log.gz", content1},
		{"goal-b.log.gz", content2},
	} {
		gzPath := filepath.Join(taskDir, tc.name)
		f, err := os.Open(gzPath)
		require.NoError(t, err)
		defer f.Close()

		gr, err := gzip.NewReader(f)
		require.NoError(t, err)
		defer gr.Close()

		decompressed, err := io.ReadAll(gr)
		require.NoError(t, err)
		assert.Equal(t, tc.content, decompressed)
	}
}

func TestCleanup_RemovesOldCompressedFiles(t *testing.T) {
	dir := t.TempDir()
	lm := New(dir)
	lm.retentionDays = 1

	taskDir := filepath.Join(dir, "task-1")
	require.NoError(t, os.MkdirAll(taskDir, 0o755))

	// Create a .log.gz file and backdate its modification time.
	oldGz := filepath.Join(taskDir, "goal-old.log.gz")
	require.NoError(t, os.WriteFile(oldGz, []byte("old data"), 0o644))
	oldTime := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(oldGz, oldTime, oldTime))

	// Create a recent .log.gz file.
	newGz := filepath.Join(taskDir, "goal-new.log.gz")
	require.NoError(t, os.WriteFile(newGz, []byte("new data"), 0o644))

	err := lm.Cleanup()
	require.NoError(t, err)

	// Old file should be removed.
	_, err = os.Stat(oldGz)
	assert.True(t, os.IsNotExist(err))

	// New file should remain.
	_, err = os.Stat(newGz)
	assert.NoError(t, err)
}

func TestCleanup_RemovesEmptyTaskDirs(t *testing.T) {
	dir := t.TempDir()
	lm := New(dir)

	emptyDir := filepath.Join(dir, "task-empty")
	require.NoError(t, os.MkdirAll(emptyDir, 0o755))

	nonEmptyDir := filepath.Join(dir, "task-full")
	require.NoError(t, os.MkdirAll(nonEmptyDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(nonEmptyDir, "goal.log"), []byte("data"), 0o644))

	err := lm.Cleanup()
	require.NoError(t, err)

	_, err = os.Stat(emptyDir)
	assert.True(t, os.IsNotExist(err))

	_, err = os.Stat(nonEmptyDir)
	assert.NoError(t, err)
}

func TestCleanup_EnforcesCragSizeLimit(t *testing.T) {
	dir := t.TempDir()
	lm := New(dir)
	lm.maxCragSize = 100 // 100 bytes total limit

	// Create two task directories with files that exceed the limit.
	task1Dir := filepath.Join(dir, "task-old")
	require.NoError(t, os.MkdirAll(task1Dir, 0o755))
	oldFile := filepath.Join(task1Dir, "goal.log.gz")
	require.NoError(t, os.WriteFile(oldFile, make([]byte, 60), 0o644))
	// Backdate to make it "older".
	oldTime := time.Now().Add(-1 * time.Hour)
	require.NoError(t, os.Chtimes(oldFile, oldTime, oldTime))

	task2Dir := filepath.Join(dir, "task-new")
	require.NoError(t, os.MkdirAll(task2Dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(task2Dir, "goal.log.gz"), make([]byte, 60), 0o644))

	// Total is 120 bytes, limit is 100. Oldest should be removed.
	err := lm.Cleanup()
	require.NoError(t, err)

	_, err = os.Stat(task1Dir)
	assert.True(t, os.IsNotExist(err), "oldest task dir should be removed")

	_, err = os.Stat(task2Dir)
	assert.NoError(t, err, "newer task dir should remain")
}

func TestStats(t *testing.T) {
	dir := t.TempDir()
	lm := New(dir)

	task1Dir := filepath.Join(dir, "task-1")
	task2Dir := filepath.Join(dir, "task-2")
	require.NoError(t, os.MkdirAll(task1Dir, 0o755))
	require.NoError(t, os.MkdirAll(task2Dir, 0o755))

	content1 := []byte("aaaaaaaaaa") // 10 bytes
	content2 := []byte("bbbbb")      // 5 bytes
	content3 := []byte("ccccccc")    // 7 bytes
	require.NoError(t, os.WriteFile(filepath.Join(task1Dir, "goal-a.log"), content1, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(task1Dir, "goal-b.log"), content2, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(task2Dir, "goal-c.log.gz"), content3, 0o644))

	stats, err := lm.Stats()
	require.NoError(t, err)

	assert.Equal(t, int64(22), stats.TotalSize)
	assert.Equal(t, 3, stats.FileCount)
	assert.Equal(t, int64(15), stats.TaskSizes["task-1"])
	assert.Equal(t, int64(7), stats.TaskSizes["task-2"])
}

func TestStats_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	lm := New(dir)

	stats, err := lm.Stats()
	require.NoError(t, err)

	assert.Equal(t, int64(0), stats.TotalSize)
	assert.Equal(t, 0, stats.FileCount)
	assert.Empty(t, stats.TaskSizes)
}
