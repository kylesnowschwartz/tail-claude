package main

// Icons used throughout the TUI.
// Requires a Nerd Font patched terminal font (e.g. JetBrains Mono Nerd Font).
// Codepoints from Font Awesome (U+F000-U+F2E0) and Material Design (U+F0001+).
const (
	IconClaude    = "\uEB08" // nf-cod-hubot (bot/robot face)
	IconUser      = "\uF007" // nf-fa-user
	IconSystem    = "\uF120" // nf-fa-terminal
	IconExpanded  = "\uF078" // nf-fa-chevron_down
	IconCollapsed = "\uF054" // nf-fa-chevron_right
	IconCursor    = "\uF054" // nf-fa-chevron_right
	IconThinking  = "\uF0EB" // nf-fa-lightbulb_o
	IconOutput    = "\uF1C9" // nf-fa-file_code_o
	IconToolOk    = "\uF00C" // nf-fa-check
	IconToolErr   = "\uF00D" // nf-fa-times
	IconSubagent  = "\uF0C0" // nf-fa-users
	IconTeammate  = "\uF075" // nf-fa-comment
	IconEllipsis  = "\u2026" // horizontal ellipsis (truncation hints)
	IconHRule     = "\u2500" // box drawing horizontal (compact separators)
	IconDot       = "\u00B7" // middle dot
	IconSelected  = "\u2502" // box drawing vertical
	IconClock     = "\uF017" // nf-fa-clock_o
	IconToken     = "\uE26B" // nf-fae-coins
	IconBeadFull  = "\u25CF" // black circle (activity indicator bead, bright)
	IconBeadEmpty = "\u00B7" // middle dot (activity indicator bead, dim)
)
