package main

import "github.com/charmbracelet/lipgloss"

// -- Colors ---------------------------------------------------------------
// All colors use AdaptiveColor for dark/light terminal support.
// Light values use ANSI base 16 (0-15) which adapt to the terminal's palette.
// Dark values use ANSI 256-color codes tuned for dark backgrounds.

// Text hierarchy
var (
	ColorTextPrimary  = lipgloss.AdaptiveColor{Light: "0", Dark: "252"}
	ColorTextSecondary = lipgloss.AdaptiveColor{Light: "8", Dark: "245"}
	ColorTextDim      = lipgloss.AdaptiveColor{Light: "8", Dark: "243"}
	ColorTextMuted    = lipgloss.AdaptiveColor{Light: "7", Dark: "240"}
	ColorTextKeyHint  = lipgloss.AdaptiveColor{Light: "0", Dark: "250"}
)

// Accents
var (
	ColorAccent  = lipgloss.AdaptiveColor{Light: "4", Dark: "75"}
	ColorSuccess = lipgloss.AdaptiveColor{Light: "2", Dark: "76"}
	ColorError   = lipgloss.AdaptiveColor{Light: "1", Dark: "196"}
	ColorInfo    = lipgloss.AdaptiveColor{Light: "4", Dark: "69"}
)

// Phase badge
var (
	ColorPhaseFg = lipgloss.AdaptiveColor{Light: "5", Dark: "212"}
	ColorPhaseBg = lipgloss.AdaptiveColor{Light: "13", Dark: "53"}
)

// Surfaces
var (
	ColorBorder      = lipgloss.AdaptiveColor{Light: "7", Dark: "60"}
	ColorStatusBarBg = lipgloss.AdaptiveColor{Light: "7", Dark: "236"}
	ColorBubbleBg    = lipgloss.AdaptiveColor{Light: "15", Dark: "237"}
)

// Live badge
var (
	ColorLiveBg = lipgloss.AdaptiveColor{Light: "2", Dark: "28"}
	ColorLiveFg = lipgloss.AdaptiveColor{Light: "15", Dark: "255"}
)
