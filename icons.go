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
	IconChat      = StyledIcon{"\uF086", ColorTextDim}
	IconClaude    = StyledIcon{"󱙺", ColorInfo}
	IconClock     = StyledIcon{"", ColorTextDim}
	IconCollapsed = StyledIcon{"", ColorTextDim}
	IconDot       = StyledIcon{"\u00B7", ColorTextMuted}
	IconDrillDown = StyledIcon{"", ColorAccent}
	IconEllipsis  = StyledIcon{"\u2026", ColorTextDim}
	IconExpanded  = StyledIcon{"", ColorTextPrimary}
	IconOutput    = StyledIcon{"󰆂", ColorAccent}
	IconSelected  = StyledIcon{"\u2502", ColorAccent}
	IconSession   = StyledIcon{"󰈷", ColorTextDim}
	IconSubagent  = StyledIcon{"󱙺", ColorAccent}
	IconSystem    = StyledIcon{"", ColorTextMuted}
	IconSystemErr = StyledIcon{"", ColorError}
	IconTeammate  = StyledIcon{"󱙺", ColorAccent}
	IconThinking  = StyledIcon{"", ColorTextDim}
	IconToken     = StyledIcon{"", ColorTextDim}
	IconToolErr   = StyledIcon{"󰯠", ColorError}
	IconToolOk    = StyledIcon{"󰯠", ColorTextDim}
	IconUser      = StyledIcon{"", ColorTextSecondary}
)

// Plain glyphs -- used as raw strings (never styled via StyledIcon).
const (
	GlyphHRule    = "\u2500" // box drawing horizontal (compact separators)
	GlyphBeadFull = ""      // black circle (activity indicator bead, bright)
)

// SpinnerFrames is a 10-frame braille spinner used for ongoing indicators.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
