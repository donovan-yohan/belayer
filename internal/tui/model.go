package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Pane identifies which pane has focus.
type Pane int

const (
	PaneTaskList Pane = iota
	PaneDetail
	PaneEvents
	paneCount = 3
)

// Model is the main bubbletea model for the TUI dashboard.
type Model struct {
	store        *Store
	instanceID   string
	instanceName string

	// Data
	tasks  []TaskSummary
	leads  []LeadDetail
	events []EventEntry

	// Cursors
	taskCursor  int
	leadCursor  int
	eventScroll int

	// Focus
	activePane Pane

	// Window
	width  int
	height int

	// State
	err     error
	quitting bool
}

// NewModel creates a new TUI model.
func NewModel(store *Store, instanceID, instanceName string) Model {
	return Model{
		store:        store,
		instanceID:   instanceID,
		instanceName: instanceName,
		activePane:   PaneTaskList,
	}
}

type tickMsg time.Time
type dataMsg struct {
	tasks  []TaskSummary
	leads  []LeadDetail
	events []EventEntry
	err    error
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) loadData() tea.Msg {
	tasks, err := m.store.ListTaskSummaries(m.instanceID)
	if err != nil {
		return dataMsg{err: err}
	}

	msg := dataMsg{tasks: tasks}

	// Load details for selected task
	if len(tasks) > 0 && m.taskCursor < len(tasks) {
		taskID := tasks[m.taskCursor].Task.ID

		leads, err := m.store.GetLeadDetails(taskID)
		if err != nil {
			return dataMsg{err: err}
		}
		msg.leads = leads

		events, err := m.store.GetRecentEvents(taskID, 50)
		if err != nil {
			return dataMsg{err: err}
		}
		msg.events = events
	}

	return msg
}

// Init initializes the model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadData, tickCmd())
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.loadData, tickCmd())

	case dataMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.tasks = msg.tasks
		m.leads = msg.leads
		m.events = msg.events
		m.err = nil

		// Clamp cursors
		if m.taskCursor >= len(m.tasks) && len(m.tasks) > 0 {
			m.taskCursor = len(m.tasks) - 1
		}
		if m.leadCursor >= len(m.leads) && len(m.leads) > 0 {
			m.leadCursor = len(m.leads) - 1
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, keys.Tab):
		m.activePane = (m.activePane + 1) % paneCount
		return m, nil

	case key.Matches(msg, keys.ShiftTab):
		m.activePane = (m.activePane - 1 + paneCount) % paneCount
		return m, nil

	case key.Matches(msg, keys.Pane1):
		m.activePane = PaneTaskList
		return m, nil
	case key.Matches(msg, keys.Pane2):
		m.activePane = PaneDetail
		return m, nil
	case key.Matches(msg, keys.Pane3):
		m.activePane = PaneEvents
		return m, nil

	case key.Matches(msg, keys.Down):
		m.moveDown()
		if m.activePane == PaneTaskList {
			return m, m.loadData
		}
		return m, nil

	case key.Matches(msg, keys.Up):
		m.moveUp()
		if m.activePane == PaneTaskList {
			return m, m.loadData
		}
		return m, nil

	case key.Matches(msg, keys.Enter):
		if m.activePane == PaneTaskList {
			m.activePane = PaneDetail
			m.leadCursor = 0
		}
		return m, nil

	case key.Matches(msg, keys.Escape):
		if m.activePane != PaneTaskList {
			m.activePane = PaneTaskList
		}
		return m, nil

	case key.Matches(msg, keys.Refresh):
		return m, m.loadData
	}

	return m, nil
}

func (m *Model) moveDown() {
	switch m.activePane {
	case PaneTaskList:
		if m.taskCursor < len(m.tasks)-1 {
			m.taskCursor++
			m.leadCursor = 0
			m.eventScroll = 0
		}
	case PaneDetail:
		if m.leadCursor < len(m.leads)-1 {
			m.leadCursor++
		}
	case PaneEvents:
		if m.eventScroll < len(m.events)-1 {
			m.eventScroll++
		}
	}
}

func (m *Model) moveUp() {
	switch m.activePane {
	case PaneTaskList:
		if m.taskCursor > 0 {
			m.taskCursor--
			m.leadCursor = 0
			m.eventScroll = 0
		}
	case PaneDetail:
		if m.leadCursor > 0 {
			m.leadCursor--
		}
	case PaneEvents:
		if m.eventScroll > 0 {
			m.eventScroll--
		}
	}
}

// View renders the TUI.
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	// Layout calculations
	headerHeight := 1
	statusBarHeight := 1
	bodyHeight := m.height - headerHeight - statusBarHeight - 2

	topHeight := bodyHeight * 7 / 10
	bottomHeight := bodyHeight - topHeight

	leftWidth := m.width * 3 / 10
	if leftWidth < 20 {
		leftWidth = 20
	}
	rightWidth := m.width - leftWidth

	// Render components
	header := renderHeader(m.instanceName, m.width)

	var selectedTask *TaskSummary
	if len(m.tasks) > 0 && m.taskCursor < len(m.tasks) {
		selectedTask = &m.tasks[m.taskCursor]
	}

	taskList := renderTaskList(m.tasks, m.taskCursor, m.activePane == PaneTaskList, leftWidth, topHeight)
	detail := renderTaskDetail(selectedTask, m.leads, m.leadCursor, m.activePane == PaneDetail, rightWidth, topHeight)
	eventLog := renderEventLog(m.events, m.eventScroll, m.activePane == PaneEvents, m.width, bottomHeight)

	activeCount := 0
	for _, t := range m.tasks {
		if t.Task.Status == "running" || t.Task.Status == "decomposing" || t.Task.Status == "aligning" {
			activeCount++
		}
	}
	statusBar := renderStatusBar(len(m.tasks), activeCount, m.width)

	// Error display
	errLine := ""
	if m.err != nil {
		errLine = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(m.err.Error()) + "\n"
	}

	// Compose layout
	topRow := lipgloss.JoinHorizontal(lipgloss.Top, taskList, detail)

	return header + "\n" + errLine + topRow + "\n" + eventLog + "\n" + statusBar
}
