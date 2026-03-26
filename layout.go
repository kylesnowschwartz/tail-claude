package main

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// screenLayout holds all the pieces needed to assemble a full-screen view.
// The assemble method guarantees the output is exactly screenH lines tall.
type screenLayout struct {
	header  string   // fixed lines above scrollable content ("" = skip)
	lines   []string // scrollable content (already scrolled/truncated by caller)
	middle  string   // between content and footer ("" = skip)
	footer  string   // rendered footer
	screenH int      // total target height in lines
	width   int      // terminal width
	cw      int      // content width cap (for centerBlock)
}

// assemble composes header, content, middle, and footer into a single string
// of exactly screenH lines. Content is truncated or padded to fill the
// remaining space after fixed sections are measured.
func (sl screenLayout) assemble() string {
	headerH := 0
	if sl.header != "" {
		headerH = lipgloss.Height(sl.header)
	}
	middleH := 0
	if sl.middle != "" {
		middleH = lipgloss.Height(sl.middle)
	}
	footerH := lipgloss.Height(sl.footer)

	contentH := sl.screenH - headerH - middleH - footerH
	if contentH < 0 {
		contentH = 0
	}

	// Truncate or pad lines to exactly contentH.
	lines := sl.lines
	switch {
	case len(lines) > contentH:
		lines = lines[:contentH]
	case len(lines) < contentH:
		padded := make([]string, contentH)
		copy(padded, lines)
		// Remaining entries are "" (empty lines).
		lines = padded
	}

	contentBlock := strings.Join(lines, "\n")
	contentBlock = centerBlock(contentBlock, sl.cw, sl.width)

	var sections []string
	if sl.header != "" {
		sections = append(sections, sl.header)
	}
	sections = append(sections, contentBlock)
	if sl.middle != "" {
		sections = append(sections, sl.middle)
	}
	sections = append(sections, sl.footer)

	return strings.Join(sections, "\n")
}
