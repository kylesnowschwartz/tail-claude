package main

import (
	"fmt"
	"strings"

	"github.com/kylesnowschwartz/tail-claude/parser"

	"github.com/charmbracelet/lipgloss"
)

// -- Rendered output ----------------------------------------------------------

// rendered pairs a rendered string with its line count. Eliminates the
// "render to string, count newlines" pattern used for scroll math.
type rendered struct {
	content string
	lines   int
}

// newRendered wraps a content string, counting its lines once.
func newRendered(content string) rendered {
	return rendered{content: content, lines: strings.Count(content, "\n") + 1}
}

// -- Layout constants ---------------------------------------------------------

// maxContentWidth is the maximum width for content rendering.
const maxContentWidth = 120

// maxCollapsedLines is the maximum content lines shown when a message is collapsed.
const maxCollapsedLines = 12

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

// userHeaderLine renders "timestamp  You {icon}" used in both list and detail views.
func userHeaderLine(msg message) string {
	return StyleDim.Render(msg.timestamp) + "  " + StylePrimaryBold.Render("You") + " " + IconUser.Render()
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
	return max(cardWidth-6, 20)
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
	nameStyle := StylePrimaryBold
	resultStyle := StyleSecondary

	result := lo.ToolResult
	if len(result) > 200 {
		result = result[:200] + IconEllipsis.Glyph
	}
	// Collapse newlines for single-line preview
	result = strings.ReplaceAll(result, "\n", " ")

	return icon.Render() + " " + nameStyle.Render(lo.ToolName) + " " + resultStyle.Render(parser.Truncate(result, 80))
}

// -- Message rendering --------------------------------------------------------

func (m model) renderMessage(msg message, containerWidth int, isSelected, isExpanded bool) rendered {
	var content string
	switch msg.role {
	case RoleClaude:
		content = m.renderClaudeMessage(msg, containerWidth, isSelected, isExpanded)
	case RoleUser:
		content = m.renderUserMessage(msg, containerWidth, isSelected, isExpanded)
	case RoleSystem:
		content = renderSystemMessage(msg, containerWidth, isSelected, isExpanded)
	case RoleCompact:
		content = renderCompactMessage(msg, containerWidth)
	default:
		content = msg.content
	}
	return newRendered(content)
}

func (m model) renderClaudeMessage(msg message, containerWidth int, isSelected, isExpanded bool) string {
	sel := selectionIndicator(isSelected)
	chev := chevron(isExpanded)
	maxWidth := containerWidth - 4 // selection indicator (2) + gutter (2)

	headerLine := sel + "  " + m.renderDetailHeader(msg, maxWidth, chev).content
	body := m.claudeMessageBody(msg, isExpanded, contentWidth(maxWidth))

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

// claudeMessageBody renders the inner card content for a Claude message.
// Expanded messages with items show structured rows; collapsed messages
// show the last output or a truncated text preview.
func (m model) claudeMessageBody(msg message, isExpanded bool, cw int) string {
	if isExpanded && len(msg.items) > 0 {
		return m.claudeExpandedItems(msg, cw)
	}

	content, hidden := m.claudeCollapsedContent(msg, isExpanded)
	rendered := m.md.renderMarkdown(content, cw)
	if hidden > 0 {
		hint := StyleDim.Render(
			fmt.Sprintf("%s (%d lines hidden)", IconEllipsis.Render(), hidden))
		rendered += "\n" + hint
	}
	return rendered
}

// claudeExpandedItems renders structured item rows plus truncated last output.
func (m model) claudeExpandedItems(msg message, cw int) string {
	var rows []string
	for i, item := range msg.items {
		rows = append(rows, m.renderDetailItemRow(item, i, -1, false, cw))
	}

	// Append truncated last output text below the items
	if msg.lastOutput != nil && msg.lastOutput.Text != "" {
		outputText := msg.lastOutput.Text
		truncated, hidden := truncateLines(outputText, maxCollapsedLines)
		if hidden > 0 {
			outputText = truncated
		}
		md := m.md.renderMarkdown(outputText, cw)
		rows = append(rows, "", md) // blank line separator
		if hidden > 0 {
			hint := StyleDim.Render(
				fmt.Sprintf("%s %d more lines — Enter for full text", IconEllipsis.Render(), hidden))
			rows = append(rows, hint)
		}
	}

	return strings.Join(rows, "\n")
}

// claudeCollapsedContent selects and truncates text for collapsed display.
// Returns the content string and the number of hidden lines (0 when not truncated).
// The caller is responsible for rendering the truncation hint after markdown.
func (m model) claudeCollapsedContent(msg message, isExpanded bool) (string, int) {
	content := msg.content
	if isExpanded {
		return content, 0
	}

	if msg.lastOutput != nil {
		switch msg.lastOutput.Type {
		case parser.LastOutputText:
			content = msg.lastOutput.Text
			truncated, hidden := truncateLines(content, maxCollapsedLines)
			if hidden > 0 {
				return truncated, hidden
			}
			return content, 0
		case parser.LastOutputToolResult:
			return formatToolResultPreview(msg.lastOutput), 0
		}
	}

	truncated, hidden := truncateLines(content, maxCollapsedLines)
	if hidden > 0 {
		return truncated, hidden
	}
	return content, 0
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

	// Header: right-aligned to terminal edge
	rightPart := userHeaderLine(msg)
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
			hint = StyleDim.Render(fmt.Sprintf("%s (%d lines hidden)", IconEllipsis.Render(), hidden))
		}
	}

	// Render markdown content inside the bubble, then append the hint
	bubbleInnerWidth := max(maxBubbleWidth-6, 20) // subtract border (2) + padding (4)
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

	// Right-align bubble within the space after the selection indicator
	selWidth := lipgloss.Width(sel)
	bubbleAlignWidth := alignWidth - selWidth
	if bubbleAlignWidth < maxBubbleWidth {
		bubbleAlignWidth = maxBubbleWidth
	}
	alignedBubble := lipgloss.PlaceHorizontal(bubbleAlignWidth, lipgloss.Right, bubble)

	// Prepend selection indicator to each bubble line
	bubbleLines := strings.Split(alignedBubble, "\n")
	var indented []string
	for _, line := range bubbleLines {
		indented = append(indented, sel+line)
	}

	return header + "\n" + strings.Join(indented, "\n")
}

func renderSystemMessage(msg message, containerWidth int, isSelected, _ bool) string {
	// System messages always show inline -- they're short
	sel := selectionIndicator(isSelected)

	sysIcon := IconSystem.Render()

	label := StyleSecondary.Render("System")

	ts := StyleDim.Render(msg.timestamp)

	content := StyleDim.Render(msg.content)

	return sel + sysIcon + " " + label + "  " + IconDot.Glyph + "  " + ts + "  " + content
}

func renderCompactMessage(msg message, width int) string {
	dim := StyleMuted
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
func (m model) renderDetailContent(msg message, width int) rendered {
	// AI messages with items get the structured items view.
	if msg.role == RoleClaude && len(msg.items) > 0 {
		return newRendered(m.renderDetailItemsContent(msg, width))
	}

	var header, body string
	switch msg.role {
	case RoleClaude:
		header = m.renderDetailHeader(msg, width).content
		body = m.md.renderMarkdown(msg.content, width-4)
	case RoleUser:
		header = userHeaderLine(msg)
		body = m.md.renderMarkdown(msg.content, width-4)
	case RoleSystem:
		header = IconSystem.Render() +
			" " + StyleSecondary.Render("System") +
			"  " + StyleDim.Render(msg.timestamp)
		body = StyleDim.Render(msg.content)
	case RoleCompact:
		return newRendered(renderCompactMessage(msg, width))
	}

	return newRendered(header + "\n\n" + body)
}

// renderDetailItemsContent renders the full content for an AI message with
// structured items (header + items list + expanded content). Returns the
// complete string before scrolling is applied.
//
// Uses the flat visible-row list so that expanded subagent children are
// interleaved with parent items and can receive cursor highlights.
func (m model) renderDetailItemsContent(msg message, width int) string {
	header := m.renderDetailHeader(msg, width).content
	rows := buildVisibleRows(msg.items, m.detailExpanded)

	childIndent := "    " // 4 spaces for child rows
	childWidth := max(width-4, 20)

	var itemLines []string
	for ri, row := range rows {
		if row.childIndex == -1 {
			// Parent row.
			isExp := m.detailExpanded[row.parentIndex]
			rowStr := m.renderDetailItemRow(row.item, ri, m.detailCursor, isExp, width)

			if isExp {
				if row.item.itemType == parser.ItemSubagent && row.item.subagentProcess != nil {
					// Trace header separates parent from its children.
					hdr := renderTraceHeader(row.item)
					rowStr += "\n" + childIndent + hdr
				} else {
					// Non-subagent or subagent without process: inline content.
					expanded := m.renderDetailItemExpanded(row.item, width)
					if expanded.content != "" {
						rowStr += "\n" + expanded.content
					}
				}
			}
			itemLines = append(itemLines, rowStr)
		} else {
			// Child row (indented, belongs to an expanded subagent).
			key := visibleRowKey{row.parentIndex, row.childIndex}
			isExp := m.detailChildExpanded[key]
			childRow := m.renderDetailItemRow(row.item, ri, m.detailCursor, isExp, childWidth)

			rowStr := childIndent + childRow
			if isExp {
				expanded := m.renderDetailItemExpanded(row.item, childWidth)
				if expanded.content != "" {
					rowStr += "\n" + indentBlock(expanded.content, childIndent)
				}
			}
			itemLines = append(itemLines, rowStr)
		}
	}

	return header + "\n\n" + strings.Join(itemLines, "\n")
}

// renderTraceHeader renders the "Execution Trace" separator between a parent
// subagent row and its child trace items. Not a navigable row.
func renderTraceHeader(parent displayItem) string {
	items := buildTraceItems(parent)
	toolCount, msgCount := traceItemStats(items)

	dimStyle := StyleDim
	traceIcon := IconSystem.WithColor(ColorTextDim)
	traceLabel := StylePrimaryBold.Render("Execution Trace")
	dot := dimStyle.Render(" " + IconDot.Glyph + " ")
	countStr := dimStyle.Render(fmt.Sprintf("%d tool calls, %d messages", toolCount, msgCount))

	return traceIcon + "  " + traceLabel + dot + countStr
}

// renderDetailItemRow renders a single item row in the detail view.
// Format: {cursor} {indicator} {name:<12} {summary}  {tokens} {duration}
// isExpanded controls the cursor chevron direction (down vs right).
func (m model) renderDetailItemRow(item displayItem, index, cursorIndex int, isExpanded bool, width int) string {
	// Cursor indicator: drillable items get a distinct arrow, expanded items
	// get chevron-down, collapsed items get chevron-right.
	cursor := "  "
	if index == cursorIndex {
		if item.subagentProcess != nil {
			cursor = IconDrillDown.RenderBold() + " "
		} else if isExpanded {
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
	nameRendered := StylePrimaryBold.Render(nameStr)

	// Summary
	var summary string
	switch item.itemType {
	case parser.ItemThinking, parser.ItemOutput:
		summary = parser.Truncate(item.text, 40)
	case parser.ItemToolCall:
		summary = item.toolSummary
	case parser.ItemSubagent:
		summary = item.subagentDesc
		if summary == "" {
			summary = item.toolSummary
		}
	case parser.ItemTeammateMessage:
		summary = parser.Truncate(item.text, 60)
	}
	summaryRendered := StyleSecondary.Render(summary)

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
		rightParts = append(rightParts, StyleDim.Render(tokStr))
	}
	if durMs > 0 {
		durStr := fmt.Sprintf("%dms", durMs)
		if durMs >= 1000 {
			durStr = formatDuration(durMs)
		}
		rightParts = append(rightParts, StyleDim.Render(durStr))
	}
	rightSide := strings.Join(rightParts, "  ")

	var left string
	if summary != "" {
		left = cursor + indicator + " " + nameRendered + StyleDim.Render(" - ") + summaryRendered
	} else {
		left = cursor + indicator + " " + nameRendered
	}
	return spaceBetween(left, rightSide, width)
}

// renderDetailItemExpanded renders the expanded content for a detail item.
// Indented 4 spaces, word-wrapped to width-8.
func (m model) renderDetailItemExpanded(item displayItem, width int) rendered {
	wrapWidth := max(width-8, 20)
	indent := "    "

	var content string
	switch item.itemType {
	case parser.ItemThinking, parser.ItemOutput, parser.ItemTeammateMessage:
		text := strings.TrimSpace(item.text)
		if text == "" {
			return rendered{}
		}
		md := m.md.renderMarkdown(text, wrapWidth)
		content = indentBlock(md, indent)

	case parser.ItemSubagent:
		if item.subagentProcess != nil {
			content = m.renderSubagentTrace(item, wrapWidth, indent)
		} else {
			content = m.renderToolExpanded(item, wrapWidth, indent)
		}

	case parser.ItemToolCall:
		content = m.renderToolExpanded(item, wrapWidth, indent)
	}

	if content == "" {
		return rendered{}
	}
	return newRendered(content)
}

// renderToolExpanded renders the expanded content for a tool call item.
func (m model) renderToolExpanded(item displayItem, wrapWidth int, indent string) string {
	var sections []string

	if item.toolInput != "" {
		headerStyle := StyleSecondaryBold
		sections = append(sections, indent+headerStyle.Render("Input:"))
		inputStyle := StyleDim.
			Width(wrapWidth)
		sections = append(sections, indentBlock(inputStyle.Render(item.toolInput), indent))
	}

	if item.toolResult != "" || item.toolError {
		if len(sections) > 0 {
			sepStyle := StyleMuted
			sections = append(sections, indent+sepStyle.Render(strings.Repeat("-", wrapWidth)))
		}

		if item.toolError {
			headerStyle := StyleErrorBold
			sections = append(sections, indent+headerStyle.Render("Error:"))
		} else {
			headerStyle := StyleSecondaryBold
			sections = append(sections, indent+headerStyle.Render("Result:"))
		}

		resultStyle := StyleDim.
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
	dimStyle := StyleDim

	// Reuse the same trace-building logic used by the tree cursor navigation,
	// so both codepaths produce identical display items.
	traceItems := buildTraceItems(item)
	toolCount, msgCount := traceItemStats(traceItems)

	// Header: >_ Execution Trace · N tool calls, N messages
	traceIcon := IconSystem.WithColor(ColorTextDim) // terminal icon for execution context
	traceLabel := StylePrimaryBold.Render("Execution Trace")
	dot := dimStyle.Render(" " + IconDot.Glyph + " ")
	countStr := dimStyle.Render(fmt.Sprintf("%d tool calls, %d messages", toolCount, msgCount))

	var lines []string
	lines = append(lines, indent+traceIcon+"  "+traceLabel+dot+countStr)

	// All trace items as compact rows (no cursor, no cap).
	nestedWidth := wrapWidth - 4
	for i, di := range traceItems {
		row := m.renderDetailItemRow(di, i, -1, false, nestedWidth)
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
func (m model) renderDetailHeader(msg message, width int, leftSuffix ...string) rendered {
	headerIcon := IconClaude
	headerLabel := "Claude"
	if msg.subagentLabel != "" {
		headerIcon = IconSubagent
		headerLabel = msg.subagentLabel
	}
	icon := headerIcon.RenderBold()
	modelName := StylePrimaryBold.Render(headerLabel)
	modelVer := lipgloss.NewStyle().Foreground(modelColor(msg.model)).Render(msg.model)

	// Breadcrumb prefix when drilled into a subagent trace.
	var breadcrumb string
	if m.savedDetail != nil && m.traceMsg != nil {
		sep := StyleMuted.Render(" > ")
		breadcrumb = StyleDim.Render(m.savedDetail.label) + sep
	}

	left := breadcrumb + icon + " " + modelName + " " + modelVer + detailHeaderStats(msg)
	for _, s := range leftSuffix {
		left += " " + s
	}

	return newRendered(spaceBetween(left, detailHeaderMeta(msg), width))
}

// detailHeaderStats formats the stats summary (thinking, tool calls, messages, etc.).
func detailHeaderStats(msg message) string {
	var parts []string
	if msg.thinkingCount > 0 {
		parts = append(parts, fmt.Sprintf("%d thinking", msg.thinkingCount))
	}
	if msg.toolCallCount > 0 {
		tcLabel := "tool calls"
		if msg.toolCallCount == 1 {
			tcLabel = "tool call"
		}
		parts = append(parts, fmt.Sprintf("%d %s", msg.toolCallCount, tcLabel))
	}
	if msg.outputCount > 0 {
		label := "messages"
		if msg.outputCount == 1 {
			label = "message"
		}
		parts = append(parts, fmt.Sprintf("%d %s", msg.outputCount, label))
	}
	if msg.teammateSpawns > 0 {
		label := "teammates"
		if msg.teammateSpawns == 1 {
			label = "teammate"
		}
		parts = append(parts, fmt.Sprintf("%d %s", msg.teammateSpawns, label))
	}
	if msg.teammateMessages > 0 {
		label := "teammate messages"
		if msg.teammateMessages == 1 {
			label = "teammate message"
		}
		parts = append(parts, fmt.Sprintf("%d %s", msg.teammateMessages, label))
	}

	if len(parts) == 0 {
		return ""
	}
	dot := " " + IconDot.Render() + " "
	return dot + StyleSecondary.Render(strings.Join(parts, ", "))
}

// detailHeaderMeta formats the right-side metadata (tokens, duration, timestamp).
func detailHeaderMeta(msg message) string {
	var parts []string
	if msg.tokensRaw > 0 {
		parts = append(parts, IconToken.Render()+" "+StyleSecondary.Render(formatTokens(msg.tokensRaw)))
	}
	if msg.durationMs > 0 {
		parts = append(parts, IconClock.Render()+" "+StyleSecondary.Render(formatDuration(msg.durationMs)))
	}
	if msg.timestamp != "" {
		parts = append(parts, StyleDim.Render(msg.timestamp))
	}
	return strings.Join(parts, "  ")
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

// -- Viewport height ----------------------------------------------------------
// Named methods for the three viewport height formulas. Each includes a <= 0
// guard returning 1 so callers never divide by zero or produce negative slices.
//
// The -1 in list view accounts for the blank line between the last message and
// the status bar area. Picker's -2 accounts for the 2-line header.

// listViewHeight returns the visible content lines in the message list view.
func (m model) listViewHeight() int {
	h := m.height - statusBarHeight - m.activityIndicatorHeight() - 1
	if h <= 0 {
		return 1
	}
	return h
}

// detailViewHeight returns the visible content lines in the detail view.
func (m model) detailViewHeight() int {
	h := m.height - statusBarHeight - m.activityIndicatorHeight()
	if h <= 0 {
		return 1
	}
	return h
}

// pickerViewHeight returns the visible content lines in the session picker.
func (m model) pickerViewHeight() int {
	h := m.height - 2 - statusBarHeight
	if h <= 0 {
		return 1
	}
	return h
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
	keyStyle := StyleAccentBold

	descStyle := StyleDim

	sep := " " + IconDot.Render() + " "

	var hints []string

	if m.watching {
		tailLabel := StyleMuted.
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
