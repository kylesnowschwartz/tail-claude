package main

import "github.com/charmbracelet/lipgloss"

// -- Colors ---------------------------------------------------------------
// All colors use AdaptiveColor for dark/light terminal support.
// Light values: ANSI 0-15 for accents (palette-adaptive), 256-color for grays
// (predictable). ANSI 7/15 (white) are invisible on light backgrounds — never
// use them for Light values.
// Dark values: ANSI 256-color codes tuned for dark backgrounds.
//
// | Name                | Light | Dark  | Light desc    | Dark desc      |
// |---------------------|-------|-------|---------------|----------------|
// | TextPrimary         |   "0" | "252" | black         | light gray     |
// | TextSecondary       |   "8" | "245" | ANSI dk gray  | gray           |
// | TextDim             | "242" | "243" | medium gray   | gray           |
// | TextMuted           | "245" | "240" | med-lt gray   | dark gray      |
// | Accent              |   "4" |  "75" | blue          | blue           |
// | Error               |   "1" | "196" | red           | red            |
// | Info                |   "4" |  "69" | blue          | blue           |
// | Border              | "250" |  "60" | subtle gray   | muted blue     |
// | ModelOpus           |   "1" | "204" | red           | coral          |
// | ModelSonnet         |   "4" |  "75" | blue          | blue           |
// | ModelHaiku          |   "2" | "114" | green         | green          |
// | TokenHigh           |   "3" | "208" | gold          | orange (>150k) |
// | Ongoing             |   "2" |  "76" | green dot     | green dot      |
// | PickerSelectedBg    | "254" | "237" | subtle elev.  | subtle elev.   |
// | PillBypass          |   "1" | "196" | red           | red            |
// | PillAcceptEdits     |   "5" | "135" | magenta       | purple         |
// | PillPlan            |   "2" | "114" | green         | green          |

var (
	// Text hierarchy
	ColorTextPrimary   = ac("0", "252")
	ColorTextSecondary = ac("8", "245")
	ColorTextDim       = ac("242", "243")
	ColorTextMuted     = ac("245", "240")

	// Accents
	ColorAccent = ac("4", "75")
	ColorError  = ac("1", "196")
	ColorInfo   = ac("4", "69")

	// Surfaces
	ColorBorder = ac("250", "60")

	// Model family (matches claude-devtools)
	ColorModelOpus   = ac("1", "204")
	ColorModelSonnet = ac("4", "75")
	ColorModelHaiku  = ac("2", "114")

	// Token highlight
	ColorTokenHigh = ac("3", "208")

	// Ongoing indicator
	ColorOngoing = ac("2", "76")

	// Context usage thresholds
	ColorContextOk   = ac("2", "114")  // green: <50%
	ColorContextWarn = ac("3", "208")  // yellow/orange: 50-80%
	ColorContextCrit = ac("1", "196")  // red: >80%

	// Permission mode pill backgrounds
	ColorPillBypass      = ac("1", "196")  // red: bypassPermissions
	ColorPillAcceptEdits = ac("5", "135")  // purple: acceptEdits
	ColorPillPlan        = ac("2", "114")  // green: plan

	// Picker
	ColorPickerSelectedBg = ac("254", "237")
)

// -- Semantic text styles -----------------------------------------------------
// Reusable styles for the four text hierarchy levels + common bold/accent
// combos. Safe to chain (.Width(), .Padding(), etc.) since lipgloss styles
// are immutable value types -- each method returns a copy.

var (
	StylePrimaryBold   = lipgloss.NewStyle().Bold(true).Foreground(ColorTextPrimary)
	StyleSecondary     = lipgloss.NewStyle().Foreground(ColorTextSecondary)
	StyleSecondaryBold = lipgloss.NewStyle().Bold(true).Foreground(ColorTextSecondary)
	StyleDim           = lipgloss.NewStyle().Foreground(ColorTextDim)
	StyleMuted         = lipgloss.NewStyle().Foreground(ColorTextMuted)
	StyleAccentBold    = lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	StyleErrorBold     = lipgloss.NewStyle().Bold(true).Foreground(ColorError)
)

// ac is a shorthand constructor for lipgloss.AdaptiveColor.
func ac(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}
