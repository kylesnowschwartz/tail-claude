package main

import (
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"golang.org/x/term"
)

// mdRenderer caches a glamour terminal renderer at a specific width.
// Recreates the renderer when the width changes.
//
// hasDarkBg is detected once at construction time (before Bubble Tea takes over
// the terminal) because termenv.HasDarkBackground() queries the terminal via
// OSC 11, which can fail or default to "dark" once alt-screen is active.
type mdRenderer struct {
	renderer  *glamour.TermRenderer
	width     int
	hasDarkBg bool
}

// newMdRenderer creates an mdRenderer with the pre-detected background color.
// The caller detects once in main() and passes the result here — keeps the
// detection at a single point rather than scattered across packages.
func newMdRenderer(hasDarkBg bool) *mdRenderer {
	return &mdRenderer{
		hasDarkBg: hasDarkBg,
	}
}

// glamourStyle returns the glamour style config matching the pre-detected
// terminal background. Two overrides from the stock configs:
//
//  1. Document.Margin zeroed — lipgloss containers handle padding.
//  2. Document.Color nilled — body text inherits the terminal's default
//     foreground instead of a hardcoded 256-color value. This is the critical
//     fix: termenv.HasDarkBackground() can return the wrong answer (terminals
//     that don't respond to OSC 11 default to "dark"), and DarkStyleConfig
//     sets Document.Color to "252" (light gray) which is invisible on light
//     backgrounds. Niling it means body text is always readable. Accent colors
//     (headings, links, code) still use the stock config's specific values.
func (r *mdRenderer) glamourStyle() ansi.StyleConfig {
	var style ansi.StyleConfig
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		style = styles.NoTTYStyleConfig
	} else if r.hasDarkBg {
		style = styles.DarkStyleConfig
	} else {
		style = styles.LightStyleConfig
	}
	style.Document.Color = nil
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
			glamour.WithStyles(r.glamourStyle()),
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
	return strings.Trim(out, "\n")
}
