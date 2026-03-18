package belayer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/logmgr"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/donovan-yohan/belayer/internal/store"
	"github.com/donovan-yohan/belayer/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractTestContract_Found(t *testing.T) {
	spec := `# My Spec

## Overview
Some overview text.

## Test Contract
- All endpoints must return 200 on success
- Auth tokens must be validated

## Implementation Notes
Some notes here.
`
	got := extractTestContract(spec)
	assert.Equal(t, "- All endpoints must return 200 on success\n- Auth tokens must be validated", got)
}

func TestExtractTestContract_AtEnd(t *testing.T) {
	spec := `# My Spec

## Overview
Some overview.

## Test Contract
- Tests must cover happy and error paths
- Coverage must be >= 80%`
	got := extractTestContract(spec)
	assert.Equal(t, "- Tests must cover happy and error paths\n- Coverage must be >= 80%", got)
}

func TestExtractTestContract_NotFound(t *testing.T) {
	spec := `# My Spec

## Overview
Some overview text.

## Implementation Notes
Some notes here.
`
	got := extractTestContract(spec)
	assert.Equal(t, "", got)
}

func TestExtractTestContract_Empty(t *testing.T) {
	spec := `# My Spec

## Overview
Some overview.

## Test Contract
## Implementation Notes
Some notes here.
`
	got := extractTestContract(spec)
	assert.Equal(t, "", got)
}

func TestLooksLikeTrustDialog(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "claude trust dialog",
			content: "Welcome to Claude Code!\n\nDo you trust the files in this folder?\n\n  /tmp/belayer/worktree\n\n(y/N)",
			want:    true,
		},
		{
			name:    "trust this folder variant",
			content: "Trust this folder?\nThis workspace has not been opened before.\n(y/N)",
			want:    true,
		},
		{
			name:    "trust the files variant",
			content: "Do you trust the files in /some/path?\n[y/N]",
			want:    true,
		},
		{
			name:    "allow access variant",
			content: "Allow access to this directory?\n(Y/n)",
			want:    true,
		},
		{
			name:    "trust this project",
			content: "Do you want to trust this project?\nYes, proceed / No",
			want:    true,
		},
		{
			name:    "confirmation prompt y/n",
			content: "Some prompt text\n(y/N)",
			want:    true,
		},
		{
			name:    "confirmation prompt Y/n",
			content: "Workspace setup required\n[Y/n]",
			want:    true,
		},
		{
			name:    "continue prompt",
			content: "First time in this directory.\nContinue?",
			want:    true,
		},
		{
			name:    "normal claude working output",
			content: "Reading file src/main.go...\n\nI'll update the function to handle the error case.\n\n",
			want:    false,
		},
		{
			name:    "claude input prompt",
			content: "Some previous output\n> ",
			want:    false,
		},
		{
			name:    "empty content",
			content: "",
			want:    false,
		},
		{
			name:    "just whitespace",
			content: "   \n\n   ",
			want:    false,
		},
		{
			name:    "codex working output",
			content: "Analyzing codebase...\nFound 3 files to modify.\nApplying changes...",
			want:    false,
		},
		{
			name:    "case insensitive trust",
			content: "DO YOU TRUST THE FILES IN THIS FOLDER?\n(Y/N)",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := looksLikeTrustDialog(tt.content)
			assert.Equal(t, tt.want, got, "looksLikeTrustDialog(%q)", tt.content)
		})
	}
}

func TestLooksLikeInputPrompt_NotTrustDialog(t *testing.T) {
	// Trust dialogs should NOT be classified as input prompts
	trustContent := "Do you trust the files in this folder?\n> "
	assert.True(t, looksLikeTrustDialog(trustContent), "should be detected as trust dialog")
	assert.False(t, looksLikeInputPrompt(trustContent), "trust dialog should not match as input prompt")

	// Normal input prompt should still work
	normalPrompt := "Some output\n> "
	assert.False(t, looksLikeTrustDialog(normalPrompt))
	assert.True(t, looksLikeInputPrompt(normalPrompt))
}

// trustMockTmux extends mockTmux with configurable pane content and raw key tracking.
type trustMockTmux struct {
	mockTmux
	paneContent    map[string]string // target -> content
	paneDead       map[string]bool   // target -> dead
	rawKeysSent    []string          // track SendKeysRaw calls
}

func newTrustMockTmux() *trustMockTmux {
	return &trustMockTmux{
		mockTmux: mockTmux{
			sessions:     make(map[string]map[string]bool),
			keys:         make(map[string]string),
			remainOnExit: make(map[string]bool),
			envVars:      make(map[string]string),
		},
		paneContent: make(map[string]string),
		paneDead:    make(map[string]bool),
	}
}

func (m *trustMockTmux) CapturePaneContent(session, windowName string, lines int) (string, error) {
	target := session + ":" + windowName
	if content, ok := m.paneContent[target]; ok {
		return content, nil
	}
	return "", nil
}

func (m *trustMockTmux) IsPaneDead(session, windowName string) (bool, error) {
	target := session + ":" + windowName
	return m.paneDead[target], nil
}

func (m *trustMockTmux) SendKeysRaw(target, key string) error {
	m.rawKeysSent = append(m.rawKeysSent, target+":"+key)
	return nil
}

func TestCheckAndResolveTrustDialog(t *testing.T) {
	tm := newTrustMockTmux()
	tm.NewSession("test-sess")
	tm.NewWindow("test-sess", "repo-climb1")

	tr := &ProblemRunner{
		tmux:        tm,
		tmuxSession: "test-sess",
	}

	t.Run("no dialog returns false", func(t *testing.T) {
		tm.paneContent["test-sess:repo-climb1"] = "Normal agent output\nWorking on task..."
		resolved, err := tr.checkAndResolveTrustDialog("repo-climb1")
		require.NoError(t, err)
		assert.False(t, resolved)
	})

	t.Run("trust dialog resolved with Enter", func(t *testing.T) {
		tm.rawKeysSent = nil
		tm.paneContent["test-sess:repo-climb1"] = "Do you trust the files in this folder?\n(y/N)"
		resolved, err := tr.checkAndResolveTrustDialog("repo-climb1")
		require.NoError(t, err)
		assert.True(t, resolved)
		require.Len(t, tm.rawKeysSent, 1)
		assert.Equal(t, "test-sess:repo-climb1:Enter", tm.rawKeysSent[0])
	})
}

// setupTrustTestEnv creates a ProblemRunner with a trustMockTmux for trust dialog tests.
func setupTrustTestEnv(t *testing.T, taskID, climbID, repoName string) (*ProblemRunner, *trustMockTmux, string) {
	t.Helper()
	tm := newTrustMockTmux()
	tm.NewSession("test-sess")
	windowName := fmt.Sprintf("%s-%s", repoName, climbID)
	tm.NewWindow("test-sess", windowName)

	db := testutil.SetupTestDB(t)
	st := store.New(db)

	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")
	os.MkdirAll(logDir, 0o755)
	lm := logmgr.New(logDir)
	lm.EnsureDir(taskID)

	// Create worktree dir with .lead/<climbID>/ (no TOP.json)
	worktreeDir := filepath.Join(tmpDir, "worktree")
	os.MkdirAll(filepath.Join(worktreeDir, ".lead", climbID), 0o755)

	// Insert problem and climb in store
	climbsJSON, _ := json.Marshal(model.ClimbsFile{})
	require.NoError(t, st.InsertProblem(&model.Problem{
		ID:         taskID,
		CragID:     "test-crag",
		Spec:       "test spec",
		ClimbsJSON: string(climbsJSON),
		Status:     model.ProblemStatusPending,
	}, []model.Climb{
		{ID: climbID, ProblemID: taskID, RepoName: repoName, Description: "do something", Status: model.ClimbStatusPending},
	}))
	require.NoError(t, st.UpdateProblemStatus(taskID, model.ProblemStatusRunning))
	require.NoError(t, st.UpdateClimbStatus(climbID, model.ClimbStatusRunning))

	tr := &ProblemRunner{
		tmux:        tm,
		tmuxSession: "test-sess",
		store:       st,
		logMgr:      lm,
		task:        &model.Problem{ID: taskID},
		worktrees:   map[string]string{repoName: worktreeDir},
		startedAt:   make(map[string]time.Time),
		dag:         BuildDAG([]model.Climb{{ID: climbID, ProblemID: taskID, RepoName: repoName, Description: "do something", Status: model.ClimbStatusRunning, Attempt: 1}}),
	}

	return tr, tm, lm.LogPath(taskID, climbID)
}

func TestCheckStaleClimbs_TrustDialogResolved(t *testing.T) {
	tr, tm, logPath := setupTrustTestEnv(t, "task1", "c1", "api")

	// Create a log file that's been silent for 3 minutes
	os.WriteFile(logPath, []byte("initial output"), 0o644)
	oldTime := time.Now().Add(-3 * time.Minute)
	os.Chtimes(logPath, oldTime, oldTime)

	// Set pane content to trust dialog
	tm.paneContent["test-sess:api-c1"] = "Do you trust the files in this folder?\n(y/N)"

	// Started 3 minutes ago (past early detection window)
	tr.startedAt["c1"] = time.Now().Add(-3 * time.Minute)

	retries, err := tr.CheckStaleClimbs(30 * time.Minute)
	require.NoError(t, err)

	// Trust dialog should be resolved — climb should NOT be retried
	assert.Empty(t, retries, "climb should not be retried after trust dialog resolution")

	// Verify Enter was sent
	require.Len(t, tm.rawKeysSent, 1)
	assert.Equal(t, "test-sess:api-c1:Enter", tm.rawKeysSent[0])

	// Verify log file mtime was updated (silence reset)
	info, err := os.Stat(logPath)
	require.NoError(t, err)
	assert.True(t, time.Since(info.ModTime()) < 5*time.Second, "log file mtime should be refreshed")
}

func TestCheckStaleClimbs_EarlyTrustDetection(t *testing.T) {
	tr, tm, logPath := setupTrustTestEnv(t, "task2", "c2", "api")

	// Create a log file that's been silent for only 15 seconds (under 2min threshold)
	os.WriteFile(logPath, []byte("starting"), 0o644)
	os.Chtimes(logPath, time.Now().Add(-15*time.Second), time.Now().Add(-15*time.Second))

	// Trust dialog in pane
	tm.paneContent["test-sess:api-c2"] = "Trust this folder?\n(y/N)"

	// Spawn was 20 seconds ago — within the 30s early detection window
	tr.startedAt["c2"] = time.Now().Add(-20 * time.Second)

	retries, err := tr.CheckStaleClimbs(30 * time.Minute)
	require.NoError(t, err)

	// Early detection should have caught and resolved the trust dialog
	assert.Empty(t, retries, "early trust detection should resolve dialog without retry")
	assert.Len(t, tm.rawKeysSent, 1, "Enter should have been sent")
	assert.Equal(t, "test-sess:api-c2:Enter", tm.rawKeysSent[0])
}
