package tui

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/donovan-yohan/belayer/internal/db"
	"github.com/donovan-yohan/belayer/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestModel(t *testing.T) Model {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	database, err := db.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })
	require.NoError(t, database.Migrate())

	store := NewStore(database.Conn())
	insertInstance(t, store, "inst1", "inst1")
	insertTask(t, store, "task1", "inst1", "Task one", model.TaskStatusRunning)
	insertTask(t, store, "task2", "inst1", "Task two", model.TaskStatusComplete)
	insertTaskRepo(t, store, "tr1", "task1", "repo-a")
	insertLead(t, store, "lead1", "tr1", model.LeadStatusRunning)

	m := NewModel(store, "inst1", "inst1")
	m.width = 120
	m.height = 40
	return m
}

func TestModel_Init(t *testing.T) {
	m := setupTestModel(t)
	cmd := m.Init()
	assert.NotNil(t, cmd)
}

func TestModel_DataMsg(t *testing.T) {
	m := setupTestModel(t)
	m.width = 120
	m.height = 40

	// Simulate data load
	tasks := []TaskSummary{{
		Task:      model.Task{ID: "task1", Status: model.TaskStatusRunning, Description: "Test"},
		RepoCount: 1, LeadCount: 1,
	}}
	updated, _ := m.Update(dataMsg{tasks: tasks})
	um := updated.(Model)
	assert.Len(t, um.tasks, 1)
	assert.Equal(t, "task1", um.tasks[0].Task.ID)
}

func TestModel_KeyNavigation(t *testing.T) {
	m := setupTestModel(t)
	m.tasks = []TaskSummary{
		{Task: model.Task{ID: "t1"}},
		{Task: model.Task{ID: "t2"}},
		{Task: model.Task{ID: "t3"}},
	}

	// Down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	um := updated.(Model)
	assert.Equal(t, 1, um.taskCursor)

	// Up
	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	um = updated.(Model)
	assert.Equal(t, 0, um.taskCursor)
}

func TestModel_TabCyclesPanes(t *testing.T) {
	m := setupTestModel(t)
	assert.Equal(t, PaneTaskList, m.activePane)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	um := updated.(Model)
	assert.Equal(t, PaneDetail, um.activePane)

	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyTab})
	um = updated.(Model)
	assert.Equal(t, PaneEvents, um.activePane)

	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyTab})
	um = updated.(Model)
	assert.Equal(t, PaneTaskList, um.activePane)
}

func TestModel_ShiftTabReverses(t *testing.T) {
	m := setupTestModel(t)
	assert.Equal(t, PaneTaskList, m.activePane)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	um := updated.(Model)
	assert.Equal(t, PaneEvents, um.activePane)
}

func TestModel_NumberKeysSelectPane(t *testing.T) {
	m := setupTestModel(t)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	um := updated.(Model)
	assert.Equal(t, PaneDetail, um.activePane)

	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	um = updated.(Model)
	assert.Equal(t, PaneEvents, um.activePane)

	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	um = updated.(Model)
	assert.Equal(t, PaneTaskList, um.activePane)
}

func TestModel_EnterSelectsTask(t *testing.T) {
	m := setupTestModel(t)
	m.tasks = []TaskSummary{{Task: model.Task{ID: "t1"}}}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	um := updated.(Model)
	assert.Equal(t, PaneDetail, um.activePane)
}

func TestModel_EscapeGoesBack(t *testing.T) {
	m := setupTestModel(t)
	m.activePane = PaneDetail

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	um := updated.(Model)
	assert.Equal(t, PaneTaskList, um.activePane)
}

func TestModel_QuitSetsFlag(t *testing.T) {
	m := setupTestModel(t)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	um := updated.(Model)
	assert.True(t, um.quitting)
	assert.NotNil(t, cmd) // tea.Quit cmd
}

func TestModel_WindowSizeMsg(t *testing.T) {
	m := setupTestModel(t)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	um := updated.(Model)
	assert.Equal(t, 200, um.width)
	assert.Equal(t, 50, um.height)
}

func TestModel_View(t *testing.T) {
	m := setupTestModel(t)
	m.tasks = []TaskSummary{{
		Task:      model.Task{ID: "task1", Status: model.TaskStatusRunning, Description: "Test task"},
		RepoCount: 1, LeadCount: 1,
	}}

	view := m.View()
	assert.Contains(t, view, "belayer")
	assert.Contains(t, view, "inst1")
	assert.Contains(t, view, "Tasks")
	assert.Contains(t, view, "Detail")
	assert.Contains(t, view, "Events")
}

func TestModel_ViewLoading(t *testing.T) {
	m := setupTestModel(t)
	m.width = 0
	m.height = 0

	view := m.View()
	assert.Equal(t, "Loading...", view)
}

func TestModel_CursorClamping(t *testing.T) {
	m := setupTestModel(t)
	m.taskCursor = 5

	updated, _ := m.Update(dataMsg{
		tasks: []TaskSummary{{Task: model.Task{ID: "t1"}}},
	})
	um := updated.(Model)
	assert.Equal(t, 0, um.taskCursor)
}

func TestModel_DetailPaneNavigation(t *testing.T) {
	m := setupTestModel(t)
	m.activePane = PaneDetail
	m.leads = []LeadDetail{
		{Lead: model.Lead{ID: "l1"}, RepoName: "repo-a"},
		{Lead: model.Lead{ID: "l2"}, RepoName: "repo-b"},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	um := updated.(Model)
	assert.Equal(t, 1, um.leadCursor)

	updated, _ = um.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	um = updated.(Model)
	assert.Equal(t, 0, um.leadCursor)
}

func TestModel_EventPaneNavigation(t *testing.T) {
	m := setupTestModel(t)
	m.activePane = PaneEvents
	m.events = []EventEntry{
		{Event: model.Event{ID: "e1"}},
		{Event: model.Event{ID: "e2"}},
	}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	um := updated.(Model)
	assert.Equal(t, 1, um.eventScroll)
}
