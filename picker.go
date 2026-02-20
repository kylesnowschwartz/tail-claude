package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/kylesnowschwartz/tail-claude/parser"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// pickerSessionsMsg delivers discovered sessions to the model.
type pickerSessionsMsg struct {
	sessions []parser.SessionInfo
	err      error
}

// loadSessionMsg delivers a parsed session ready for the list view,
// including classified messages and offset for watcher handoff.
type loadSessionMsg struct {
	messages   []message
	path       string
	classified []parser.ClassifiedMsg
	offset     int64
	err        error
}

// loadPickerSessionsCmd discovers sessions for the current project.
func loadPickerSessionsCmd() tea.Msg {
	projectDir, err := parser.CurrentProjectDir()
	if err != nil {
		return pickerSessionsMsg{err: err}
	}
	sessions, err := parser.DiscoverProjectSessions(projectDir)
	return pickerSessionsMsg{sessions: sessions, err: err}
}

// loadSessionCmd returns a command that loads a session file into messages.
// Uses ReadSessionIncremental so the result includes classified messages and
// offset for handing off to a new watcher.
func loadSessionCmd(path string) tea.Cmd {
	return func() tea.Msg {
		classified, offset, err := parser.ReadSessionIncremental(path, 0)
		if err != nil {
			return loadSessionMsg{err: err, path: path}
		}
		chunks := parser.BuildChunks(classified)
		return loadSessionMsg{
			messages:   chunksToMessages(chunks),
			path:       path,
			classified: classified,
			offset:     offset,
		}
	}
}

// updatePicker handles key events in the session picker view.
func (m model) updatePicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "escape":
		m.view = viewList
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.pickerCursor < len(m.pickerSessions)-1 {
			m.pickerCursor++
		}
		m.ensurePickerVisible()
	case "k", "up":
		if m.pickerCursor > 0 {
			m.pickerCursor--
		}
		m.ensurePickerVisible()
	case "G":
		if len(m.pickerSessions) > 0 {
			m.pickerCursor = len(m.pickerSessions) - 1
			m.ensurePickerVisible()
		}
	case "g":
		m.pickerCursor = 0
		m.pickerScroll = 0
	case "enter":
		if m.pickerCursor < len(m.pickerSessions) {
			return m, loadSessionCmd(m.pickerSessions[m.pickerCursor].Path)
		}
	}
	return m, nil
}

// ensurePickerVisible adjusts pickerScroll so the cursor is visible.
func (m *model) ensurePickerVisible() {
	viewHeight := m.height - 4 // header (2 lines) + status bar (2 lines)
	if viewHeight <= 0 {
		return
	}
	if m.pickerCursor < m.pickerScroll {
		m.pickerScroll = m.pickerCursor
	}
	if m.pickerCursor >= m.pickerScroll+viewHeight {
		m.pickerScroll = m.pickerCursor - viewHeight + 1
	}
}

// viewPicker renders the session picker screen.
func (m model) viewPicker() string {
	width := m.width
	if width > 120 {
		width = 120
	}

	// Header
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
	countStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	header := titleStyle.Render("Sessions") + " " +
		countStyle.Render(fmt.Sprintf("(%d)", len(m.pickerSessions)))
	header += "\n"

	// Error / empty states
	if len(m.pickerSessions) == 0 {
		dim := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
		return header + "\n" + dim.Render("No sessions found for this project.")
	}

	// Session list
	viewHeight := m.height - 4
	if viewHeight <= 0 {
		viewHeight = 1
	}

	var rows []string
	endIdx := m.pickerScroll + viewHeight
	if endIdx > len(m.pickerSessions) {
		endIdx = len(m.pickerSessions)
	}

	for i := m.pickerScroll; i < endIdx; i++ {
		s := m.pickerSessions[i]
		isSelected := i == m.pickerCursor

		sel := selectionIndicator(isSelected)

		// Left side: preview text
		preview := s.FirstMessage
		if preview == "" {
			preview = "(no user messages)"
		}

		// Right side: message count + relative time
		countStr := fmt.Sprintf("%d msgs", s.MessageCount)
		timeStr := relativeTime(s.ModTime)
		right := lipgloss.NewStyle().Foreground(lipgloss.Color("243")).
			Render(countStr + "  " + timeStr)

		rightWidth := lipgloss.Width(right)
		selWidth := lipgloss.Width(sel)
		previewMaxWidth := width - rightWidth - selWidth - 4
		if previewMaxWidth < 20 {
			previewMaxWidth = 20
		}

		if lipgloss.Width(preview) > previewMaxWidth {
			preview = truncate(preview, previewMaxWidth)
		}

		previewStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
		if !isSelected {
			previewStyle = previewStyle.Foreground(lipgloss.Color("245"))
		}

		left := sel + previewStyle.Render(preview)
		gap := width - lipgloss.Width(left) - rightWidth
		if gap < 2 {
			gap = 2
		}

		rows = append(rows, left+strings.Repeat(" ", gap)+right)
	}

	content := header + "\n" + strings.Join(rows, "\n")

	// Pad to fill viewport so status bar stays at bottom
	renderedLines := strings.Count(content, "\n") + 1
	if renderedLines < m.height-2 {
		content += strings.Repeat("\n", m.height-2-renderedLines)
	}

	status := m.renderStatusBar(
		"j/k", "nav",
		"G/g", "jump",
		"enter", "open",
		"q/esc", "back",
	)

	return content + "\n" + status
}

// relativeTime formats a time.Time as a human-readable relative duration.
func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}
