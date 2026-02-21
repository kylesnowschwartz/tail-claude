package main

import "github.com/charmbracelet/lipgloss"

// StyledIcon pairs an icon glyph with its default foreground color.
// Centralizes glyph-color pairings so changes happen in one place.
type StyledIcon struct {
	Glyph string
	Color lipgloss.AdaptiveColor
}

// Render returns the icon styled with its default color.
func (s StyledIcon) Render() string {
	return lipgloss.NewStyle().Foreground(s.Color).Render(s.Glyph)
}

// RenderBold returns the icon styled bold with its default color.
func (s StyledIcon) RenderBold() string {
	return lipgloss.NewStyle().Bold(true).Foreground(s.Color).Render(s.Glyph)
}

// WithColor returns the icon styled with an override color.
func (s StyledIcon) WithColor(c lipgloss.AdaptiveColor) string {
	return lipgloss.NewStyle().Foreground(c).Render(s.Glyph)
}

// Icons used throughout the TUI.
// Requires a Nerd Font patched terminal font (e.g. JetBrains Mono Nerd Font).
// Codepoints from Font Awesome (U+F000-U+F2E0) and Material Design (U+F0001+).
var (
	IconClaude    = StyledIcon{"󱙺", ColorInfo}               // nf-cod-hubot (bot/robot face)
	IconUser      = StyledIcon{"\uF007", ColorTextSecondary} // nf-fa-user
	IconSystem    = StyledIcon{"\uF120", ColorTextMuted}     // nf-fa-terminal
	IconExpanded  = StyledIcon{"\uF078", ColorTextPrimary}   // nf-fa-chevron_down
	IconCollapsed = StyledIcon{"\uF054", ColorTextDim}       // nf-fa-chevron_right
	IconCursor    = StyledIcon{"\uF054", ColorAccent}        // nf-fa-chevron_right
	IconDrillDown = StyledIcon{"\uF061", ColorAccent}        // nf-fa-arrow_right
	IconThinking  = StyledIcon{"", ColorTextDim}            // nf-fa-lightbulb_o
	IconOutput    = StyledIcon{"󰆂", ColorAccent}             // nf-fa-file_code_o
	IconToolOk    = StyledIcon{"󰯠", ColorTextDim}
	IconToolErr   = StyledIcon{"󰯠", ColorError} // nf-fa-times
	IconSubagent  = StyledIcon{"󱙺", ColorAccent}
	IconTeammate  = StyledIcon{"󱙺", ColorAccent}         // nf-fa-comment
	IconSelected  = StyledIcon{"\u2502", ColorAccent}    // box drawing vertical
	IconToken     = StyledIcon{"", ColorTextDim}        // nf-fae-coins
	IconClock     = StyledIcon{"\uF017", ColorTextDim}   // nf-fa-clock_o
	IconDot       = StyledIcon{"\u00B7", ColorTextMuted} // middle dot
	IconChat      = StyledIcon{"\uF086", ColorTextDim}   // nf-fa-comments (turn count badge)
	IconLive      = StyledIcon{"\u25CF", ColorOngoing}   // filled circle (for ongoing indicator)
)

// Plain glyphs -- used as raw strings (never styled via StyledIcon).
const (
	GlyphEllipsis  = "\u2026" // horizontal ellipsis (truncation hints)
	GlyphHRule     = "\u2500" // box drawing horizontal (compact separators)
	GlyphBeadFull  = "\u25CF" // black circle (activity indicator bead, bright)
	GlyphBeadEmpty = "\u00B7" // middle dot (activity indicator bead, dim/unused)
)
