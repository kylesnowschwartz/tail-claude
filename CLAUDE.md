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

- **main.go** -- Model, Update, View, rendering functions, utility formatters
- **watcher.go** -- fsnotify-based file watcher for live tailing
- **picker.go** -- Session discovery and selection UI

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
