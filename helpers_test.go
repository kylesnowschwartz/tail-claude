package main

import (
	tea "github.com/charmbracelet/bubbletea"
)

// key constructs a tea.KeyMsg from a string like "j", "tab", "enter", "ctrl+c".
// Single-character strings are mapped to KeyRunes; named keys get their
// corresponding KeyType constant.
func key(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case "esc", "escape":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// mouseScroll constructs a tea.MouseMsg for wheel events.
func mouseScroll(button tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{Button: button}
}

// testModel returns a model with 3 messages (user, claude, system),
// width=120, height=40, and a dark-background markdown renderer.
// lineOffsets are pre-computed so scroll math is immediately usable.
func testModel() model {
	msgs := []message{
		userMsg("Hello, world"),
		claudeMsg(func(m *message) {
			m.model = "opus4.6"
			m.content = "I can help with that."
			m.thinkingCount = 1
			m.toolCallCount = 2
			m.timestamp = "10:00:01 AM"
		}),
		{
			role:      RoleSystem,
			content:   "system output",
			timestamp: "10:00:02 AM",
		},
	}
	m := initialModel(msgs, true) // dark background
	m.width = 120
	m.height = 40
	m.computeLineOffsets()
	return m
}

// claudeMsg builds a message{role: RoleClaude} with sensible defaults.
// Functional options let callers override individual fields.
func claudeMsg(opts ...func(*message)) message {
	m := message{
		role:      RoleClaude,
		model:     "opus4.6",
		content:   "Claude response",
		timestamp: "10:00:00 AM",
	}
	for _, opt := range opts {
		opt(&m)
	}
	return m
}

// userMsg builds a message{role: RoleUser} with the given content.
func userMsg(content string) message {
	return message{
		role:      RoleUser,
		content:   content,
		timestamp: "10:00:00 AM",
	}
}
