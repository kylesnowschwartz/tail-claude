package main

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// StyledIcon pairs an icon glyph with its default foreground color.
// Centralizes glyph-color pairings so changes happen in one place.
type StyledIcon struct {
	Glyph string
	Color color.Color
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
func (s StyledIcon) WithColor(c color.Color) string {
	return lipgloss.NewStyle().Foreground(c).Render(s.Glyph)
}

// Icons used throughout the TUI.
// Requires a Nerd Font patched terminal font (e.g. JetBrains Mono Nerd Font).
// Codepoints from Font Awesome (U+F000-U+F2E0) and Material Design (U+F0001+).
//
// NOTE: These are initialized by initIcons() after initTheme() resolves colors.
var (
	IconBranch    StyledIcon
	IconChat      StyledIcon
	IconClaude    StyledIcon
	IconClock     StyledIcon
	IconCollapsed StyledIcon
	IconDot       StyledIcon
	IconDrillDown StyledIcon
	IconEllipsis  StyledIcon
	IconExpanded  StyledIcon
	IconOutput    StyledIcon
	IconSelected  StyledIcon
	IconSession   StyledIcon
	IconSubagent  StyledIcon
	IconSystem    StyledIcon
	IconSystemErr StyledIcon
	IconTeammate  StyledIcon
	IconThinking  StyledIcon
	IconToken     StyledIcon
	IconToolErr   StyledIcon
	IconToolOk    StyledIcon
	IconUser      StyledIcon

	// Per-category tool icons (detail view item rows).
	IconToolRead  StyledIcon
	IconToolEdit  StyledIcon
	IconToolWrite StyledIcon
	IconToolBash  StyledIcon
	IconToolGrep  StyledIcon
	IconToolGlob  StyledIcon
	IconToolTask  StyledIcon
	IconToolSkill StyledIcon
	IconToolWeb   StyledIcon
	IconToolMisc  StyledIcon

	// Picker metadata icons -- carry ColorPickerMeta so call sites
	// use .Render() instead of .WithColor(metaColor) everywhere.
	IconPickerBranch  StyledIcon
	IconPickerChat    StyledIcon
	IconPickerSession StyledIcon

	// Task board status glyphs
	IconTaskDone    StyledIcon
	IconTaskActive  StyledIcon
	IconTaskPending StyledIcon
)

// Plain glyphs -- used as raw strings (never styled via StyledIcon).
const (
	GlyphHRule    = "\u2500" // box drawing horizontal (compact separators)
	GlyphBeadFull = ""       // black circle (activity indicator bead, bright)
)

// SpinnerFrames is a 10-frame braille spinner used for ongoing indicators.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// initIcons builds all StyledIcon values from resolved theme colors.
// Must be called after initTheme().
func initIcons() {
	IconBranch = StyledIcon{"\uE0A0", ColorGitBranch}
	IconChat = StyledIcon{"\uF086", ColorTextDim}
	IconClaude = StyledIcon{"󱙺", ColorInfo}
	IconClock = StyledIcon{"", ColorTextDim}
	IconCollapsed = StyledIcon{"", ColorTextDim}
	IconDot = StyledIcon{"\u00B7", ColorTextMuted}
	IconDrillDown = StyledIcon{"", ColorAccent}
	IconEllipsis = StyledIcon{"\u2026", ColorTextDim}
	IconExpanded = StyledIcon{"", ColorTextPrimary}
	IconOutput = StyledIcon{"󰆂", ColorAccent}
	IconSelected = StyledIcon{"\u2502", ColorAccent}
	IconSession = StyledIcon{"󰈷", ColorTextDim}
	IconSubagent = StyledIcon{"󱙺", ColorAccent}
	IconSystem = StyledIcon{"", ColorTextMuted}
	IconSystemErr = StyledIcon{"", ColorError}
	IconTeammate = StyledIcon{"󱙺", ColorAccent}
	IconThinking = StyledIcon{"", ColorTextDim}
	IconToken = StyledIcon{"", ColorTextDim}
	IconToolErr = StyledIcon{"󰯠", ColorError}
	IconToolOk = StyledIcon{"󰯠", ColorTextDim}
	IconUser = StyledIcon{"", ColorTextSecondary}

	IconToolRead = StyledIcon{"", ColorToolRead}
	IconToolEdit = StyledIcon{"", ColorToolEdit}
	IconToolWrite = StyledIcon{"", ColorToolWrite}
	IconToolBash = StyledIcon{"󰯠", ColorToolBash}
	IconToolGrep = StyledIcon{"󰥨", ColorToolGrep}
	IconToolGlob = StyledIcon{"󰥨", ColorToolGlob}
	IconToolTask = StyledIcon{"󱙺", ColorToolTask}
	IconToolSkill = StyledIcon{"󰯠", ColorToolSkill}
	IconToolWeb = StyledIcon{"󰖟", ColorToolWeb}
	IconToolMisc = StyledIcon{"󰯠", ColorToolOther}

	IconPickerBranch = StyledIcon{IconBranch.Glyph, ColorPickerMeta}
	IconPickerChat = StyledIcon{IconChat.Glyph, ColorPickerMeta}
	IconPickerSession = StyledIcon{IconSession.Glyph, ColorPickerMeta}

	IconTaskDone = StyledIcon{"\u2713", ColorOngoing}
	IconTaskActive = StyledIcon{"\u27F3", ColorAccent}
	IconTaskPending = StyledIcon{"\u25CB", ColorTextMuted}
}
