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
	GlyphBeadFull = "\uEABC" // nf-cod-circle_filled (activity indicator bead)
)

// SpinnerFrames is a 10-frame braille spinner used for ongoing indicators.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// initIcons builds all StyledIcon values from resolved theme colors.
// Must be called after initTheme().
//
// All glyphs use explicit Unicode escapes (\uXXXX / \U000XXXXX) to prevent
// silent corruption when LLM tools round-trip the file. Nerd Font codepoints
// in the Private Use Area are particularly vulnerable to being dropped.
func initIcons() {
	IconBranch = StyledIcon{"\uE0A0", ColorGitBranch}     // nf-pl-branch
	IconChat = StyledIcon{"\uF086", ColorTextDim}         // nf-fa-comments
	IconClaude = StyledIcon{"\U000F167A", ColorInfo}      // nf-md-robot_outline
	IconClock = StyledIcon{"\uF017", ColorTextDim}        // nf-fa-clock_o
	IconCollapsed = StyledIcon{"\uF054", ColorTextDim}    // nf-fa-chevron_right
	IconDot = StyledIcon{"\u00B7", ColorTextMuted}        // middle dot
	IconDrillDown = StyledIcon{"\uF061", ColorAccent}     // nf-fa-arrow_right
	IconEllipsis = StyledIcon{"\u2026", ColorTextDim}     // horizontal ellipsis
	IconExpanded = StyledIcon{"\uF078", ColorTextPrimary} // nf-fa-chevron_down
	IconOutput = StyledIcon{"\U000F0182", ColorAccent}    // nf-md-code_tags
	IconSelected = StyledIcon{"\u2502", ColorAccent}      // box drawing vertical
	IconSession = StyledIcon{"\U000F0237", ColorTextDim}  // nf-md-file_document
	IconSubagent = StyledIcon{"\U000F167A", ColorAccent}  // nf-md-robot_outline
	IconSystem = StyledIcon{"\uF120", ColorTextMuted}     // nf-fa-terminal
	IconSystemErr = StyledIcon{"\uF06A", ColorError}      // nf-fa-exclamation_circle
	IconTeammate = StyledIcon{"\U000F167A", ColorAccent}  // nf-md-robot_outline
	IconThinking = StyledIcon{"\uF0EB", ColorTextDim}     // nf-fa-lightbulb_o
	IconToken = StyledIcon{"\uEDE8", ColorTextDim}        // nf-cod-symbol_numeric
	IconToolErr = StyledIcon{"\U000F0BE0", ColorError}    // nf-md-console
	IconToolOk = StyledIcon{"\U000F0BE0", ColorTextDim}   // nf-md-console
	IconUser = StyledIcon{"\uF007", ColorTextSecondary}   // nf-fa-user

	IconToolRead = StyledIcon{"\uE28B", ColorToolRead}       // nf-custom-file
	IconToolEdit = StyledIcon{"\uEE75", ColorToolEdit}       // nf-cod-edit
	IconToolWrite = StyledIcon{"\uEE75", ColorToolWrite}     // nf-cod-new_file
	IconToolBash = StyledIcon{"\U000F0BE0", ColorToolBash}   // nf-md-console
	IconToolGrep = StyledIcon{"\U000F0968", ColorToolGrep}   // nf-md-text_search
	IconToolGlob = StyledIcon{"\U000F0968", ColorToolGlob}   // nf-md-folder_search
	IconToolTask = StyledIcon{"\U000F167A", ColorToolTask}   // nf-md-robot_outline
	IconToolSkill = StyledIcon{"\U000F0BE0", ColorToolSkill} // nf-md-console
	IconToolWeb = StyledIcon{"\U000F059F", ColorToolWeb}     // nf-md-web
	IconToolMisc = StyledIcon{"\U000F0BE0", ColorToolOther}  // nf-md-console

	IconPickerBranch = StyledIcon{IconBranch.Glyph, ColorPickerMeta}
	IconPickerChat = StyledIcon{IconChat.Glyph, ColorPickerMeta}
	IconPickerSession = StyledIcon{IconSession.Glyph, ColorPickerMeta}

	IconTaskDone = StyledIcon{"\u2713", ColorOngoing}      // check mark
	IconTaskActive = StyledIcon{"\u27F3", ColorAccent}     // clockwise arrow
	IconTaskPending = StyledIcon{"\u25CB", ColorTextMuted} // white circle
}
