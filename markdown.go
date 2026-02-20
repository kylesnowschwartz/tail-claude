package main

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

// mdRenderer caches a glamour terminal renderer at a specific width.
// Recreates the renderer when the width changes.
type mdRenderer struct {
	renderer *glamour.TermRenderer
	width    int
}

// renderMarkdown renders markdown content for terminal display.
// Returns the original content on error. Recreates the renderer if width changed.
func (r *mdRenderer) renderMarkdown(content string, width int) string {
	if width <= 0 {
		return content
	}
	if r.renderer == nil || r.width != width {
		renderer, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
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
