package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	Enter     key.Binding
	Tab       key.Binding
	ShiftTab  key.Binding
	Quit      key.Binding
	Refresh   key.Binding
	Escape    key.Binding
	Pane1     key.Binding
	Pane2     key.Binding
	Pane3     key.Binding
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("k", "up"),
		key.WithHelp("k/up", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("j", "down"),
		key.WithHelp("j/down", "down"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	Tab: key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "next pane"),
	),
	ShiftTab: key.NewBinding(
		key.WithKeys("shift+tab"),
		key.WithHelp("shift+tab", "prev pane"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
	Escape: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Pane1: key.NewBinding(
		key.WithKeys("1"),
		key.WithHelp("1", "tasks"),
	),
	Pane2: key.NewBinding(
		key.WithKeys("2"),
		key.WithHelp("2", "detail"),
	),
	Pane3: key.NewBinding(
		key.WithKeys("3"),
		key.WithHelp("3", "events"),
	),
}
