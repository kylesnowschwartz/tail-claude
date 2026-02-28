package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"strings"

	"github.com/kylesnowschwartz/tail-claude/parser"

	"charm.land/lipgloss/v2"
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
const maxContentWidth = 160

// maxCollapsedLines is the maximum content lines shown when a message is collapsed.
const maxCollapsedLines = 12

// keybindBarHeight is the rendered line count of the keybind hints bar
// (rounded border: top + content + bottom = 3 lines).
const keybindBarHeight = 3

// infoBarHeight returns the rendered line count of the session info bar.
// Colored modes (bypassPermissions, acceptEdits, plan) render a 3-line
// RoundedBorder chip; default mode renders a plain 1-line bar.
func (m model) infoBarHeight() int {
	switch m.sessionMode {
	case "bypassPermissions", "acceptEdits", "plan":
		return 3
	default:
		return 1
	}
}

// detailItemTokWidth is the fixed column width for token counts in the detail
// item row right side. Fits "~9.9k tok" (9 chars); right-aligns smaller values.
const detailItemTokWidth = 9

// detailItemDurWidth is the fixed column width for durations in the detail
// item row right side. Fits "999ms" and "1m 5s" (5 chars); left-aligns shorter values.
const detailItemDurWidth = 5

// beadCount is the number of dots in the activity indicator animation.
const beadCount = 5

// -- Helpers ------------------------------------------------------------------

// chevron returns the expand/collapse indicator
func chevron(expanded bool) string {
	if expanded {
		return Icon.Expanded.Render()
	}
	return Icon.Collapsed.Render()
}

// selectionIndicator returns a left-margin marker for the selected message
func selectionIndicator(selected bool) string {
	if selected {
		return Icon.Selected.Render() + " "
	}
	return "  "
}

// userHeaderLine renders "timestamp  You {icon}" used in both list and detail views.
func userHeaderLine(msg message) string {
	return StyleDim.Render(msg.timestamp) + "  " + StylePrimaryBold.Render("You") + " " + Icon.User.Render()
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

// centerBlock pads each line with a left gutter so content is visually centered
// within termWidth. No-op when content already fills the terminal.
func centerBlock(content string, contentWidth, termWidth int) string {
	gutter := (termWidth - contentWidth) / 2
	if gutter <= 0 {
		return content
	}
	pad := strings.Repeat(" ", gutter)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = pad + line
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
	icon := Icon.Tool.Ok
	if lo.IsError {
		icon = Icon.Tool.Err
	}
	nameStyle := StylePrimaryBold
	resultStyle := StyleSecondary

	result := lo.ToolResult
	if len(result) > 200 {
		result = result[:200] + Icon.Ellipsis.Glyph
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
	// Left-aligned with a right gutter for chat-bubble asymmetry.
	// Wide terminals (>= content cap): 3/4 width. Narrow: 7/8 to conserve space.
	fraction := 3 * containerWidth / 4
	if containerWidth < maxContentWidth {
		fraction = 7 * containerWidth / 8
	}
	maxWidth := fraction - 4 // minus selection indicator (2) + gutter (2)

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
	return headerLine + "\n" + indentBlock(card, sel+"  ")
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
			fmt.Sprintf("%s (%d lines hidden)", Icon.Ellipsis.Render(), hidden))
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
				fmt.Sprintf("%s %d more lines — Enter for full text", Icon.Ellipsis.Render(), hidden))
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

	// Right-align within the content column; centerBlock in viewList
	// handles centering the whole block within the terminal.
	alignWidth := containerWidth

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
			hint = StyleDim.Render(fmt.Sprintf("%s (%d lines hidden)", Icon.Ellipsis.Render(), hidden))
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
		Width(maxBubbleWidth)

	bubble := bubbleStyle.Render(rendered)

	// Right-align bubble within the space after the selection indicator
	selWidth := lipgloss.Width(sel)
	bubbleAlignWidth := alignWidth - selWidth
	if bubbleAlignWidth < maxBubbleWidth {
		bubbleAlignWidth = maxBubbleWidth
	}
	alignedBubble := lipgloss.PlaceHorizontal(bubbleAlignWidth, lipgloss.Right, bubble)

	// Prepend selection indicator to each bubble line
	return header + "\n" + indentBlock(alignedBubble, sel)
}

func renderSystemMessage(msg message, containerWidth int, isSelected, _ bool) string {
	// System messages always show inline -- they're short
	sel := selectionIndicator(isSelected)

	icon := Icon.System
	if msg.isError {
		icon = Icon.SystemErr
	}
	sysIcon := icon.Render()

	label := StyleSecondary.Render("System")

	ts := StyleDim.Render(msg.timestamp)

	content := StyleDim.Render(msg.content)

	line := sel + sysIcon + " " + label + "  " + Icon.Dot.Glyph + "  " + ts + "  " + content
	return "\n" + line + "\n"
}

func renderCompactMessage(msg message, width int) string {
	dim := StyleMuted
	text := msg.content
	if text == "" {
		text = "Context compressed"
	}
	textWidth := lipgloss.Width(text) + 2 // " text " with spacing
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
		header = Icon.System.Render() +
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
	traceIcon := Icon.System.WithColor(ColorTextDim)
	traceLabel := StylePrimaryBold.Render("Execution Trace")
	dot := dimStyle.Render(" " + Icon.Dot.Glyph + " ")
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
			cursor = Icon.DrillDown.RenderBold() + " "
		} else if isExpanded {
			cursor = Icon.Expanded.RenderBold() + " "
		} else {
			cursor = Icon.Collapsed.Render() + " "
		}
	}

	// Type indicator and name
	var indicator, name string

	switch item.itemType {
	case parser.ItemThinking:
		indicator = Icon.Thinking.Render()
		name = "Thinking"
	case parser.ItemOutput:
		indicator = Icon.Output.Render()
		name = "Output"
		if item.toolName != "" {
			name = item.toolName
		}
	case parser.ItemToolCall:
		indicator = toolCategoryIcon(item.toolCategory, item.toolError)
		name = item.toolName
	case parser.ItemSubagent:
		if item.teamColor != "" {
			indicator = Icon.Subagent.WithColor(teamColor(item.teamColor))
		} else {
			indicator = toolCategoryIcon(item.toolCategory, item.toolError)
		}
		// Team agents show member name ("file-counter"), others show type ("Explore").
		if item.teamMemberName != "" {
			name = item.teamMemberName
		} else {
			name = item.subagentType
		}
		if name == "" {
			name = "Subagent"
		}
	case parser.ItemTeammateMessage:
		if item.teamColor != "" {
			indicator = Icon.Teammate.WithColor(teamColor(item.teamColor))
		} else {
			indicator = Icon.Teammate.Render()
		}
		name = item.teammateID
		if name == "" {
			name = "Teammate"
		}
	}

	// Pad name to 12 chars
	nameStr := fmt.Sprintf("%-12s", name)
	var nameRendered string
	if item.teamColor != "" {
		nameRendered = lipgloss.NewStyle().Bold(true).Foreground(teamColor(item.teamColor)).Render(nameStr)
	} else {
		nameRendered = StylePrimaryBold.Render(nameStr)
	}

	// Ongoing spinner for subagent items: 1 glyph + 1 space, or 2 spaces for alignment.
	spinnerSlot := "  "
	if item.itemType == parser.ItemSubagent && item.subagentOngoing {
		frame := SpinnerFrames[m.animFrame%len(SpinnerFrames)]
		spinnerSlot = lipgloss.NewStyle().Foreground(ColorOngoing).Render(frame) + " "
	}

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
	// Suppress summary when it just repeats the tool name (common for MCP
	// tools with empty input, where summaryDefault returns the name).
	if summary == item.toolName {
		summary = ""
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
	// Build fixed-width right side so tok and dur columns align across all rows.
	// "%*s  %-*s": tok right-aligned in detailItemTokWidth, dur left-aligned in detailItemDurWidth.
	// Empty strings produce spaces, keeping the total width constant.
	tokStr := ""
	if tokCount > 0 {
		tokStr = fmt.Sprintf("~%s tok", formatTokens(tokCount))
	}
	durStr := ""
	if durMs >= 1000 {
		durStr = formatDuration(durMs)
	} else if durMs > 0 {
		durStr = "<1s"
	}
	var rightSide string
	if tokStr != "" || durStr != "" {
		tokPart := StyleDim.Render(fmt.Sprintf("%*s", detailItemTokWidth, tokStr))
		durPart := StyleDim.Render(fmt.Sprintf("%-*s", detailItemDurWidth, durStr))
		// When both present, prefix duration with a green dot separator.
		// The dot + space adds 2 visible chars; pad the else branch to match.
		if tokStr != "" && durStr != "" {
			dot := lipgloss.NewStyle().Foreground(ColorOngoing).Render(Icon.Dot.Glyph)
			rightSide = tokPart + "  " + dot + " " + durPart
		} else {
			rightSide = tokPart + "    " + durPart
		}
	}

	var left string
	if summary != "" {
		left = cursor + indicator + " " + nameRendered + spinnerSlot + StyleDim.Render("- ") + summaryRendered
	} else {
		left = cursor + indicator + " " + nameRendered + spinnerSlot
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
			content = m.renderTaskInput(item, wrapWidth, indent)
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
		sections = append(sections, indentBlock(
			m.highlightOrDim(item.toolInput, wrapWidth), indent))
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

		sections = append(sections, indentBlock(
			m.highlightOrDim(item.toolResult, wrapWidth), indent))
	}

	if len(sections) == 0 {
		return ""
	}
	return strings.Join(sections, "\n")
}

// highlightOrDim tries JSON syntax highlighting; falls back to dim text.
// Width wrapping is applied in both paths for consistent layout.
func (m model) highlightOrDim(text string, wrapWidth int) string {
	if highlighted, ok := m.jsonHL.highlight(text); ok {
		// lipgloss Width uses muesli/reflow which is ANSI-aware,
		// so wrapping chroma-highlighted text preserves escape codes.
		return lipgloss.NewStyle().Width(wrapWidth).Render(highlighted)
	}
	return StyleDim.Width(wrapWidth).Render(text)
}

// renderTaskInput renders structured metadata for a Task item without a linked
// subagent process. Extracts key fields from the JSON input and truncates the
// prompt field (which can be thousands of characters) instead of dumping raw JSON.
// Falls back to renderToolExpanded if JSON parsing fails.
func (m model) renderTaskInput(item displayItem, wrapWidth int, indent string) string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(item.toolInput), &fields); err != nil {
		return m.renderToolExpanded(item, wrapWidth, indent)
	}

	labelStyle := StyleSecondaryBold
	valueStyle := StyleDim

	var lines []string

	// Extract and render key metadata fields.
	for _, key := range []string{"description", "subagent_type", "team_name", "name", "model"} {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		var val string
		if json.Unmarshal(raw, &val) != nil || val == "" {
			continue
		}
		lines = append(lines, indent+labelStyle.Render(key+":")+
			" "+valueStyle.Render(val))
	}

	// Prompt: truncate to avoid wall-of-text.
	if raw, ok := fields["prompt"]; ok {
		var prompt string
		if json.Unmarshal(raw, &prompt) == nil && prompt != "" {
			const maxPrompt = 500
			if len(prompt) > maxPrompt {
				prompt = prompt[:maxPrompt] + Icon.Ellipsis.Glyph
			}
			// Collapse newlines for a compact preview.
			prompt = strings.ReplaceAll(prompt, "\n", " ")
			promptRendered := valueStyle.Width(wrapWidth).Render(prompt)
			lines = append(lines, indent+labelStyle.Render("prompt:")+
				" "+promptRendered)
		}
	}

	// Show the result if present (tool completed).
	if item.toolResult != "" || item.toolError {
		if len(lines) > 0 {
			sepStyle := StyleMuted
			lines = append(lines, indent+sepStyle.Render(strings.Repeat("-", wrapWidth)))
		}
		if item.toolError {
			lines = append(lines, indent+StyleErrorBold.Render("Error:"))
		} else {
			lines = append(lines, indent+labelStyle.Render("Result:"))
		}
		lines = append(lines, indentBlock(
			m.highlightOrDim(item.toolResult, wrapWidth), indent))
	}

	if len(lines) == 0 {
		return m.renderToolExpanded(item, wrapWidth, indent)
	}
	return strings.Join(lines, "\n")
}

// renderSubagentTrace renders an execution trace for a linked subagent,
// matching the claude-devtools layout: a header line with counts, followed
// by a flat list of all items across the subagent's chunks.
func (m model) renderSubagentTrace(item displayItem, wrapWidth int, indent string) string {
	traceItems := buildTraceItems(item)

	var lines []string
	lines = append(lines, indent+renderTraceHeader(item))

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
	headerIcon := Icon.Claude
	headerLabel := "Claude"
	if msg.subagentLabel != "" {
		headerIcon = Icon.Subagent
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

	left := breadcrumb + icon + " " + modelName + " " + modelVer + detailHeaderStats(msg) + subagentIcons(msg.items)
	for _, s := range leftSuffix {
		left += "  " + s
	}

	return newRendered(spaceBetween(left, detailHeaderMeta(msg), width))
}

// detailHeaderStats formats the stats summary using icons for compactness:
// 🧠2  󰯠9  💬4  instead of "2 thinking, 9 tool calls, 4 messages".
func detailHeaderStats(msg message) string {
	var parts []string
	if msg.thinkingCount > 0 {
		parts = append(parts, Icon.Thinking.Render()+" "+StyleSecondary.Render(fmt.Sprintf("%d", msg.thinkingCount)))
	}
	if msg.toolCallCount > 0 {
		parts = append(parts, Icon.Tool.Ok.Render()+" "+StyleSecondary.Render(fmt.Sprintf("%d", msg.toolCallCount)))
	}
	if msg.outputCount > 0 {
		parts = append(parts, Icon.Output.Render()+" "+StyleSecondary.Render(fmt.Sprintf("%d", msg.outputCount)))
	}
	if msg.teammateSpawns > 0 {
		parts = append(parts, Icon.Teammate.Render()+" "+StyleSecondary.Render(fmt.Sprintf("%d", msg.teammateSpawns)))
	}
	if msg.teammateMessages > 0 {
		parts = append(parts, Icon.Chat.Render()+" "+StyleSecondary.Render(fmt.Sprintf("%d", msg.teammateMessages)))
	}

	if len(parts) == 0 {
		return ""
	}
	dot := " " + Icon.Dot.Render() + " "
	return dot + strings.Join(parts, "  ")
}

// subagentIcons returns a colored bot icon for each subagent spawned in this
// message. Provides an at-a-glance count and identity of spawned agents.
func subagentIcons(items []displayItem) string {
	var icons []string
	for _, it := range items {
		if it.itemType == parser.ItemSubagent {
			icons = append(icons, Icon.Subagent.WithColor(teamColor(it.teamColor)))
		}
	}
	if len(icons) == 0 {
		return ""
	}
	return " " + strings.Join(icons, " ")
}

// detailHeaderMeta formats the right-side metadata (tokens, duration, timestamp).
func detailHeaderMeta(msg message) string {
	var parts []string
	if msg.tokensRaw > 0 {
		parts = append(parts, Icon.Token.Render()+" "+StyleSecondary.Render(formatTokens(msg.tokensRaw)))
	}
	if msg.durationMs > 0 {
		parts = append(parts, Icon.Clock.Render()+" "+StyleSecondary.Render(formatDuration(msg.durationMs)))
	}
	if msg.timestamp != "" {
		parts = append(parts, StyleDim.Render(msg.timestamp))
	}
	return strings.Join(parts, "  ")
}

// -- Debug log rendering ------------------------------------------------------

// debugLevelBadge returns a colored level label for a debug entry.
func debugLevelBadge(level parser.DebugLevel) string {
	switch level {
	case parser.LevelWarn:
		return lipgloss.NewStyle().Foreground(ColorContextWarn).Render("WARN ")
	case parser.LevelError:
		return lipgloss.NewStyle().Bold(true).Foreground(ColorError).Render("ERROR")
	default:
		return StyleDim.Render("DEBUG")
	}
}

// debugFilterLabel returns the human-readable label for the current filter.
func debugFilterLabel(level parser.DebugLevel) string {
	switch level {
	case parser.LevelWarn:
		return "warn+"
	case parser.LevelError:
		return "error"
	default:
		return "all"
	}
}

// viewDebugLog renders the debug log viewer.
func (m model) viewDebugLog() string {
	width := m.clampWidth()

	if len(m.debugFiltered) == 0 {
		empty := StyleDim.Render("No debug entries" + " (filter: " + debugFilterLabel(m.debugMinLevel) + ")")
		footer := m.renderFooter(
			"f", "filter:"+debugFilterLabel(m.debugMinLevel),
			"q/esc", "back",
			"?", "keys",
		)
		padding := strings.Repeat("\n", max(m.debugViewHeight()-1, 0))
		output := centerBlock(empty+padding, width, m.width)
		return output + "\n" + footer
	}

	// Render all visible lines.
	var lines []string
	for i, entry := range m.debugFiltered {
		isCursor := i == m.debugCursor
		lines = append(lines, m.renderDebugEntry(entry, i, isCursor, width))
		if m.debugExpanded[i] && entry.HasExtra() {
			// Expanded multi-line content, indented.
			extraLines := strings.Split(entry.Extra, "\n")
			for _, el := range extraLines {
				lines = append(lines, "  "+StyleDim.Render(el))
			}
		}
	}

	content := strings.Join(lines, "\n")
	allLines := strings.Split(content, "\n")
	totalLines := len(allLines)

	viewHeight := m.debugViewHeight()

	// Apply scroll offset.
	scroll := m.debugScroll
	maxScroll := totalLines - viewHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll > 0 && scroll < totalLines {
		allLines = allLines[scroll:]
	}
	if len(allLines) > viewHeight {
		allLines = allLines[:viewHeight]
	}
	for len(allLines) < viewHeight {
		allLines = append(allLines, "")
	}

	output := strings.Join(allLines, "\n")
	output = centerBlock(output, width, m.width)

	// Scroll position indicator
	scrollInfo := ""
	if totalLines > viewHeight && maxScroll > 0 {
		pct := scroll * 100 / maxScroll
		scrollInfo = fmt.Sprintf("  %d%%", pct)
	}

	// Footer
	footer := m.renderFooter(
		"j/k", "nav",
		"↑/↓", "scroll",
		"G/g", "jump",
		"tab", "expand",
		"f", "filter:"+debugFilterLabel(m.debugMinLevel),
		"q/esc", "back"+scrollInfo,
		"?", "keys",
	)

	return output + "\n" + footer
}

// renderDebugEntry renders a single collapsed debug entry line.
//
// Format: {cursor} HH:MM:SS.mmm  LEVEL  [category] message  [+N lines]  xN
// Category is inlined as a bracketed prefix on the message -- only present
// when the entry has one, so entries without a category waste no space.
func (m model) renderDebugEntry(entry parser.DebugEntry, index int, isCursor bool, width int) string {
	// Cursor indicator
	cursor := "  "
	if isCursor {
		if entry.HasExtra() {
			if m.debugExpanded[index] {
				cursor = Icon.Expanded.RenderBold() + " "
			} else {
				cursor = Icon.Collapsed.Render() + " "
			}
		} else {
			cursor = Icon.Selected.Render() + " "
		}
	}

	// Timestamp: HH:MM:SS.mmm (local time, dimmed)
	ts := entry.Timestamp.Local().Format("15:04:05.000")
	tsRendered := StyleDim.Render(ts)

	// Level badge
	level := debugLevelBadge(entry.Level)

	// Build suffixes (right side).
	var suffixes []string
	if entry.HasExtra() && !m.debugExpanded[index] {
		hint := fmt.Sprintf("[+%d lines]", entry.ExtraLineCount())
		suffixes = append(suffixes, StyleDim.Render(hint))
	}
	if entry.Count > 1 {
		countStr := fmt.Sprintf("x%d", entry.Count)
		suffixes = append(suffixes, StyleMuted.Render(countStr))
	}

	right := strings.Join(suffixes, " ")

	// Fixed prefix: cursor + timestamp + level.
	prefix := cursor + tsRendered + "  " + level + "  "
	prefixWidth := lipgloss.Width(prefix)
	rightWidth := lipgloss.Width(right)

	// Message with optional inline category prefix.
	msgSpace := width - prefixWidth - rightWidth - 2 // 2 for gap
	if msgSpace < 10 {
		msgSpace = 10
	}

	msg := entry.Message
	if entry.Category != "" {
		msg = "[" + entry.Category + "] " + msg
	}
	if lipgloss.Width(msg) > msgSpace {
		msg = parser.Truncate(msg, msgSpace)
	}

	// Style message based on level.
	var msgRendered string
	switch entry.Level {
	case parser.LevelError:
		msgRendered = lipgloss.NewStyle().Foreground(ColorError).Render(msg)
	case parser.LevelWarn:
		msgRendered = lipgloss.NewStyle().Foreground(ColorContextWarn).Render(msg)
	default:
		msgRendered = StyleDim.Render(msg)
	}

	leftPart := prefix + msgRendered
	if right != "" {
		return spaceBetween(leftPart, right, width)
	}
	return leftPart
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

// -- Footer height ------------------------------------------------------------

// footerHeight returns the total footer line count: info bar (always) +
// keybind hints (when showKeybinds is true).
func (m model) footerHeight() int {
	h := m.infoBarHeight()
	if m.showKeybinds {
		h += keybindBarHeight
	}
	return h
}

// -- Viewport height ----------------------------------------------------------
// Named methods for the three viewport height formulas. Each includes a <= 0
// guard returning 1 so callers never divide by zero or produce negative slices.
//
// The -1 in list view accounts for the blank line between the last message and
// the footer area. Picker's -2 accounts for the 2-line header.

// listViewHeight returns the visible content lines in the message list view.
func (m model) listViewHeight() int {
	h := m.height - m.footerHeight() - m.activityIndicatorHeight() - 1
	if h <= 0 {
		return 1
	}
	return h
}

// detailViewHeight returns the visible content lines in the detail view.
func (m model) detailViewHeight() int {
	h := m.height - m.footerHeight() - m.activityIndicatorHeight()
	if h <= 0 {
		return 1
	}
	return h
}

// pickerViewHeight returns the visible content lines in the session picker.
func (m model) pickerViewHeight() int {
	h := m.height - 2 - m.footerHeight()
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
	colors := []color.Color{
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

// -- Info bar -----------------------------------------------------------------

// renderModeBadge renders the permission mode as a 3-line RoundedBorder chip.
// Returns an empty string for default/unknown modes (caller falls back to plain text).
//
//	╭──────────╮
//	│ auto-edit │
//	╰──────────╯
func renderModeBadge(mode string) string {
	label := shortMode(mode)
	var clr color.Color
	switch mode {
	case "bypassPermissions":
		clr = ColorPillBypass
	case "acceptEdits":
		clr = ColorPillAcceptEdits
	case "plan":
		clr = ColorPillPlan
	default:
		return ""
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(clr).
		Foreground(clr).
		Padding(0, 1).
		Render(label)
}

// renderInfoBar renders the session metadata bar.
//
// When a colored mode badge is active the bar is 3 lines: a RoundedBorder chip
// on the left, with project/branch/context% vertically centered on the middle
// row beside it. Otherwise it collapses to a single line.
func (m model) renderInfoBar() string {
	sep := " " + Icon.Dot.Render() + " "

	// Build left metadata parts (path, branch).
	var leftParts []string
	if proj := shortPath(m.sessionCwd, m.sessionGitBranch); proj != "" {
		leftParts = append(leftParts, StyleSecondary.Render(proj))
	}
	if m.liveBranch != "" {
		branch := StyleDim.Render(m.liveBranch)
		if m.liveDirty {
			branch += lipgloss.NewStyle().Foreground(ColorContextWarn).Render("*")
		}
		leftParts = append(leftParts, branch)
	}

	// Context usage percentage (right-aligned).
	var rightStr string
	if pct := contextPercent(m.messages); pct >= 0 {
		var clr color.Color
		switch {
		case pct > 80:
			clr = ColorContextCrit
		case pct > 50:
			clr = ColorContextWarn
		default:
			clr = ColorContextOk
		}
		rightStr = lipgloss.NewStyle().Foreground(clr).Render(fmt.Sprintf("%d%% ctx", pct))
	}

	badge := renderModeBadge(m.sessionMode)

	if badge == "" {
		// No badge: single-line layout with mode as plain muted text.
		if m.sessionMode != "" {
			leftParts = append(leftParts, StyleMuted.Render(shortMode(m.sessionMode)))
		}
		leftStr := strings.Join(leftParts, sep)
		if rightStr != "" {
			return spaceBetween(" "+leftStr, rightStr+" ", m.width)
		}
		return " " + leftStr
	}

	// Badge active: 3-line layout.
	// Badge occupies the left column; metadata is a single line placed beside
	// it using JoinHorizontal(Center), which vertically centers the 1-line
	// string at the middle row of the 3-line badge.
	leftStr := strings.Join(leftParts, sep)
	badgeWidth := lipgloss.Width(badge)
	remainingWidth := m.width - badgeWidth
	var metaLine string
	if rightStr != "" {
		metaLine = spaceBetween(" "+leftStr, rightStr+" ", remainingWidth)
	} else {
		metaLine = " " + leftStr
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, badge, metaLine)
}

// renderFooter builds the complete footer: info bar + optional keybind hints.
func (m model) renderFooter(keybindPairs ...string) string {
	footer := m.renderInfoBar()
	if m.showKeybinds {
		footer += "\n" + m.renderKeybindBar(keybindPairs...)
	}
	return footer
}

// -- Status bar ---------------------------------------------------------------

// renderKeybindBar renders key hints in a rounded-border box.
// When m.watching is true, a dim "tail" indicator is prepended.
func (m model) renderKeybindBar(pairs ...string) string {
	keyStyle := StyleAccentBold

	descStyle := StyleDim

	sep := " " + Icon.Dot.Render() + " "

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

// -- Team task board ----------------------------------------------------------

// teamViewHeight returns the visible content lines in the team task board.
func (m model) teamViewHeight() int {
	h := m.height - m.footerHeight()
	if h <= 0 {
		return 1
	}
	return h
}

// viewTeamBoard renders the team task board view with scrolling and footer.
func (m model) viewTeamBoard() string {
	width := m.clampWidth()

	if len(m.teams) == 0 {
		empty := StyleDim.Render("No teams found")
		padding := strings.Repeat("\n", max(m.teamViewHeight()-1, 0))
		footer := m.renderFooter("q/esc", "back", "?", "keys")
		output := centerBlock(empty+padding, width, m.width)
		return output + "\n" + footer
	}

	content := m.renderTeamContent(width, m.animFrame)
	lines := strings.Split(content, "\n")
	totalLines := len(lines)
	viewHeight := m.teamViewHeight()

	// Scroll
	scroll := m.teamScroll
	maxScroll := totalLines - viewHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll > 0 && scroll < totalLines {
		lines = lines[scroll:]
	}
	if len(lines) > viewHeight {
		lines = lines[:viewHeight]
	}
	for len(lines) < viewHeight {
		lines = append(lines, "")
	}

	output := strings.Join(lines, "\n")
	output = centerBlock(output, width, m.width)

	// Scroll indicator
	scrollInfo := ""
	if totalLines > viewHeight && maxScroll > 0 {
		pct := scroll * 100 / maxScroll
		scrollInfo = fmt.Sprintf("  %d%%", pct)
	}

	footer := m.renderFooter(
		"j/k", "scroll",
		"↑/↓", "scroll",
		"G/g", "jump",
		"q/esc", "back"+scrollInfo,
		"?", "keys",
	)

	return output + "\n" + footer
}

// renderTeamContent renders all team sections joined by blank lines.
// Most recent team first (reverse order, since teams are appended chronologically).
func (m model) renderTeamContent(width, animFrame int) string {
	var sections []string
	for i := len(m.teams) - 1; i >= 0; i-- {
		team := m.teams[i]
		if team.Deleted {
			continue
		}
		sections = append(sections, renderTeamSection(team, width, animFrame))
	}
	if len(sections) == 0 {
		return StyleDim.Render("All teams deleted")
	}
	return strings.Join(sections, "\n\n")
}

// renderTeamSection renders a single team: divider, description, progress, members, task rows.
func renderTeamSection(team parser.TeamSnapshot, width, animFrame int) string {
	var lines []string

	// Divider: "── team-name ──────────────────────"
	lines = append(lines, renderTeamDivider(team.Name, width))

	// Description (if present)
	if team.Description != "" {
		lines = append(lines, StyleDim.Render(team.Description))
	}

	// Progress summary: "3 members · 1/3 done"
	lines = append(lines, renderTeamSummary(team))

	// Members row with colored names and ongoing spinners.
	if len(team.Members) > 0 {
		lines = append(lines, renderTeamMembers(team, animFrame))
	}

	// Blank line before tasks
	if len(team.Tasks) > 0 {
		lines = append(lines, "")
	}

	// Task rows
	for _, task := range team.Tasks {
		if task.Status == "deleted" {
			continue
		}
		lines = append(lines, renderTeamTaskRow(task, team, width, animFrame))
	}

	return strings.Join(lines, "\n")
}

// renderTeamSummary renders the progress summary line: "3 members · 2/5 done".
func renderTeamSummary(team parser.TeamSnapshot) string {
	var parts []string

	if len(team.Members) > 0 {
		parts = append(parts, fmt.Sprintf("%d members", len(team.Members)))
	}

	// Count completed and total (excluding deleted).
	total, completed := 0, 0
	for _, task := range team.Tasks {
		if task.Status == "deleted" {
			continue
		}
		total++
		if task.Status == "completed" {
			completed++
		}
	}
	if total > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d done", completed, total))
	}

	if len(parts) == 0 {
		return ""
	}
	return StyleDim.Render(strings.Join(parts, " · "))
}

// renderTeamMembers renders a row of colored member names with ongoing spinners.
func renderTeamMembers(team parser.TeamSnapshot, animFrame int) string {
	var parts []string
	for _, name := range team.Members {
		var rendered string

		// Spinner prefix for ongoing members.
		if team.MemberOngoing[name] {
			frame := SpinnerFrames[animFrame%len(SpinnerFrames)]
			rendered = lipgloss.NewStyle().Foreground(ColorOngoing).Render(frame) + " "
		}

		if colorName, ok := team.MemberColors[name]; ok && colorName != "" {
			rendered += lipgloss.NewStyle().Foreground(teamColor(colorName)).Render(name)
		} else {
			rendered += StyleDim.Render(name)
		}
		parts = append(parts, rendered)
	}
	return "  " + strings.Join(parts, "  ")
}

// renderTeamDivider renders a horizontal rule with the team name embedded.
// Format: "── team-name ──────────────────────"
func renderTeamDivider(name string, width int) string {
	prefix := GlyphHRule + GlyphHRule + " "
	suffix := " "
	nameWidth := lipgloss.Width(name) + lipgloss.Width(prefix) + lipgloss.Width(suffix)
	remaining := width - nameWidth
	if remaining < 3 {
		remaining = 3
	}
	rule := strings.Repeat(GlyphHRule, remaining)
	return StyleDim.Render(prefix) + StylePrimaryBold.Render(name) + StyleDim.Render(suffix+rule)
}

// renderTeamTaskRow renders a single task row.
// Format: "  #1  ✓  Fix shell hook anti-patterns         shell-hooks-worker"
// When the task's owner has an ongoing session, a spinner appears after the status glyph.
func renderTeamTaskRow(task parser.TeamTask, team parser.TeamSnapshot, width, animFrame int) string {
	// Status glyph
	status := taskStatusGlyph(task.Status)

	// Ongoing spinner for active workers: 1 glyph + 1 space, or 2 spaces for alignment.
	spinnerSlot := "  "
	if team.MemberOngoing[task.Owner] {
		frame := SpinnerFrames[animFrame%len(SpinnerFrames)]
		spinnerSlot = lipgloss.NewStyle().Foreground(ColorOngoing).Render(frame) + " "
	}

	// Task ID
	id := StyleDim.Render(fmt.Sprintf("#%-3s", task.ID))

	// Subject — takes remaining space minus owner
	ownerWidth := 0
	if task.Owner != "" {
		ownerWidth = len(task.Owner) + 2 // 2 for gap
	}
	subjectWidth := width - 16 - ownerWidth // 16 = indent(2) + id(4) + status(3) + spinner(2) + gaps(5)
	if subjectWidth < 10 {
		subjectWidth = 10
	}

	subject := task.Subject
	if lipgloss.Width(subject) > subjectWidth {
		subject = parser.Truncate(subject, subjectWidth)
	}
	subjectRendered := fmt.Sprintf("%-*s", subjectWidth, subject)

	// Owner (right-aligned, colored if team color available)
	ownerRendered := ""
	if task.Owner != "" {
		if colorName, ok := team.MemberColors[task.Owner]; ok && colorName != "" {
			ownerRendered = lipgloss.NewStyle().Foreground(teamColor(colorName)).Render(task.Owner)
		} else {
			ownerRendered = StyleDim.Render(task.Owner)
		}
	}

	left := "  " + id + "  " + status + spinnerSlot + subjectRendered
	if ownerRendered != "" {
		return spaceBetween(left, ownerRendered, width)
	}
	return left
}

// taskStatusGlyph maps a task status string to its colored icon.
func taskStatusGlyph(status string) string {
	switch status {
	case "completed":
		return Icon.Task.Done.Render()
	case "in_progress":
		return Icon.Task.Active.Render()
	default:
		return Icon.Task.Pending.Render()
	}
}
