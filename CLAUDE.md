# tail-claude

A Go Bubble Tea TUI for reading Claude Code session JSONL files.

## Architecture

Pipeline-oriented data flow. Each stage transforms data and passes it forward:

```
JSONL line -> ParseEntry -> Classify -> BuildChunks -> chunksToMessages -> TUI
```

### Parser package (`parser/`)

Pure data transformation -- no side effects except file IO in `ReadSession` / `ReadSessionIncremental`.

- **entry.go** -- JSONL line to `Entry` struct (raw deserialization)
- **classify.go** -- `Entry` to `ClassifiedMsg` (sealed interface: `UserMsg`, `AIMsg`, `SystemMsg`, `TeammateMsg`, `CompactMsg`). Noise filtering lives here.
- **sanitize.go** -- XML tag stripping, command display formatting, text extraction from JSON content blocks
- **chunk.go** -- `[]ClassifiedMsg` to `[]Chunk`. Merges consecutive AI messages into single display units. `Chunk.Usage` is the last assistant message's context-window snapshot, not the sum.
- **session.go** -- File IO: `ReadSession` (full), `ReadSessionIncremental` (from offset), session discovery
- **last_output.go** -- `FindLastOutput`: extracts the final text or tool result from a chunk for collapsed preview
- **subagent.go** -- Subagent/teammate process discovery and linking across chunks
- **summary.go** -- Summary entry handling, `Ellipsis` and `Truncate` helpers
- **ongoing.go** -- Heuristics for whether a session is still in progress
- **dategroup.go** -- Date-based session grouping (Today, Yesterday, This Week, etc.)
- **patterns.go** -- Shared regex patterns for content classification

### TUI

Bubble Tea model with three view states: list, detail, picker.

- **main.go** -- Model struct, Init, View, entry point
- **update.go** -- Bubble Tea Update handler (key events, messages, state transitions)
- **convert.go** -- `chunksToMessages`, `convertDisplayItems` (parser -> TUI data bridge)
- **format.go** -- Pure formatters: `shortModel`, `formatTokens`, `formatDuration`, `modelColor`
- **render.go** -- All rendering functions
- **scroll.go** -- Scroll math: line offsets, cursor visibility, viewport calculations
- **visible_rows.go** -- Flat row list for detail view (parent + expanded subagent children)
- **watcher.go** -- fsnotify-based file watcher for live tailing
- **picker.go** -- Session discovery and selection UI
- **picker_watcher.go** -- Directory watcher for live picker updates (new/changed sessions)
- **markdown.go** -- Glamour-based markdown renderer with width-based caching
- **theme.go** -- AdaptiveColor definitions for dark/light terminal support
- **icons.go** -- Nerd Font icon constants

### Rendering (`render.go`, `markdown.go`, `theme.go`, `icons.go`)

`render.go` contains all rendering functions. The dispatch flow:

```
View() -> viewList/viewDetail/viewPicker
  viewList:   renderMessage -> renderClaudeMessage | renderUserMessage | renderSystemMessage | renderCompactMessage
  viewDetail: renderDetailContent -> renderDetailHeader + renderDetailItemsContent | markdown body
```

**List view rendering:**
- `renderMessage` dispatches by role to type-specific renderers.
- `renderClaudeMessage` renders a header (model, stats, tokens, duration, timestamp) + card body. Collapsed view uses `FindLastOutput` -- shows `LastOutputText` as markdown or `LastOutputToolResult` as a one-line summary. Expanded view with items shows structured `renderDetailItemRow` rows + truncated last output text at the bottom.
- `renderUserMessage` renders a right-aligned bubble with markdown content.
- `renderSystemMessage` renders a single inline line (icon + label + timestamp + content).
- `renderCompactMessage` renders a centered horizontal divider with text.

**Detail view rendering:**
- `renderDetailContent` is the single source of truth for detail content (used by both `viewDetail` and `computeDetailMaxScroll`).
- AI messages with items get `renderDetailItemsContent`: header + item rows with optional expanded content.
- `renderDetailItemRow` format: `{cursor} {indicator} {name:<12} {summary}  {tokens} {duration}`.
- `renderDetailItemExpanded` renders indented content -- markdown for thinking/output/teammate, input+separator+result for tool calls/subagents.

**Scroll computation:**
- `layoutList` renders every message once, caching both rendered content (`listParts`) and line-offset metadata. `viewList` assembles from the cache -- one render pass, one source of truth.
- `computeDetailMaxScroll` renders the detail content once to calculate total lines.
- `ensureDetailCursorVisible` counts header lines + item rows + expanded content to find the cursor's line position.

**Markdown renderer (`markdown.go`):**
- `mdRenderer` wraps glamour. Caches the renderer at a specific width; recreates when width changes.
- Terminal background (dark/light) detected once in `main()` before Bubble Tea alt-screen activation, passed to `newMdRenderer`.
- Document.Color is nilled so body text inherits terminal default foreground (avoids invisible text on light backgrounds).

**Layout constants (`render.go`):**
- `maxContentWidth = 120` -- content rendering width cap.
- `maxCollapsedLines = 12` -- collapsed message line limit before truncation.
- `statusBarHeight = 3` -- rounded border: top + content + bottom.

**Theme (`theme.go`):**
- All colors are `lipgloss.AdaptiveColor` with Light/Dark values.
- Text hierarchy: Primary, Secondary, Dim, Muted.
- Model family colors: Opus (red/coral), Sonnet (blue), Haiku (green).
- Accent colors: Accent (blue), Success (green), Warning (yellow), Error (red), Info (blue).

**Icons (`icons.go`):**
- Nerd Font codepoints. Requires a patched terminal font.
- Icon set: Claude, User, System, Expanded/Collapsed, Cursor, DrillDown, Thinking, Output, ToolOk/ToolErr, Subagent, Teammate, Selected, Token, Clock, Dot, Chat, Live, Ellipsis.

## Functional Thinking

Prefer pure functions that take inputs and return outputs. Push side effects to the edges.

- Parser functions are pure transformations. Keep them that way.
- `chunksToMessages`, `shortModel`, `formatTokens`, `formatDuration` are pure -- no model state.
- Bubble Tea's `Update` returns `(model, cmd)` -- treat it as a state reducer, not a mutation point.
- New features should follow the same pattern: parse/transform in `parser/`, display in the TUI layer.
- Avoid shared mutable state. The watcher communicates via channels, not shared structs.

## Session file format

Claude Code stores sessions at `~/.claude/projects/{encoded-project-path}/{session-uuid}.jsonl`.

Path encoding: `/Users/kyle/Code/foo` becomes `-Users-kyle-Code-foo`.

Each JSONL line is a JSON object with: `type`, `uuid`, `timestamp`, `isSidechain`, `isMeta`, `message` (with `role`, `content`, `model`, `usage`, `stop_reason`).

Content can be a JSON string (user messages) or JSON array of content blocks (assistant messages with text, thinking, tool_use blocks).

### Session entry types

Not all entries are conversation messages. Files may contain:
- `type=user` / `type=assistant` -- conversation messages
- `type=system` -- noise, filtered by Classify
- `type=summary` -- context compression boundaries, classified as `CompactMsg`
- `type=file-history-snapshot` -- internal bookkeeping, no conversation content ("ghost sessions")
- Teammate messages: `type=user` with `<teammate-message>` XML wrapper in content
- Meta entries: `isMeta=true` on user entries marks tool results, classified as `AIMsg`

### Preview extraction rule

Process ALL `type=user` entries for session previews. Only skip command output and interruptions. Sanitize everything else. Commands are fallback. No isMeta/sidechain/teammate filtering.

## Development

```bash
just check    # go build ./... && go vet ./...
just test     # go test ./...
just run      # build and launch TUI
just race     # build with race detector
```

## Conventions

- Conventional commits: `feat:`, `fix:`, `test:`, `chore:`
- Keep parser package free of TUI dependencies
- Test files live alongside source (`*_test.go`)
- Test fixtures in `parser/testdata/`
- No external dependencies beyond bubbletea, glamour, lipgloss, termenv, and fsnotify
- Attribution for ported parsing logic documented in ATTRIBUTION.md
