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
// | Success             |   "2" |  "76" | green         | green          |
// | Warning             |   "3" | "214" | yellow        | orange         |
// | Error               |   "1" | "196" | red           | red            |
// | Info                |   "4" |  "69" | blue          | blue           |
// | Border              | "250" |  "60" | subtle gray   | muted blue     |
// | LiveBg              |   "2" |  "28" | green         | dark green     |
// | LiveFg              |   "0" | "255" | black         | white          |
// | ModelOpus           |   "1" | "204" | red           | coral          |
// | ModelSonnet         |   "4" |  "75" | blue          | blue           |
// | ModelHaiku          |   "2" | "114" | green         | green          |
// | BadgeBg             | "254" | "237" | light gray    | dark gray      |
// | TokenIcon           |   "3" | "178" | gold          | amber          |
// | TokenHigh           |   "3" | "208" | gold          | orange (>150k) |
// | Ongoing             |   "2" |  "76" | green dot     | green dot      |
// | OngoingDim          | "114" |  "34" | dim green     | dim green      |
// | PickerSelectedBg    | "254" | "237" | subtle elev.  | subtle elev.   |

var (
	// Text hierarchy
	ColorTextPrimary   = ac("0", "252")
	ColorTextSecondary = ac("8", "245")
	ColorTextDim       = ac("242", "243")
	ColorTextMuted     = ac("245", "240")

	// Accents
	ColorAccent  = ac("4", "75")
	ColorSuccess = ac("2", "76")
	ColorWarning = ac("3", "214")
	ColorError   = ac("1", "196")
	ColorInfo    = ac("4", "69")

	// Surfaces
	ColorBorder = ac("250", "60")

	// Live badge
	ColorLiveBg = ac("2", "28")
	ColorLiveFg = ac("0", "255")

	// Model family (matches claude-devtools)
	ColorModelOpus   = ac("1", "204")
	ColorModelSonnet = ac("4", "75")
	ColorModelHaiku  = ac("2", "114")

	// Badge backgrounds
	ColorBadgeBg = ac("254", "237")

	// Token icon
	ColorTokenIcon = ac("3", "178")
	ColorTokenHigh = ac("3", "208")

	// Ongoing indicator
	ColorOngoing    = ac("2", "76")
	ColorOngoingDim = ac("114", "34")

	// Picker
	ColorPickerSelectedBg = ac("254", "237")
)

// ac is a shorthand constructor for lipgloss.AdaptiveColor.
func ac(light, dark string) lipgloss.AdaptiveColor {
	return lipgloss.AdaptiveColor{Light: light, Dark: dark}
}
