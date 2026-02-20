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

// -- Helpers ------------------------------------------------------------------

// chevron returns the expand/collapse indicator
func chevron(expanded bool) string {
	if expanded {
		return lipgloss.NewStyle().Foreground(ColorTextPrimary).Render(IconExpanded)
	}
	return lipgloss.NewStyle().Foreground(ColorTextDim).Render(IconCollapsed)
}

// selectionIndicator returns a left-margin marker for the selected message
func selectionIndicator(selected bool) string {
	if selected {
		return lipgloss.NewStyle().Foreground(ColorAccent).Render(IconSelected + " ")
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
// Subtracts border (2) + padding (4) and floors at 20.
func contentWidth(cardWidth int) int {
	w := cardWidth - 4
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
	iconStyle := lipgloss.NewStyle().Foreground(ColorSuccess)
	if lo.IsError {
		icon = IconToolErr
		iconStyle = lipgloss.NewStyle().Foreground(ColorError)
	}
	nameStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorTextPrimary)
	resultStyle := lipgloss.NewStyle().Foreground(ColorTextSecondary)

	result := lo.ToolResult
	if len(result) > 200 {
		result = result[:200] + "\u2026"
	}
	// Collapse newlines for single-line preview
	result = strings.ReplaceAll(result, "\n", " ")

	return iconStyle.Render(icon) + " " + nameStyle.Render(lo.ToolName) + " " + resultStyle.Render(truncate(result, 80))
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
					content = truncated + "\n" + fmt.Sprintf("\u2026 (%d lines hidden)", hidden)
				}
			case parser.LastOutputToolResult:
				content = formatToolResultPreview(msg.lastOutput)
			}
		} else {
			truncated, hidden := truncateLines(content, maxCollapsedLines)
			if hidden > 0 {
				content = truncated + "\n" + fmt.Sprintf("\u2026 (%d lines hidden)", hidden)
			}
		}
	}

	contentWidth := contentWidth(maxWidth)
	var body string
	if isExpanded && len(msg.items) > 0 {
		// Structured item rows instead of raw markdown
		var rows []string
		for i, item := range msg.items {
			rows = append(rows, m.renderDetailItemRow(item, i, -1, contentWidth))
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

	userIcon := lipgloss.NewStyle().
		Foreground(ColorTextSecondary).
		Render(IconUser)

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

	sysIcon := lipgloss.NewStyle().
		Foreground(ColorTextMuted).
		Render(IconSystem)

	label := lipgloss.NewStyle().
		Foreground(ColorTextSecondary).
		Render("System")

	ts := lipgloss.NewStyle().
		Foreground(ColorTextDim).
		Render(msg.timestamp)

	content := lipgloss.NewStyle().
		Foreground(ColorTextDim).
		Render(msg.content)

	return sel + sysIcon + " " + label + "  " + IconDot + "  " + ts + "  " + content
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
	left := strings.Repeat("─", leftPad)
	right := strings.Repeat("─", rightPad)
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
			" " + lipgloss.NewStyle().Foreground(ColorTextSecondary).Render(IconUser)
		body = m.md.renderMarkdown(msg.content, width-4)
	case RoleSystem:
		header = lipgloss.NewStyle().Foreground(ColorTextMuted).Render(IconSystem) +
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
	// Cursor indicator
	cursor := "  "
	if index == cursorIndex {
		cursor = lipgloss.NewStyle().Bold(true).Foreground(ColorAccent).Render(IconCursor + " ")
	}

	// Type indicator and name
	var indicator, name string
	dim := lipgloss.NewStyle().Foreground(ColorTextDim)
	green := lipgloss.NewStyle().Foreground(ColorSuccess)
	red := lipgloss.NewStyle().Foreground(ColorError)

	blue := lipgloss.NewStyle().Foreground(ColorInfo)

	switch item.itemType {
	case parser.ItemThinking:
		indicator = dim.Render(IconThinking)
		name = "Thinking"
	case parser.ItemOutput:
		indicator = blue.Render(IconOutput)
		name = "Output"
	case parser.ItemToolCall:
		if item.toolError {
			indicator = red.Render(IconToolErr)
		} else {
			indicator = green.Render(IconToolOk)
		}
		name = item.toolName
	case parser.ItemSubagent:
		indicator = blue.Render(IconSubagent)
		name = item.subagentType
		if name == "" {
			name = "Subagent"
		}
	case parser.ItemTeammateMessage:
		indicator = lipgloss.NewStyle().Foreground(ColorWarning).Render(IconTeammate)
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

	// Right-side: tokens + duration
	var rightParts []string
	if item.tokenCount > 0 {
		tokStr := fmt.Sprintf("~%s tok", formatTokens(item.tokenCount))
		rightParts = append(rightParts, lipgloss.NewStyle().Foreground(ColorTextDim).Render(tokStr))
	}
	if item.durationMs > 0 {
		durStr := fmt.Sprintf("%dms", item.durationMs)
		if item.durationMs >= 1000 {
			durStr = formatDuration(item.durationMs)
		}
		rightParts = append(rightParts, lipgloss.NewStyle().Foreground(ColorTextDim).Render(durStr))
	}
	rightSide := strings.Join(rightParts, "  ")

	left := cursor + indicator + " " + nameRendered + " " + summaryRendered
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

	case parser.ItemToolCall, parser.ItemSubagent:
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

	return ""
}

// renderDetailHeader renders metadata for the detail view header.
// An optional leftSuffix is appended after the stats (used for the chevron
// in list view). Matches the list view header layout for visual consistency.
func (m model) renderDetailHeader(msg message, width int, leftSuffix ...string) string {
	icon := lipgloss.NewStyle().Foreground(ColorInfo).Bold(true).Render(IconClaude)
	modelName := lipgloss.NewStyle().Bold(true).Foreground(ColorTextPrimary).Render("Claude")
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

	stats := ""
	if len(statParts) > 0 {
		dot := lipgloss.NewStyle().Foreground(ColorTextMuted).Render(" " + IconDot + " ")
		stats = dot + lipgloss.NewStyle().Foreground(ColorTextSecondary).Render(strings.Join(statParts, ", "))
	}

	left := icon + " " + modelName + " " + modelVer + stats
	for _, s := range leftSuffix {
		left += " " + s
	}

	// Right-side metadata
	var rightParts []string

	if msg.tokensRaw > 0 {
		coin := lipgloss.NewStyle().Foreground(ColorTokenIcon).Render(IconToken)
		rightParts = append(rightParts, coin+" "+lipgloss.NewStyle().
			Foreground(ColorTextSecondary).
			Render(formatTokens(msg.tokensRaw)))
	}

	if msg.durationMs > 0 {
		clock := lipgloss.NewStyle().Foreground(ColorTextDim).Render(IconClock)
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

// -- Status bar ---------------------------------------------------------------

// renderStatusBar renders key hints in a rounded-border box.
// When m.watching is true, a green LIVE badge is prepended.
func (m model) renderStatusBar(pairs ...string) string {
	keyStyle := lipgloss.NewStyle().
		Foreground(ColorAccent).
		Bold(true)

	descStyle := lipgloss.NewStyle().
		Foreground(ColorTextDim)

	sep := lipgloss.NewStyle().Foreground(ColorTextMuted).Render(" " + IconDot + " ")

	var hints []string

	if m.watching {
		liveBadge := lipgloss.NewStyle().
			Background(ColorLiveBg).
			Foreground(ColorLiveFg).
			Bold(true).
			Padding(0, 1).
			Render("LIVE")
		hints = append(hints, liveBadge)
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
