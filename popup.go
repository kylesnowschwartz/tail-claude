package main

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// popup represents a modal confirmation dialog overlaid on the current view.
// When active, it captures all key input until the user confirms or cancels.
//
// Usage:
//
//	m.popup = newPopup("Delete session?", "This cannot be undone.", func() (tea.Model, tea.Cmd) {
//	    // confirm action
//	})
//
// Clear with m.popup = nil when done.
type popup struct {
	title     string
	message   string
	onConfirm func() (tea.Model, tea.Cmd)
}

// newPopup creates a confirmation popup. The onConfirm callback is called
// when the user presses enter or y.
func newPopup(title, message string, onConfirm func() (tea.Model, tea.Cmd)) *popup {
	return &popup{
		title:     title,
		message:   message,
		onConfirm: onConfirm,
	}
}

// updatePopup handles key events when a popup is active.
func (m model) updatePopup(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "y":
		result, cmd := m.popup.onConfirm()
		rm := result.(model)
		rm.popup = nil
		return rm, cmd
	default:
		// Any other key cancels.
		m.popup = nil
		return m, nil
	}
}

// renderPopup overlays a centered popup box on top of the background content.
// Background ANSI sequences are preserved on both sides of the overlay.
func (m model) renderPopup(background string) string {
	p := m.popup

	titleStr := StyleAccentBold.Render(p.title)
	msgStr := StyleSecondary.Render(p.message)
	hintStr := StyleAccentBold.Render("y") + StyleMuted.Render("/") +
		StyleAccentBold.Render("enter") + StyleMuted.Render(" to confirm")

	body := titleStr + "\n\n" + msgStr + "\n\n" + hintStr

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder).
		Padding(1, 3)

	box := boxStyle.Render(body)

	return overlay(background, box, m.width)
}

// overlay splices the fg onto the bg, centered within the given width.
// ANSI escape sequences in the background are truncated cleanly on
// both sides of each foreground line.
//
// Based on: https://gist.github.com/ras0q/9bf5d81544b22302393f61206892e2cd
func overlay(bg, fg string, width int) string {
	bgLines := strings.Split(bg, "\n")
	overlayLines := strings.Split(fg, "\n")

	row := max((len(bgLines)-len(overlayLines))/2, 0)
	col := max((width-ansi.StringWidth(overlayLines[0]))/2, 0)

	for i, overlayLine := range overlayLines {
		targetRow := row + i
		if targetRow >= len(bgLines) {
			break
		}

		bgLine := bgLines[targetRow]

		// Pad background if it's shorter than the insertion column.
		if bgWidth := ansi.StringWidth(bgLine); bgWidth < col {
			bgLine += strings.Repeat(" ", col-bgWidth)
		}

		// ANSI-safe splice: left of overlay | overlay | right of overlay.
		bgLeft := ansi.TruncateWc(bgLine, col, "")
		overlayWidth := ansi.StringWidth(overlayLine)
		insertEnd := col + overlayWidth
		var bgRight string
		if insertEnd < ansi.StringWidth(bgLine) {
			bgRight = ansi.TruncateLeftWc(bgLine, insertEnd, "")
		}

		bgLines[targetRow] = bgLeft + overlayLine + bgRight
	}

	return strings.Join(bgLines, "\n")
}
