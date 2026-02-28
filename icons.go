package main

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/kylesnowschwartz/tail-claude/parser"
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

// Shared glyphs -- named so intentional reuse across icons is explicit.
// All use Unicode escapes to prevent silent corruption by LLM tools.
const (
	glyphRobot        = "\U000F167A" // nf-md-robot_outline
	glyphWrench       = "\U000F0BE0" // nf-md-wrench_outline
	glyphFolderSearch = "\U000F0968" // nf-md-folder_search
	glyphPenNib       = "\uEE75"     // nf-fa-pen_nib
)

// toolIcons groups per-category icons for the detail view item rows.
type toolIcons struct {
	Err   StyledIcon
	Ok    StyledIcon
	Read  StyledIcon
	Edit  StyledIcon
	Write StyledIcon
	Bash  StyledIcon
	Grep  StyledIcon
	Glob  StyledIcon
	Task  StyledIcon
	Skill StyledIcon
	Web   StyledIcon
	Misc  StyledIcon
}

// taskIcons groups status glyphs for the task board.
type taskIcons struct {
	Done    StyledIcon
	Active  StyledIcon
	Pending StyledIcon
}

// iconSet holds every icon in the TUI, grouped by domain.
// Requires a Nerd Font patched terminal font (e.g. JetBrains Mono Nerd Font).
// Codepoints from Font Awesome (U+F000-U+F2E0) and Material Design (U+F0001+).
type iconSet struct {
	Branch    StyledIcon
	Chat      StyledIcon
	Claude    StyledIcon
	Clock     StyledIcon
	Collapsed StyledIcon
	Dot       StyledIcon
	DrillDown StyledIcon
	Ellipsis  StyledIcon
	Expanded  StyledIcon
	Output    StyledIcon
	Selected  StyledIcon
	Session   StyledIcon
	Subagent  StyledIcon
	System    StyledIcon
	SystemErr StyledIcon
	Teammate  StyledIcon
	Thinking  StyledIcon
	Token     StyledIcon
	User      StyledIcon
	Tool      toolIcons
	Task      taskIcons
}

// Icon is the single source of truth for all TUI icons.
// Initialized by initIcons() after initTheme() resolves colors.
var Icon iconSet

// Plain glyphs -- used as raw strings (never styled via StyledIcon).
const (
	GlyphHRule    = "\u2500" // box drawing horizontal (compact separators)
	GlyphBeadFull = "\uEABC" // nf-cod-circle (activity indicator bead)
)

// SpinnerFrames is a 10-frame braille spinner used for ongoing indicators.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// initIcons builds all icon values from resolved theme colors.
// Must be called after initTheme().
//
// All glyphs use explicit Unicode escapes (\uXXXX / \U000XXXXX) to prevent
// silent corruption when LLM tools round-trip the file. Nerd Font codepoints
// in the Private Use Area are particularly vulnerable to being dropped.
func initIcons() {
	Icon = iconSet{
		Branch:    StyledIcon{"\uE0A0", ColorGitBranch}, // nf-pl-branch
		Chat:      StyledIcon{"\uF086", ColorTextDim},   // nf-fa-comments
		Claude:    StyledIcon{glyphRobot, ColorInfo},
		Clock:     StyledIcon{"\uF017", ColorTextDim},     // nf-fa-clock
		Collapsed: StyledIcon{"\uF054", ColorTextDim},     // nf-fa-chevron_right
		Dot:       StyledIcon{"\u00B7", ColorTextMuted},   // middle dot
		DrillDown: StyledIcon{"\uF061", ColorAccent},      // nf-fa-arrow_right
		Ellipsis:  StyledIcon{"\u2026", ColorTextDim},     // horizontal ellipsis
		Expanded:  StyledIcon{"\uF078", ColorTextPrimary}, // nf-fa-chevron_down
		Output:    StyledIcon{"\U000F0182", ColorAccent},  // nf-md-comment_outline
		Selected:  StyledIcon{"\u2502", ColorAccent},      // box drawing vertical
		Session:   StyledIcon{"\U000F0237", ColorTextDim}, // nf-md-fingerprint
		Subagent:  StyledIcon{glyphRobot, ColorAccent},
		System:    StyledIcon{"\uF120", ColorTextMuted}, // nf-fa-terminal
		SystemErr: StyledIcon{"\uF06A", ColorError},     // nf-fa-circle_exclamation
		Teammate:  StyledIcon{glyphRobot, ColorAccent},
		Thinking:  StyledIcon{"\uF0EB", ColorTextDim},       // nf-fa-lightbulb
		Token:     StyledIcon{"\uEDE8", ColorTextDim},       // nf-fa-coins
		User:      StyledIcon{"\uF007", ColorTextSecondary}, // nf-fa-user
		Tool: toolIcons{
			Err:   StyledIcon{glyphWrench, ColorError},
			Ok:    StyledIcon{glyphWrench, ColorTextDim},
			Read:  StyledIcon{"\uE28B", ColorToolRead}, // nf-fae-book_open_o
			Edit:  StyledIcon{glyphPenNib, ColorToolEdit},
			Write: StyledIcon{glyphPenNib, ColorToolWrite},
			Bash:  StyledIcon{glyphWrench, ColorToolBash},
			Grep:  StyledIcon{glyphFolderSearch, ColorToolGrep},
			Glob:  StyledIcon{glyphFolderSearch, ColorToolGlob},
			Task:  StyledIcon{glyphRobot, ColorToolTask},
			Skill: StyledIcon{glyphWrench, ColorToolSkill},
			Web:   StyledIcon{"\U000F059F", ColorToolWeb}, // nf-md-web
			Misc:  StyledIcon{glyphWrench, ColorToolOther},
		},
		Task: taskIcons{
			Done:    StyledIcon{"\u2713", ColorOngoing},   // check mark
			Active:  StyledIcon{"\u27F3", ColorAccent},    // clockwise arrow
			Pending: StyledIcon{"\u25CB", ColorTextMuted}, // white circle
		},
	}
}

// toolCategoryIcon returns the styled icon for a tool category.
// Error tools always get the red error icon regardless of category.
func toolCategoryIcon(cat parser.ToolCategory, isError bool) string {
	if isError {
		return Icon.Tool.Err.Render()
	}
	switch cat {
	case parser.CategoryRead:
		return Icon.Tool.Read.Render()
	case parser.CategoryEdit:
		return Icon.Tool.Edit.Render()
	case parser.CategoryWrite:
		return Icon.Tool.Write.Render()
	case parser.CategoryBash:
		return Icon.Tool.Bash.Render()
	case parser.CategoryGrep:
		return Icon.Tool.Grep.Render()
	case parser.CategoryGlob:
		return Icon.Tool.Glob.Render()
	case parser.CategoryTask:
		return Icon.Tool.Task.Render()
	case parser.CategoryTool:
		return Icon.Tool.Skill.Render()
	case parser.CategoryWeb:
		return Icon.Tool.Web.Render()
	default:
		return Icon.Tool.Misc.Render()
	}
}
