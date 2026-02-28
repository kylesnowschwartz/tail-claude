package main

import (
	tea "charm.land/bubbletea/v2"
)

// key constructs a tea.KeyPressMsg from a string like "j", "tab", "enter", "ctrl+c".
// Single-character strings produce a key with that Text; named keys get their
// corresponding KeyType / Code.
func key(s string) tea.KeyPressMsg {
	switch s {
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "ctrl+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	case "ctrl+z":
		return tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl}
	case "esc", "escape":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	default:
		runes := []rune(s)
		if len(runes) == 1 {
			return tea.KeyPressMsg{Code: runes[0], Text: s}
		}
		return tea.KeyPressMsg{Text: s}
	}
}

// mouseScroll constructs a tea.MouseWheelMsg for wheel events.
func mouseScroll(button tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg{Button: button}
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
	m.layoutList()
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
