package tui

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })
	require.NoError(t, database.Migrate())
	return NewStore(database.Conn())
}

func insertInstance(t *testing.T, s *Store, id, name string) {
	t.Helper()
	_, err := s.db.Exec(
		`INSERT INTO instances (id, name, path, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		id, name, "/tmp/"+name, time.Now().UTC(), time.Now().UTC(),
	)
	require.NoError(t, err)
}

func insertTask(t *testing.T, s *Store, id, instanceID, desc string, status model.TaskStatus) {
	t.Helper()
	_, err := s.db.Exec(
		`INSERT INTO tasks (id, instance_id, description, source, source_ref, status, sufficiency_checked, created_at, updated_at)
		VALUES (?, ?, ?, 'text', '', ?, 0, ?, ?)`,
		id, instanceID, desc, string(status), time.Now().UTC(), time.Now().UTC(),
	)
	require.NoError(t, err)
}

func insertTaskRepo(t *testing.T, s *Store, id, taskID, repoName string) {
	t.Helper()
	_, err := s.db.Exec(
		`INSERT INTO task_repos (id, task_id, repo_name, spec, worktree_path, created_at)
		VALUES (?, ?, ?, 'test spec', '/tmp/wt', ?)`,
		id, taskID, repoName, time.Now().UTC(),
	)
	require.NoError(t, err)
}

func insertLead(t *testing.T, s *Store, id, taskRepoID string, status model.LeadStatus) {
	t.Helper()
	_, err := s.db.Exec(
		`INSERT INTO leads (id, task_repo_id, status, attempt, output, created_at, updated_at)
		VALUES (?, ?, ?, 0, '', ?, ?)`,
		id, taskRepoID, string(status), time.Now().UTC(), time.Now().UTC(),
	)
	require.NoError(t, err)
}

func insertEvent(t *testing.T, s *Store, id, taskID, leadID string, eventType model.EventType) {
	t.Helper()
	_, err := s.db.Exec(
		`INSERT INTO events (id, task_id, lead_id, type, payload, created_at)
		VALUES (?, ?, ?, ?, '{}', ?)`,
		id, taskID, leadID, string(eventType), time.Now().UTC(),
	)
	require.NoError(t, err)
}

func insertLeadGoal(t *testing.T, s *Store, id, leadID string, index int, status model.LeadGoalStatus) {
	t.Helper()
	_, err := s.db.Exec(
		`INSERT INTO lead_goals (id, lead_id, goal_index, description, status, attempt, output, verdict_json, created_at, updated_at)
		VALUES (?, ?, ?, 'test goal', ?, 0, '', '', ?, ?)`,
		id, leadID, index, string(status), time.Now().UTC(), time.Now().UTC(),
	)
	require.NoError(t, err)
}

func TestListTaskSummaries(t *testing.T) {
	s := setupTestDB(t)
	insertInstance(t, s, "inst1", "inst1")
	insertTask(t, s, "task1", "inst1", "Add feature X", model.TaskStatusRunning)
	insertTaskRepo(t, s, "tr1", "task1", "repo-a")
	insertTaskRepo(t, s, "tr2", "task1", "repo-b")
	insertLead(t, s, "lead1", "tr1", model.LeadStatusComplete)
	insertLead(t, s, "lead2", "tr2", model.LeadStatusRunning)

	summaries, err := s.ListTaskSummaries("inst1")
	require.NoError(t, err)
	require.Len(t, summaries, 1)

	ts := summaries[0]
	assert.Equal(t, "task1", ts.Task.ID)
	assert.Equal(t, model.TaskStatusRunning, ts.Task.Status)
	assert.Equal(t, 2, ts.RepoCount)
	assert.Equal(t, 2, ts.LeadCount)
	assert.Equal(t, 1, ts.LeadsDone)
	assert.Equal(t, 0, ts.LeadsFailed)
}

func TestListTaskSummaries_Empty(t *testing.T) {
	s := setupTestDB(t)
	insertInstance(t, s, "inst1", "inst1")

	summaries, err := s.ListTaskSummaries("inst1")
	require.NoError(t, err)
	assert.Empty(t, summaries)
}

func TestGetLeadDetails(t *testing.T) {
	s := setupTestDB(t)
	insertInstance(t, s, "inst1", "inst1")
	insertTask(t, s, "task1", "inst1", "Test task", model.TaskStatusRunning)
	insertTaskRepo(t, s, "tr1", "task1", "api")
	insertLead(t, s, "lead1", "tr1", model.LeadStatusRunning)
	insertLeadGoal(t, s, "g1", "lead1", 0, model.LeadGoalComplete)
	insertLeadGoal(t, s, "g2", "lead1", 1, model.LeadGoalRunning)

	details, err := s.GetLeadDetails("task1")
	require.NoError(t, err)
	require.Len(t, details, 1)

	d := details[0]
	assert.Equal(t, "lead1", d.Lead.ID)
	assert.Equal(t, "api", d.RepoName)
	assert.Len(t, d.Goals, 2)
	assert.Equal(t, model.LeadGoalComplete, d.Goals[0].Status)
	assert.Equal(t, model.LeadGoalRunning, d.Goals[1].Status)
}

func TestGetRecentEvents(t *testing.T) {
	s := setupTestDB(t)
	insertInstance(t, s, "inst1", "inst1")
	insertTask(t, s, "task1", "inst1", "Test", model.TaskStatusRunning)
	insertTaskRepo(t, s, "tr1", "task1", "svc")
	insertLead(t, s, "lead1", "tr1", model.LeadStatusRunning)
	insertEvent(t, s, "evt1", "task1", "lead1", model.EventLeadStarted)
	insertEvent(t, s, "evt2", "task1", "lead1", model.EventLeadProgress)

	events, err := s.GetRecentEvents("task1", 10)
	require.NoError(t, err)
	assert.Len(t, events, 2)
	// Most recent first
	assert.Equal(t, "svc", events[0].RepoName)
}

func TestGetRecentEvents_NoLead(t *testing.T) {
	s := setupTestDB(t)
	insertInstance(t, s, "inst1", "inst1")
	insertTask(t, s, "task1", "inst1", "Test", model.TaskStatusPending)
	insertEvent(t, s, "evt1", "task1", "", model.EventTaskCreated)

	events, err := s.GetRecentEvents("task1", 10)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, "", events[0].RepoName)
}

func TestRelativeTime(t *testing.T) {
	tests := []struct {
		name     string
		offset   time.Duration
		expected string
	}{
		{"just now", 5 * time.Second, "just now"},
		{"1 minute", 90 * time.Second, "1m ago"},
		{"5 minutes", 5 * time.Minute, "5m ago"},
		{"1 hour", 90 * time.Minute, "1h ago"},
		{"3 hours", 3 * time.Hour, "3h ago"},
		{"1 day", 36 * time.Hour, "1d ago"},
		{"3 days", 72 * time.Hour, "3d ago"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RelativeTime(time.Now().Add(-tt.offset))
			assert.Equal(t, tt.expected, result)
		})
	}
}
