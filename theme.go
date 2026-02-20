package main

import "github.com/charmbracelet/lipgloss"

// -- Colors ---------------------------------------------------------------
// All colors use AdaptiveColor for dark/light terminal support.
// Light values: ANSI 0-15 for accents (palette-adaptive), 256-color for grays
// (predictable). ANSI 7/15 (white) are invisible on light backgrounds — never
// use them for Light values.
// Dark values: ANSI 256-color codes tuned for dark backgrounds.

// Text hierarchy
var (
	ColorTextPrimary   = lipgloss.AdaptiveColor{Light: "0", Dark: "252"}   // black / light gray
	ColorTextSecondary = lipgloss.AdaptiveColor{Light: "8", Dark: "245"}   // ANSI dark gray / gray
	ColorTextDim       = lipgloss.AdaptiveColor{Light: "245", Dark: "243"} // medium gray / gray
	ColorTextMuted     = lipgloss.AdaptiveColor{Light: "249", Dark: "240"} // light gray / dark gray
)

// Accents
var (
	ColorAccent  = lipgloss.AdaptiveColor{Light: "4", Dark: "75"}
	ColorSuccess = lipgloss.AdaptiveColor{Light: "2", Dark: "76"}
	ColorError   = lipgloss.AdaptiveColor{Light: "1", Dark: "196"}
	ColorInfo    = lipgloss.AdaptiveColor{Light: "4", Dark: "69"}
)

// Surfaces
var (
	ColorBorder = lipgloss.AdaptiveColor{Light: "250", Dark: "60"} // subtle gray / muted blue
)

// Live badge
var (
	ColorLiveBg = lipgloss.AdaptiveColor{Light: "2", Dark: "28"}
	ColorLiveFg = lipgloss.AdaptiveColor{Light: "15", Dark: "255"}
)

// Model family colors (matches claude-devtools color coding)
var (
	ColorModelOpus   = lipgloss.AdaptiveColor{Light: "1", Dark: "204"} // red/coral
	ColorModelSonnet = lipgloss.AdaptiveColor{Light: "4", Dark: "75"}  // blue
	ColorModelHaiku  = lipgloss.AdaptiveColor{Light: "2", Dark: "114"} // green
)

// Badge backgrounds
var (
	ColorBadgeBg = lipgloss.AdaptiveColor{Light: "254", Dark: "237"} // subtle bg for inline badges
)

// Token icon
var (
	ColorTokenIcon = lipgloss.AdaptiveColor{Light: "3", Dark: "178"} // gold/amber
)
