package main

import (
	"fmt"
	"strings"

	"github.com/kylesnowschwartz/tail-claude/parser"

	"github.com/charmbracelet/lipgloss"
)

// -- Layout constants ---------------------------------------------------------

// maxContentWidth is the maximum width for content rendering.
const maxContentWidth = 120

// maxCollapsedLines is the maximum content lines shown when a message is collapsed.
const maxCollapsedLines = 6

// statusBarHeight is the number of rendered lines the status bar occupies.
// Rounded border: top + content + bottom = 3 lines.
const statusBarHeight = 3

// beadCount is the number of dots in the activity indicator animation.
const beadCount = 5

// -- Helpers ------------------------------------------------------------------

// chevron returns the expand/collapse indicator
func chevron(expanded bool) string {
	if expanded {
		return IconExpanded.Render()
	}
	return IconCollapsed.Render()
}

// selectionIndicator returns a left-margin marker for the selected message
func selectionIndicator(selected bool) string {
	if selected {
		return IconSelected.Render() + " "
	}
	return "  "
}

// spaceBetween lays out left and right strings with gap-fill spacing to span width.
func spaceBetween(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		gap = 2
	}
	return left + strings.Repeat(" ", gap) + right
}

// indentBlock adds a prefix to every line of a block of text.
func indentBlock(text string, indent string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n")
}

// contentWidth returns the inner width for card content, given a card width.
// Subtracts border (2) + padding (4) = 6 and floors at 20.
func contentWidth(cardWidth int) int {
	w := cardWidth - 6
	if w < 20 {
		w = 20
	}
	return w
}

// truncateLines caps content to maxLines and returns the truncated text plus
// the number of hidden lines. Returns (content, 0) when within the limit.
func truncateLines(content string, maxLines int) (string, int) {
	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content, 0
	}
	return strings.Join(lines[:maxLines], "\n"), len(lines) - maxLines
}

// formatToolResultPreview renders a one-line tool result summary for collapsed view.
func formatToolResultPreview(lo *parser.LastOutput) string {
	icon := IconToolOk
	if lo.IsError {
		icon = IconToolErr
	}
	nameStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorTextPrimary)
	resultStyle := lipgloss.NewStyle().Foreground(ColorTextSecondary)

	result := lo.ToolResult
	if len(result) > 200 {
		result = result[:200] + GlyphEllipsis
	}
	// Collapse newlines for single-line preview
	result = strings.ReplaceAll(result, "\n", " ")

	return icon.Render() + " " + nameStyle.Render(lo.ToolName) + " " + resultStyle.Render(truncate(result, 80))
}

// -- Message rendering --------------------------------------------------------

func (m model) renderMessage(msg message, containerWidth int, isSelected, isExpanded bool) string {
	switch msg.role {
	case RoleClaude:
		return m.renderClaudeMessage(msg, containerWidth, isSelected, isExpanded)
	case RoleUser:
		return m.renderUserMessage(msg, containerWidth, isSelected, isExpanded)
	case RoleSystem:
		return renderSystemMessage(msg, containerWidth, isSelected, isExpanded)
	case RoleCompact:
		return renderCompactMessage(msg, containerWidth)
	default:
		return msg.content
	}
}

func (m model) renderClaudeMessage(msg message, containerWidth int, isSelected, isExpanded bool) string {
	sel := selectionIndicator(isSelected)
	chev := chevron(isExpanded)
	maxWidth := containerWidth - 4 // selection indicator (2) + gutter (2)

	// Delegate to renderDetailHeader with chevron appended after stats.
	headerLine := sel + "  " + m.renderDetailHeader(msg, maxWidth, chev)

	// Render the card body -- truncate when collapsed
	content := msg.content
	if !isExpanded {
		if msg.lastOutput != nil {
			switch msg.lastOutput.Type {
			case parser.LastOutputText:
				content = msg.lastOutput.Text
				truncated, hidden := truncateLines(content, maxCollapsedLines)
				if hidden > 0 {
					content = truncated + "\n" + fmt.Sprintf("%s (%d lines hidden)", GlyphEllipsis, hidden)
				}
			case parser.LastOutputToolResult:
				content = formatToolResultPreview(msg.lastOutput)
			}
		} else {
			truncated, hidden := truncateLines(content, maxCollapsedLines)
			if hidden > 0 {
				content = truncated + "\n" + fmt.Sprintf("%s (%d lines hidden)", GlyphEllipsis, hidden)
			}
		}
	}

	contentWidth := contentWidth(maxWidth)
	var body string
	if isExpanded && len(msg.items) > 0 {
		// Structured item rows, then last output text at the bottom (matches claude-devtools)
		var rows []string
		for i, item := range msg.items {
			rows = append(rows, m.renderDetailItemRow(item, i, -1, contentWidth))
		}

		// Append truncated last output text below the items
		if msg.lastOutput != nil && msg.lastOutput.Text != "" {
			outputText := msg.lastOutput.Text
			truncated, hidden := truncateLines(outputText, maxCollapsedLines)
			if hidden > 0 {
				outputText = truncated
			}
			rendered := m.md.renderMarkdown(outputText, contentWidth)
			rows = append(rows, "", rendered) // blank line separator
			if hidden > 0 {
				hint := lipgloss.NewStyle().Foreground(ColorTextSecondary).
					Render(fmt.Sprintf("%s %d more lines — Enter for full text", GlyphEllipsis, hidden))
				rows = append(rows, hint)
			}
		}

		body = strings.Join(rows, "\n")
	} else {
		body = m.md.renderMarkdown(content, contentWidth)
	}

	cardBorderColor := ColorBorder
	if isSelected {
		cardBorderColor = ColorAccent
	}

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cardBorderColor).
		Width(maxWidth).
		Padding(0, 2)

	card := cardStyle.Render(body)

	// Indent card to align with header content
	cardLines := strings.Split(card, "\n")
	var indented []string
	for _, line := range cardLines {
		indented = append(indented, sel+"  "+line)
	}

	return headerLine + "\n" + strings.Join(indented, "\n")
}

func (m model) renderUserMessage(msg message, containerWidth int, isSelected, isExpanded bool) string {
	sel := selectionIndicator(isSelected)
	maxBubbleWidth := containerWidth * 3 / 4

	// Use full terminal width for alignment so user messages right-align to
	// the terminal edge, not just within the 120-col content area.
	alignWidth := m.width
	if alignWidth < containerWidth {
		alignWidth = containerWidth
	}

	// Header: timestamp + You + icon, right-aligned to terminal edge
	ts := lipgloss.NewStyle().
		Foreground(ColorTextDim).
		Render(msg.timestamp)

	youLabel := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorTextPrimary).
		Render("You")

	userIcon := IconUser.Render()

	rightPart := ts + "  " + youLabel + " " + userIcon
	leftPart := sel

	headerGap := alignWidth - lipgloss.Width(leftPart) - lipgloss.Width(rightPart)
	if headerGap < 0 {
		headerGap = 0
	}
	header := leftPart + strings.Repeat(" ", headerGap) + rightPart

	bubbleBorderColor := ColorTextMuted
	if isSelected {
		bubbleBorderColor = ColorAccent
	}

	content := msg.content
	var hint string

	// Truncate long user messages when collapsed
	if !isExpanded {
		truncated, hidden := truncateLines(content, maxCollapsedLines)
		if hidden > 0 {
			content = truncated
			hint = lipgloss.NewStyle().Foreground(ColorTextDim).
				Render(fmt.Sprintf("… (%d lines hidden)", hidden))
		}
	}

	// Render markdown content inside the bubble, then append the hint
	bubbleInnerWidth := maxBubbleWidth - 6 // subtract border (2) + padding (4)
	if bubbleInnerWidth < 20 {
		bubbleInnerWidth = 20
	}
	rendered := m.md.renderMarkdown(content, bubbleInnerWidth)
	if hint != "" {
		rendered += "\n" + hint
	}

	bubbleStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(bubbleBorderColor).
		Padding(0, 2).
		MaxWidth(maxBubbleWidth)

	bubble := bubbleStyle.Render(rendered)
	alignedBubble := lipgloss.PlaceHorizontal(alignWidth, lipgloss.Right, bubble)

	return header + "\n" + alignedBubble
}

func renderSystemMessage(msg message, containerWidth int, isSelected, _ bool) string {
	// System messages always show inline -- they're short
	sel := selectionIndicator(isSelected)

	sysIcon := IconSystem.Render()

	label := lipgloss.NewStyle().
		Foreground(ColorTextSecondary).
		Render("System")

	ts := lipgloss.NewStyle().
		Foreground(ColorTextDim).
		Render(msg.timestamp)

	content := lipgloss.NewStyle().
		Foreground(ColorTextDim).
		Render(msg.content)

	return sel + sysIcon + " " + label + "  " + IconDot.Glyph + "  " + ts + "  " + content
}

func renderCompactMessage(msg message, width int) string {
	dim := lipgloss.NewStyle().Foreground(ColorTextMuted)
	text := msg.content
	if text == "" {
		text = "Context compressed"
	}
	textWidth := lipgloss.Width(text) + 4 // " text " with spacing
	leftPad := (width - textWidth) / 2
	if leftPad < 0 {
		leftPad = 0
	}
	rightPad := width - leftPad - textWidth
	if rightPad < 0 {
		rightPad = 0
	}
	left := strings.Repeat(GlyphHRule, leftPad)
	right := strings.Repeat(GlyphHRule, rightPad)
	return dim.Render(left + " " + text + " " + right)
}

// -- Detail rendering ---------------------------------------------------------

// renderDetailContent renders the full detail content for the current message.
// Used by both computeDetailMaxScroll and viewDetail to avoid duplication.
func (m model) renderDetailContent(msg message, width int) string {
	// AI messages with items get the structured items view.
	if msg.role == RoleClaude && len(msg.items) > 0 {
		return m.renderDetailItemsContent(msg, width)
	}

	var header, body string
	switch msg.role {
	case RoleClaude:
		header = m.renderDetailHeader(msg, width)
		body = m.md.renderMarkdown(msg.content, width-4)
	case RoleUser:
		header = lipgloss.NewStyle().Foreground(ColorTextDim).Render(msg.timestamp) +
			"  " + lipgloss.NewStyle().Bold(true).Foreground(ColorTextPrimary).Render("You") +
			" " + IconUser.Render()
		body = m.md.renderMarkdown(msg.content, width-4)
	case RoleSystem:
		header = IconSystem.Render() +
			" " + lipgloss.NewStyle().Foreground(ColorTextSecondary).Render("System") +
			"  " + lipgloss.NewStyle().Foreground(ColorTextDim).Render(msg.timestamp)
		body = lipgloss.NewStyle().Foreground(ColorTextDim).Render(msg.content)
	case RoleCompact:
		return renderCompactMessage(msg, width)
	}

	return header + "\n\n" + body
}

// renderDetailItemsContent renders the full content for an AI message with
// structured items (header + items list + expanded content). Returns the
// complete string before scrolling is applied.
func (m model) renderDetailItemsContent(msg message, width int) string {
	header := m.renderDetailHeader(msg, width)

	var itemLines []string
	for i, item := range msg.items {
		row := m.renderDetailItemRow(item, i, m.detailCursor, width)

		if m.detailExpanded[i] {
			expanded := m.renderDetailItemExpanded(item, width)
			if expanded != "" {
				row += "\n" + expanded
			}
		}
		itemLines = append(itemLines, row)
	}

	return header + "\n\n" + strings.Join(itemLines, "\n")
}

// renderDetailItemRow renders a single item row in the detail view.
// Format: {cursor} {indicator} {name:<12} {summary}  {tokens} {duration}
func (m model) renderDetailItemRow(item displayItem, index, cursorIndex, width int) string {
	// Cursor indicator: drillable items get a distinct arrow, expanded items
	// get chevron-down, collapsed items get chevron-right.
	cursor := "  "
	if index == cursorIndex {
		if item.subagentProcess != nil {
			cursor = IconDrillDown.RenderBold() + " "
		} else if m.detailExpanded[index] {
			cursor = IconExpanded.RenderBold() + " "
		} else {
			cursor = IconCollapsed.Render() + " "
		}
	}

	// Type indicator and name
	var indicator, name string

	switch item.itemType {
	case parser.ItemThinking:
		indicator = IconThinking.Render()
		name = "Thinking"
	case parser.ItemOutput:
		indicator = IconOutput.Render()
		name = "Output"
		if item.toolName != "" {
			name = item.toolName
		}
	case parser.ItemToolCall:
		if item.toolError {
			indicator = IconToolErr.Render()
		} else {
			indicator = IconToolOk.Render()
		}
		name = item.toolName
	case parser.ItemSubagent:
		indicator = IconSubagent.Render()
		name = item.subagentType
		if name == "" {
			name = "Subagent"
		}
	case parser.ItemTeammateMessage:
		indicator = IconTeammate.Render()
		name = item.teammateID
		if name == "" {
			name = "Teammate"
		}
	}

	// Pad name to 12 chars
	nameStr := fmt.Sprintf("%-12s", name)
	nameRendered := lipgloss.NewStyle().Bold(true).Foreground(ColorTextPrimary).Render(nameStr)

	// Summary
	var summary string
	switch item.itemType {
	case parser.ItemThinking, parser.ItemOutput:
		summary = truncate(item.text, 40)
	case parser.ItemToolCall:
		summary = item.toolSummary
	case parser.ItemSubagent:
		summary = item.subagentDesc
		if summary == "" {
			summary = item.toolSummary
		}
	case parser.ItemTeammateMessage:
		summary = truncate(item.text, 60)
	}
	summaryRendered := lipgloss.NewStyle().Foreground(ColorTextSecondary).Render(summary)

	// Right-side: tokens + duration.
	// Prefer subagent process stats when linked (actual internal consumption).
	tokCount := item.tokenCount
	durMs := item.durationMs
	if item.subagentProcess != nil {
		if t := item.subagentProcess.Usage.TotalTokens(); t > 0 {
			tokCount = t
		}
		if d := item.subagentProcess.DurationMs; d > 0 {
			durMs = d
		}
	}
	var rightParts []string
	if tokCount > 0 {
		tokStr := fmt.Sprintf("~%s tok", formatTokens(tokCount))
		rightParts = append(rightParts, lipgloss.NewStyle().Foreground(ColorTextDim).Render(tokStr))
	}
	if durMs > 0 {
		durStr := fmt.Sprintf("%dms", durMs)
		if durMs >= 1000 {
			durStr = formatDuration(durMs)
		}
		rightParts = append(rightParts, lipgloss.NewStyle().Foreground(ColorTextDim).Render(durStr))
	}
	rightSide := strings.Join(rightParts, "  ")

	var left string
	if summary != "" {
		left = cursor + indicator + " " + nameRendered + lipgloss.NewStyle().Foreground(ColorTextDim).Render(" - ") + summaryRendered
	} else {
		left = cursor + indicator + " " + nameRendered
	}
	return spaceBetween(left, rightSide, width)
}

// renderDetailItemExpanded renders the expanded content for a detail item.
// Indented 4 spaces, word-wrapped to width-8.
func (m model) renderDetailItemExpanded(item displayItem, width int) string {
	wrapWidth := width - 8
	if wrapWidth < 20 {
		wrapWidth = 20
	}
	indent := "    "

	switch item.itemType {
	case parser.ItemThinking, parser.ItemOutput, parser.ItemTeammateMessage:
		text := strings.TrimSpace(item.text)
		if text == "" {
			return ""
		}
		rendered := m.md.renderMarkdown(text, wrapWidth)
		return indentBlock(rendered, indent)

	case parser.ItemSubagent:
		// Linked subagent: show execution trace summary.
		if item.subagentProcess != nil {
			return m.renderSubagentTrace(item, wrapWidth, indent)
		}
		// Unlinked subagent: fall through to tool call rendering.
		return m.renderToolExpanded(item, wrapWidth, indent)

	case parser.ItemToolCall:
		return m.renderToolExpanded(item, wrapWidth, indent)
	}

	return ""
}

// renderToolExpanded renders the expanded content for a tool call item.
func (m model) renderToolExpanded(item displayItem, wrapWidth int, indent string) string {
	var sections []string

	if item.toolInput != "" {
		headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorTextSecondary)
		sections = append(sections, indent+headerStyle.Render("Input:"))
		inputStyle := lipgloss.NewStyle().
			Foreground(ColorTextDim).
			Width(wrapWidth)
		sections = append(sections, indentBlock(inputStyle.Render(item.toolInput), indent))
	}

	if item.toolResult != "" || item.toolError {
		if len(sections) > 0 {
			sepStyle := lipgloss.NewStyle().Foreground(ColorTextMuted)
			sections = append(sections, indent+sepStyle.Render(strings.Repeat("-", wrapWidth)))
		}

		if item.toolError {
			headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorError)
			sections = append(sections, indent+headerStyle.Render("Error:"))
		} else {
			headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorTextSecondary)
			sections = append(sections, indent+headerStyle.Render("Result:"))
		}

		resultStyle := lipgloss.NewStyle().
			Foreground(ColorTextDim).
			Width(wrapWidth)
		sections = append(sections, indentBlock(resultStyle.Render(item.toolResult), indent))
	}

	if len(sections) == 0 {
		return ""
	}
	return strings.Join(sections, "\n")
}

// renderSubagentTrace renders an execution trace for a linked subagent,
// matching the claude-devtools layout: a header line with counts, followed
// by a flat list of all items across the subagent's chunks.
func (m model) renderSubagentTrace(item displayItem, wrapWidth int, indent string) string {
	proc := item.subagentProcess
	dimStyle := lipgloss.NewStyle().Foreground(ColorTextDim)

	// Build flat trace items from all subagent chunks.
	// UserChunks become "Input" items; AIChunk items pass through directly.
	var traceItems []displayItem
	var toolCount, msgCount int

	for _, c := range proc.Chunks {
		switch c.Type {
		case parser.UserChunk:
			traceItems = append(traceItems, displayItem{
				itemType: parser.ItemOutput,
				toolName: "Input", // overrides "Output" label in renderDetailItemRow
				text:     c.UserText,
			})
			msgCount++
		case parser.AIChunk:
			for _, it := range c.Items {
				traceItems = append(traceItems, displayItem{
					itemType:     it.Type,
					text:         it.Text,
					toolName:     it.ToolName,
					toolSummary:  it.ToolSummary,
					toolResult:   it.ToolResult,
					toolError:    it.ToolError,
					durationMs:   it.DurationMs,
					tokenCount:   it.TokenCount,
					subagentType: it.SubagentType,
					subagentDesc: it.SubagentDesc,
				})
				switch it.Type {
				case parser.ItemToolCall, parser.ItemSubagent:
					toolCount++
				case parser.ItemOutput:
					msgCount++
				}
			}
		}
	}

	// Header: >_ Execution Trace · N tool calls, N messages
	traceIcon := IconSystem.WithColor(ColorTextDim) // terminal icon for execution context
	traceLabel := lipgloss.NewStyle().Bold(true).
		Foreground(ColorTextPrimary).Render("Execution Trace")
	dot := dimStyle.Render(" " + IconDot.Glyph + " ")
	countStr := dimStyle.Render(fmt.Sprintf("%d tool calls, %d messages", toolCount, msgCount))

	var lines []string
	lines = append(lines, indent+traceIcon+"  "+traceLabel+dot+countStr)

	// All trace items as compact rows (no cursor, no cap).
	nestedWidth := wrapWidth - 4
	for i, di := range traceItems {
		row := m.renderDetailItemRow(di, i, -1, nestedWidth)
		lines = append(lines, indent+"  "+row)
	}

	return strings.Join(lines, "\n")
}

// renderDetailHeader renders metadata for the detail view header.
// An optional leftSuffix is appended after the stats (used for the chevron
// in list view). Matches the list view header layout for visual consistency.
//
// When in a trace drill-down (savedDetail != nil && traceMsg != nil), a dim
// breadcrumb prefix shows the parent view: "Claude opus4.6 > ..."
func (m model) renderDetailHeader(msg message, width int, leftSuffix ...string) string {
	headerIcon := IconClaude
	headerLabel := "Claude"
	if msg.subagentLabel != "" {
		headerIcon = IconSubagent
		headerLabel = msg.subagentLabel
	}
	icon := headerIcon.RenderBold()
	modelName := lipgloss.NewStyle().Bold(true).Foreground(ColorTextPrimary).Render(headerLabel)
	modelVer := lipgloss.NewStyle().Foreground(modelColor(msg.model)).Render(msg.model)

	var statParts []string
	if msg.thinkingCount > 0 {
		statParts = append(statParts, fmt.Sprintf("%d thinking", msg.thinkingCount))
	}
	if msg.toolCallCount > 0 {
		tcLabel := "tool calls"
		if msg.toolCallCount == 1 {
			tcLabel = "tool call"
		}
		statParts = append(statParts, fmt.Sprintf("%d %s", msg.toolCallCount, tcLabel))
	}
	if msg.messages > 0 {
		label := "messages"
		if msg.messages == 1 {
			label = "message"
		}
		statParts = append(statParts, fmt.Sprintf("%d %s", msg.messages, label))
	}
	if msg.teammateSpawns > 0 {
		label := "teammates"
		if msg.teammateSpawns == 1 {
			label = "teammate"
		}
		statParts = append(statParts, fmt.Sprintf("%d %s", msg.teammateSpawns, label))
	}
	if msg.teammateMessages > 0 {
		label := "teammate messages"
		if msg.teammateMessages == 1 {
			label = "teammate message"
		}
		statParts = append(statParts, fmt.Sprintf("%d %s", msg.teammateMessages, label))
	}

	stats := ""
	if len(statParts) > 0 {
		dot := " " + IconDot.Render() + " "
		stats = dot + lipgloss.NewStyle().Foreground(ColorTextSecondary).Render(strings.Join(statParts, ", "))
	}

	// Breadcrumb prefix when drilled into a subagent trace.
	var breadcrumb string
	if m.savedDetail != nil && m.traceMsg != nil {
		parentStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
		sep := lipgloss.NewStyle().Foreground(ColorTextMuted).Render(" > ")
		breadcrumb = parentStyle.Render(m.savedDetail.label) + sep
	}

	left := breadcrumb + icon + " " + modelName + " " + modelVer + stats
	for _, s := range leftSuffix {
		left += " " + s
	}

	// Right-side metadata
	var rightParts []string

	if msg.tokensRaw > 0 {
		coin := IconToken.Render()
		rightParts = append(rightParts, coin+" "+lipgloss.NewStyle().
			Foreground(ColorTextSecondary).
			Render(formatTokens(msg.tokensRaw)))
	}

	if msg.durationMs > 0 {
		clock := IconClock.Render()
		rightParts = append(rightParts, clock+" "+lipgloss.NewStyle().
			Foreground(ColorTextSecondary).
			Render(formatDuration(msg.durationMs)))
	}

	if msg.timestamp != "" {
		rightParts = append(rightParts, lipgloss.NewStyle().
			Foreground(ColorTextDim).
			Render(msg.timestamp))
	}

	return spaceBetween(left, strings.Join(rightParts, "  "), width)
}

// -- Activity indicator --------------------------------------------------------

// activityIndicatorHeight returns the number of extra lines the activity
// indicator occupies above the status bar (0 or 1).
func (m model) activityIndicatorHeight() int {
	if m.watching && m.sessionOngoing {
		return 1
	}
	return 0
}

// renderActivityIndicator returns a centered animated bead line when the
// session is ongoing, or an empty string otherwise. Each tick shifts the
// bright "head" position through 5 dots.
func (m model) renderActivityIndicator(width int) string {
	if !m.watching || !m.sessionOngoing {
		return ""
	}

	// Color palette from brightest to dimmest.
	colors := []lipgloss.AdaptiveColor{
		ColorAccent,        // head (bright blue)
		ColorInfo,          // near head
		ColorTextSecondary, // mid
		ColorTextMuted,     // dim
	}

	head := m.animFrame % beadCount
	var dots []string
	for i := 0; i < beadCount; i++ {
		// Distance from head, wrapping around.
		dist := (i - head + beadCount) % beadCount
		// Map distance to a color index (0=closest, capped at len-1).
		ci := dist
		if ci >= len(colors) {
			ci = len(colors) - 1
		}
		style := lipgloss.NewStyle().Foreground(colors[ci])
		dots = append(dots, style.Render(GlyphBeadFull))
	}

	line := strings.Join(dots, " ")
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, line)
}

// -- Status bar ---------------------------------------------------------------

// renderStatusBar renders key hints in a rounded-border box.
// When m.watching is true, a dim "tail" indicator is prepended.
func (m model) renderStatusBar(pairs ...string) string {
	keyStyle := lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true)

	descStyle := lipgloss.NewStyle().
		Foreground(ColorTextDim)

	sep := " " + IconDot.Render() + " "

	var hints []string

	if m.watching {
		tailLabel := lipgloss.NewStyle().
			Foreground(ColorTextMuted).
			Render("tail")
		hints = append(hints, tailLabel)
	}

	for i := 0; i+1 < len(pairs); i += 2 {
		hints = append(hints, keyStyle.Render(pairs[i])+" "+descStyle.Render(pairs[i+1]))
	}

	barStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorBorder).
		Width(m.width-2). // border chars take 2 columns
		Padding(0, 1)

	return barStyle.Render(strings.Join(hints, sep))
}
