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
- **classify.go** -- `Entry` to `ClassifiedMsg` (sealed interface: `UserMsg`, `AIMsg`, `SystemMsg`). Noise filtering lives here.
- **sanitize.go** -- XML tag stripping, command display formatting, text extraction from JSON content blocks
- **chunk.go** -- `[]ClassifiedMsg` to `[]Chunk`. Merges consecutive AI messages into single display units.
- **session.go** -- File IO: `ReadSession` (full), `ReadSessionIncremental` (from offset), session discovery

### TUI (`main.go`, `watcher.go`, `picker.go`)

Bubble Tea model with three view states: list, detail, picker.

- **main.go** -- Model, Update, View, `chunksToMessages`, `convertDisplayItems`, utility formatters
- **render.go** -- All rendering functions (extracted from main.go)
- **watcher.go** -- fsnotify-based file watcher for live tailing
- **picker.go** -- Session discovery and selection UI
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
- `computeLineOffsets` pre-renders every message to calculate per-message line counts. Used by `ensureCursorVisible` to keep the cursor in the viewport.
- `computeDetailMaxScroll` renders the detail content once to calculate total lines.
- `ensureDetailCursorVisible` counts header lines + item rows + expanded content to find the cursor's line position.

**Markdown renderer (`markdown.go`):**
- `mdRenderer` wraps glamour. Caches the renderer at a specific width; recreates when width changes.
- Terminal background (dark/light) detected once in `main()` before Bubble Tea alt-screen activation, passed to `newMdRenderer`.
- Document.Color is nilled so body text inherits terminal default foreground (avoids invisible text on light backgrounds).

**Layout constants (`render.go`):**
- `maxContentWidth = 120` -- content rendering width cap.
- `maxCollapsedLines = 6` -- collapsed message line limit before truncation.
- `statusBarHeight = 3` -- rounded border: top + content + bottom.

**Theme (`theme.go`):**
- All colors are `lipgloss.AdaptiveColor` with Light/Dark values.
- Text hierarchy: Primary, Secondary, Dim, Muted.
- Model family colors: Opus (red/coral), Sonnet (blue), Haiku (green).
- Accent colors: Accent (blue), Success (green), Warning (yellow), Error (red), Info (blue).

**Icons (`icons.go`):**
- Nerd Font codepoints. Requires a patched terminal font.
- Icon set: Claude, User, System, Expanded/Collapsed, Thinking, Output, ToolOk/ToolErr, Subagent, Teammate, Clock, Token, Cursor, Dot, Selected.

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
- `type=system` / `type=summary` -- noise, filtered by Classify
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
- No external dependencies beyond bubbletea, lipgloss, and fsnotify
