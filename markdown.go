package main

import (
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/muesli/termenv"
	"golang.org/x/term"
)

// mdRenderer caches a glamour terminal renderer at a specific width.
// Recreates the renderer when the width changes.
type mdRenderer struct {
	renderer *glamour.TermRenderer
	width    int
}

// autoStyle returns the appropriate glamour style config with Document.Margin
// zeroed out so lipgloss containers handle their own padding.
func autoStyle() ansi.StyleConfig {
	var style ansi.StyleConfig
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		style = styles.NoTTYStyleConfig
	} else if termenv.HasDarkBackground() {
		style = styles.DarkStyleConfig
	} else {
		style = styles.LightStyleConfig
	}
	style.Document.Margin = uintPtr(0)
	return style
}

func uintPtr(v uint) *uint { return &v }

// renderMarkdown renders markdown content for terminal display.
// Returns the original content on error. Recreates the renderer if width changed.
func (r *mdRenderer) renderMarkdown(content string, width int) string {
	if width <= 0 {
		return content
	}
	if r.renderer == nil || r.width != width {
		renderer, err := glamour.NewTermRenderer(
			glamour.WithStyles(autoStyle()),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return content
		}
		r.renderer = renderer
		r.width = width
	}
	out, err := r.renderer.Render(content)
	if err != nil {
		return content
	}
	return strings.TrimRight(out, "\n")
}
