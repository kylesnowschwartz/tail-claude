package main

import (
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"golang.org/x/term"
)

// mdRenderer caches glamour terminal renderers keyed by wrap width. A render
// pass mixes several widths (Claude cards, user bubbles, detail insets), so a
// single-width cache would rebuild the goldmark pipeline on nearly every
// message; the map keeps one renderer per width instead. Only a handful of
// widths exist per terminal size, and reset() on resize keeps the map bounded.
//
// hasDarkBg is detected once at construction time (before Bubble Tea takes over
// the terminal) because termenv.HasDarkBackground() queries the terminal via
// OSC 11, which can fail or default to "dark" once alt-screen is active.
type mdRenderer struct {
	renderers map[int]*glamour.TermRenderer
	hasDarkBg bool
}

// newMdRenderer creates an mdRenderer with the pre-detected background color.
// The caller detects once in main() and passes the result here — keeps the
// detection at a single point rather than scattered across packages.
func newMdRenderer(hasDarkBg bool) *mdRenderer {
	return &mdRenderer{
		renderers: make(map[int]*glamour.TermRenderer),
		hasDarkBg: hasDarkBg,
	}
}

// reset drops all cached renderers. Called on terminal resize so widths from
// the old terminal size don't accumulate.
func (r *mdRenderer) reset() {
	clear(r.renderers)
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
// Returns the original content on error. Reuses the cached renderer for the
// given width, constructing one on first use.
func (r *mdRenderer) renderMarkdown(content string, width int) string {
	if width <= 0 {
		return content
	}
	renderer, ok := r.renderers[width]
	if !ok {
		var err error
		renderer, err = glamour.NewTermRenderer(
			glamour.WithStyles(r.glamourStyle()),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return content
		}
		r.renderers[width] = renderer
	}
	out, err := renderer.Render(content)
	if err != nil {
		return content
	}
	return strings.Trim(out, "\n")
}
