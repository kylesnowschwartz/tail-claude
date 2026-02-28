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
- **subagent.go** -- Subagent/teammate process discovery and linking across chunks (two discovery paths: `DiscoverSubagents` for `subagents/` files, `DiscoverTeamSessions` for project-dir team files)
- **summary.go** -- `Truncate` helper and per-tool one-line summary generation
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

### Rendering gotchas

- Terminal background (dark/light) must be detected in `main()` **before** Bubble Tea activates alt-screen -- detection inside alt-screen returns wrong results. Pass it to `newMdRenderer`.
- `mdRenderer` nils `Document.Color` so body text inherits the terminal's default foreground. Removing this makes text invisible on light backgrounds.
- `renderDetailContent` is the single source of truth for detail rendering -- both `viewDetail` and `computeDetailMaxScroll` call it. If you add a new render path, wire it through here or scroll math breaks.
- `layoutList` does one render pass and caches results (`listParts` + line offsets). Scroll math reads the cache. Don't render twice.

## Functional Thinking

Prefer pure functions that take inputs and return outputs. Push side effects to the edges.

- Parser functions are pure transformations. Keep them that way.
- `chunksToMessages`, `shortModel`, `formatTokens`, `formatDuration` are pure -- no model state.
- Bubble Tea's `Update` returns `(model, cmd)` -- treat it as a state reducer, not a mutation point.
- New features should follow the same pattern: parse/transform in `parser/`, display in the TUI layer.
- Avoid shared mutable state. The watcher communicates via channels, not shared structs.

## Session file format

Claude Code stores sessions at `~/.claude/projects/{encoded-project-path}/{session-uuid}.jsonl`.

### Path encoding

Claude Code encodes absolute paths by replacing three characters with `-`:

| Character | Example input | Encoded |
|-----------|--------------|---------|
| `/` (separator) | `/Users/kyle/Code/foo` | `-Users-kyle-Code-foo` |
| `.` (dot) | `/Users/kyle/.config/nvim` | `-Users-kyle--config-nvim` |
| `_` (underscore) | `/tmp/abc_def/proj` | `-tmp-abc-def-proj` |

The double-dash `--` pattern appears when `/` is immediately followed by `.` in the original path (each replaced independently). This is common in dotfile and worktree paths:

```
/Users/kyle/.claude/worktrees/wt  ->  -Users-kyle--claude-worktrees-wt
/Users/kyle/.worktrees/agent-foo  ->  -Users-kyle--worktrees-agent-foo
```

The encoding is **lossy** -- paths containing literal dashes can't be round-tripped. For authoritative path resolution, read the `cwd` field from session JSONL entries.

**Caveat**: some reference projects only replace `/` and `\`. The CLI uses the stricter three-character rule above. Verified empirically against 273 project directories on disk (zero contain literal dots or underscores).

Implementation: `parser/session.go:encodePath()`.

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

### Subagent session discovery

Subagent sessions appear in two locations depending on how they were spawned:

**Regular subagents** (Task without `team_name`): files in `{session}/subagents/agent-{agentId}.jsonl`. First entry has `isSidechain=true`, `agentId` matches filename. Parent links via `toolUseResult.agentId` (hex UUID).

**Team agents** (Task with `team_name` + `name`): standalone `.jsonl` files in the project directory. First entry has top-level `teamName` and `agentName` fields, `isSidechain=false`. Parent links via `toolUseResult.agent_id` in `"name@team"` format (e.g. `"planner@analysis"`).

Discovery and linking pipeline:

```
loadSession / readAndRebuild
  ├─ DiscoverSubagents(path)       → scans {session}/subagents/agent-*.jsonl
  ├─ DiscoverTeamSessions(path, chunks) → scans project dir for teamName/agentName matches
  │   └─ Sets ID = "agentName@teamName" to match parent's agent_id format
  ├─ allProcs = append(subagents, teamProcs...)
  └─ LinkSubagents(allProcs, chunks, path)
       ├─ Phase 1: agentId → tool_use_id (handles BOTH hex UUIDs AND name@team)
       ├─ Phase 2: TeamSummary == SubagentDesc (subagents/ team files only)
       └─ Phase 3: positional fallback (non-team only)
```

The render path checks `displayItem.subagentProcess != nil` to decide between showing an execution trace (drill-down with nested items) vs raw Task input/result text.

### Preview extraction rule

Process ALL `type=user` entries for session previews. Only skip command output and interruptions. Sanitize everything else. Commands are fallback. No isMeta/sidechain/teammate filtering.

## Development

```bash
just check    # go build ./... && go vet ./...
just test     # go test ./...
just run      # build and launch TUI
just race     # build with race detector
just dump     # render latest session to stdout
```

### CLI flags

```
tail-claude [flags] [session.jsonl]
  --dump          Print rendered output to stdout (no interactive TUI)
  --expand        Expand all messages (use with --dump)
  --width N       Set terminal width for --dump output (default 160, min 40)
  -h, --help      Show this help
```

## Conventions

- Conventional commits: `feat:`, `fix:`, `test:`, `chore:`
- Keep parser package free of TUI dependencies
- Test files live alongside source (`*_test.go`)
- Test fixtures in `parser/testdata/`
- No external dependencies beyond bubbletea/v2, lipgloss/v2, glamour, chroma/v2, colorprofile, fsnotify, and x/term
- Attribution for ported parsing logic documented in ATTRIBUTION.md
