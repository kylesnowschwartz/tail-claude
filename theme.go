package main

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// -- Colors ---------------------------------------------------------------
// All colors resolve at init via initTheme(hasDarkBg). Before Bubble Tea
// enters alt-screen, main() detects the background and calls initTheme once.
// After that, every color is a concrete color.Color — no runtime lookup.
//
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
	ColorTextPrimary   color.Color
	ColorTextSecondary color.Color
	ColorTextDim       color.Color
	ColorTextMuted     color.Color

	// Accents
	ColorAccent color.Color
	ColorError  color.Color
	ColorInfo   color.Color

	// Surfaces
	ColorBorder color.Color

	// Model family (matches claude-devtools)
	ColorModelOpus   color.Color
	ColorModelSonnet color.Color
	ColorModelHaiku  color.Color

	// Token highlight
	ColorTokenHigh color.Color

	// Ongoing indicator
	ColorOngoing color.Color

	// Context usage thresholds
	ColorContextOk   color.Color // green: <50%
	ColorContextWarn color.Color // yellow/orange: 50-80%
	ColorContextCrit color.Color // red: >80%

	// Permission mode pill backgrounds
	ColorPillBypass      color.Color // red: bypassPermissions
	ColorPillAcceptEdits color.Color // purple: acceptEdits
	ColorPillPlan        color.Color // green: plan

	// Picker
	ColorPickerSelectedBg color.Color
	ColorPickerMeta       color.Color // metadata icons in picker rows
	ColorGitBranch        color.Color // purple: acceptEdits

	// Tool category colors (per-category icons in detail view).
	ColorToolRead  color.Color
	ColorToolEdit  color.Color
	ColorToolWrite color.Color
	ColorToolBash  color.Color
	ColorToolGrep  color.Color
	ColorToolGlob  color.Color
	ColorToolTask  color.Color
	ColorToolSkill color.Color
	ColorToolWeb   color.Color
	ColorToolOther color.Color

	// Team member colors (matches claude-devtools teamColors.ts).
	// 8 named colors assignable to team-spawned agents.
	ColorTeamBlue   color.Color
	ColorTeamGreen  color.Color
	ColorTeamRed    color.Color
	ColorTeamYellow color.Color
	ColorTeamPurple color.Color
	ColorTeamCyan   color.Color
	ColorTeamOrange color.Color
	ColorTeamPink   color.Color
)

// -- Semantic text styles -----------------------------------------------------
// Reusable styles for the four text hierarchy levels + common bold/accent
// combos. Safe to chain (.Width(), .Padding(), etc.) since lipgloss styles
// are immutable value types -- each method returns a copy.

var (
	StylePrimaryBold   lipgloss.Style
	StyleSecondary     lipgloss.Style
	StyleSecondaryBold lipgloss.Style
	StyleDim           lipgloss.Style
	StyleMuted         lipgloss.Style
	StyleAccentBold    lipgloss.Style
	StyleErrorBold     lipgloss.Style
)

// initTheme resolves all colors for the detected background and rebuilds
// styles. Called once in main() before Bubble Tea starts.
func initTheme(hasDarkBg bool) {
	ld := lipgloss.LightDark(hasDarkBg)

	// Text hierarchy
	ColorTextPrimary = ld(lipgloss.Color("0"), lipgloss.Color("252"))
	ColorTextSecondary = ld(lipgloss.Color("8"), lipgloss.Color("245"))
	ColorTextDim = ld(lipgloss.Color("242"), lipgloss.Color("243"))
	ColorTextMuted = ld(lipgloss.Color("245"), lipgloss.Color("240"))

	// Accents
	ColorAccent = ld(lipgloss.Color("4"), lipgloss.Color("75"))
	ColorError = ld(lipgloss.Color("1"), lipgloss.Color("196"))
	ColorInfo = ld(lipgloss.Color("4"), lipgloss.Color("69"))

	// Surfaces
	ColorBorder = ld(lipgloss.Color("250"), lipgloss.Color("60"))

	// Model family
	ColorModelOpus = ld(lipgloss.Color("1"), lipgloss.Color("204"))
	ColorModelSonnet = ld(lipgloss.Color("4"), lipgloss.Color("75"))
	ColorModelHaiku = ld(lipgloss.Color("2"), lipgloss.Color("114"))

	// Token highlight
	ColorTokenHigh = ld(lipgloss.Color("3"), lipgloss.Color("208"))

	// Ongoing indicator
	ColorOngoing = ld(lipgloss.Color("2"), lipgloss.Color("76"))

	// Context usage thresholds
	ColorContextOk = ld(lipgloss.Color("2"), lipgloss.Color("114"))
	ColorContextWarn = ld(lipgloss.Color("3"), lipgloss.Color("208"))
	ColorContextCrit = ld(lipgloss.Color("1"), lipgloss.Color("196"))

	// Permission mode pill backgrounds
	ColorPillBypass = ld(lipgloss.Color("1"), lipgloss.Color("196"))
	ColorPillAcceptEdits = ld(lipgloss.Color("5"), lipgloss.Color("135"))
	ColorPillPlan = ld(lipgloss.Color("2"), lipgloss.Color("114"))

	// Picker
	ColorPickerSelectedBg = ld(lipgloss.Color("254"), lipgloss.Color("237"))
	ColorPickerMeta = ColorTextMuted
	ColorGitBranch = ld(lipgloss.Color("5"), lipgloss.Color("135"))

	// Tool category colors — all dim for now.
	ColorToolRead = ColorTextDim
	ColorToolEdit = ColorTextDim
	ColorToolWrite = ColorTextDim
	ColorToolBash = ColorTextDim
	ColorToolGrep = ColorTextDim
	ColorToolGlob = ColorTextDim
	ColorToolTask = ColorTextDim
	ColorToolSkill = ColorTextDim
	ColorToolWeb = ColorTextDim
	ColorToolOther = ColorTextDim

	// Team member colors
	ColorTeamBlue = ld(lipgloss.Color("4"), lipgloss.Color("75"))
	ColorTeamGreen = ld(lipgloss.Color("2"), lipgloss.Color("114"))
	ColorTeamRed = ld(lipgloss.Color("1"), lipgloss.Color("204"))
	ColorTeamYellow = ld(lipgloss.Color("3"), lipgloss.Color("220"))
	ColorTeamPurple = ld(lipgloss.Color("5"), lipgloss.Color("177"))
	ColorTeamCyan = ld(lipgloss.Color("6"), lipgloss.Color("80"))
	ColorTeamOrange = ld(lipgloss.Color("3"), lipgloss.Color("208"))
	ColorTeamPink = ld(lipgloss.Color("5"), lipgloss.Color("211"))

	// Rebuild styles with resolved colors.
	StylePrimaryBold = lipgloss.NewStyle().Bold(true).Foreground(ColorTextPrimary)
	StyleSecondary = lipgloss.NewStyle().Foreground(ColorTextSecondary)
	StyleSecondaryBold = lipgloss.NewStyle().Bold(true).Foreground(ColorTextSecondary)
	StyleDim = lipgloss.NewStyle().Foreground(ColorTextDim)
	StyleMuted = lipgloss.NewStyle().Foreground(ColorTextMuted)
	StyleAccentBold = lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	StyleErrorBold = lipgloss.NewStyle().Bold(true).Foreground(ColorError)
}
