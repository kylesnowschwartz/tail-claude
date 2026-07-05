# tail-claude

A Go Bubble Tea TUI for reading Claude Code session JSONL files.

## Architecture

Pipeline-oriented data flow. Each stage transforms data and passes it forward:

```
JSONL line -> ParseEntry -> Classify -> BuildChunks -> chunksToMessages -> TUI
```

### Parsing library (`github.com/kylesnowschwartz/agent-ouija`)

All parsing/discovery lives in the shared **agent-ouija** library (repo:
`~/Code/my-projects/agent-ouija`) — pure data transformation, no side effects
except file IO in `ReadSession` / `ReadSessionIncremental`. tail-claude imports:

- **`claude/transcript`** -- `Entry`/`ParseEntry` (raw deserialization), `Classify` (`Entry` to `ClassifiedMsg`, a sealed interface: `UserMsg`, `AIMsg`, `SystemMsg`, `TeammateMsg`, `CompactMsg`; noise filtering lives here), `BuildChunks` (`[]ClassifiedMsg` to `[]Chunk` -- merges consecutive AI messages; `Chunk.Usage` is the last assistant message's context-window snapshot, not the sum), `ReadSession`/`ReadSessionIncremental` (file IO), `FindLastOutput` (final text/tool result for collapsed preview), sanitization, ongoing heuristics
- **`claude/discover`** -- session discovery, titles, cache, date grouping (Today, Yesterday, ...), `ProjectName`
- **`claude/agents`** -- subagent/teammate discovery and linking (`DiscoverSubagents` for `subagents/` files, `DiscoverTeamSessions` for project-dir team files, `LinkSubagents`, `ScanWorkflowActivity`)
- **`claude/tools`** -- per-tool one-line summaries, `Truncate`/`ShortPath`
- **`claude/debuglog`** -- debug-log parsing and incremental reads
- **`claude/claudedir`** -- `Root` type: path encoding, project-dir resolution (see `claudepaths.go` for the app-side helpers that inject `claudedir.DefaultRoot()`)
- **`jsonl`**, **`gitroot`** -- line scanning (search), git main-worktree resolution

During migration the worktree builds against a local checkout via a
git-ignored `go.work`; the final release pins a tagged module version.

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
- **popup.go** -- Reusable modal confirmation overlay (ANSI-safe background splicing)
- **theme.go** -- AdaptiveColor definitions for dark/light terminal support
- **icons.go** -- Nerd Font icon constants

### Rendering gotchas

- Terminal background (dark/light) must be detected in `main()` **before** Bubble Tea activates alt-screen -- detection inside alt-screen returns wrong results. Pass it to `newMdRenderer`.
- `mdRenderer` nils `Document.Color` so body text inherits the terminal's default foreground. Removing this makes text invisible on light backgrounds.
- `renderDetailContent` is the single source of truth for detail rendering -- both `viewDetail` and `computeDetailMaxScroll` call it. If you add a new render path, wire it through here or scroll math breaks.
- `layoutList` does one render pass and caches results (`listParts` + line offsets). Scroll math reads the cache. Don't render twice.

## Functional Thinking

Prefer pure functions that take inputs and return outputs. Push side effects to the edges.

- Library parsing functions are pure transformations. Keep them that way.
- `chunksToMessages`, `shortModel`, `formatTokens`, `formatDuration` are pure -- no model state.
- Bubble Tea's `Update` returns `(model, cmd)` -- treat it as a state reducer, not a mutation point.
- New features should follow the same pattern: parse/transform in agent-ouija, display in the TUI layer.
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

Implementation: agent-ouija `claude/claudedir`, `EncodeProjectPath()`.

Each JSONL line is a JSON object with: `type`, `uuid`, `timestamp`, `isSidechain`, `isMeta`, `message` (with `role`, `content`, `model`, `usage`, `stop_reason`).

Content can be a JSON string (user messages) or JSON array of content blocks (assistant messages with text, thinking, tool_use blocks).

### Session entry types

Not all entries are conversation messages. Files may contain:

- `type=user` / `type=assistant` -- conversation messages
- `type=system` -- noise, filtered by Classify, **except** `subtype=compact_boundary` which becomes `CompactMsg` (Claude Code 2.1.18x+ compaction signal, with `compactMetadata {trigger, preTokens}`)
- `type=summary` -- legacy context compression boundaries (pre-2.1.18x files only), classified as `CompactMsg`
- `type=file-history-snapshot` -- internal bookkeeping, no conversation content ("ghost sessions")
- Session-metadata records (2.1.19x+, re-appended on flush/resume, last occurrence wins, no uuid): `last-prompt`, `custom-title`, `ai-title`, `agent-name`, `mode`, `permission-mode`, `pr-link`, `tag`, and friends. The picker reads titles/last-prompt from these in `scanSessionMetadata`; the chunk pipeline drops them (Classify's fallback is gated on `type=="user"`, so unknown future types drop too)
- Teammate messages: `type=user` with `<teammate-message>` XML wrapper in content
- Meta entries: `isMeta=true` on user entries marks tool results, classified as `AIMsg`

Thinking blocks from Opus 4.7+/Claude 5 models arrive with empty text (`{"thinking":"","signature":"..."}` — content encrypted into the signature by API default). The parser counts them but emits no block. Large tool results are externalized to `{session}/tool-results/{id}.txt` with a `<persisted-output>` placeholder; the parser splices them back in at the IO edge (agent-ouija `claude/transcript/persisted.go`).

### Subagent session discovery

Subagent sessions appear in two locations depending on how they were spawned:

**Regular subagents** (Task without `team_name`): files in `{session}/subagents/agent-{agentId}.jsonl`. First entry has `isSidechain=true`, `agentId` matches filename. Parent links via the `agent-{agentId}.meta.json` sidecar's `toolUseId` (2.1.19x+, exact and available from spawn time) or via `toolUseResult.agentId` (hex UUID).

**Workflow agents** (Workflow tool): transcripts in `{session}/subagents/workflows/wf_{runId}/` subdirectories. `ScanWorkflowActivity` (agent-ouija `claude/agents`) surfaces their presence — run/agent counts and last write drive the ongoing indicator and the info-bar "workflow running · N agents" badge. The watcher **polls** this scan (2s ticker) rather than fsnotify-watching the run dirs: macOS kqueue opens one fd per file in a watched directory, and workflow-heavy sessions hold hundreds of transcripts — enough to exhaust the fd budget and break select(2)-based terminal readers (FD_SETSIZE 1024). Transcript parsing/drill-down is a planned follow-up (the parent `Workflow` tool_use links via `toolUseResult.runId`; one call spawns many agents, which the current one-process-per-item UI can't represent).

**Team agents** (Task with `team_name` + `name`): standalone `.jsonl` files in the project directory. First entry has top-level `teamName` and `agentName` fields, `isSidechain=false`. Parent links via `toolUseResult.agent_id` in `"name@team"` format (e.g. `"planner@analysis"`).

Discovery and linking pipeline:

```
loadSession / readAndRebuild
  ├─ DiscoverSubagents(path)       → scans {session}/subagents/agent-*.jsonl
  ├─ DiscoverTeamSessions(path, chunks) → scans project dir for teamName/agentName matches
  │   └─ Sets ID = "agentName@teamName" to match parent's agent_id format
  ├─ allProcs = append(subagents, teamProcs...)
  └─ LinkSubagents(allProcs, chunks, path)
       ├─ Phase 0: sidecar meta.json toolUseId (pre-filled by DiscoverSubagents)
       ├─ Phase 1: agentId → tool_use_id (handles BOTH hex UUIDs AND name@team)
       ├─ Phase 2: TeamSummary == SubagentDesc (subagents/ team files only)
       └─ Phase 3: positional fallback (non-team, non-sidecar only)
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
  --update        Update to the latest version via go install
  -v, --version   Show version
  -h, --help      Show this help
```

## Conventions

- Conventional commits: `feat:`, `fix:`, `test:`, `chore:`
- Parsing logic belongs in agent-ouija, not the TUI layer; keep the library free of TUI dependencies
- Test files live alongside source (`*_test.go`)
- Parsing test fixtures live in agent-ouija (`claude/transcript/testdata/`)
- No external dependencies beyond agent-ouija, bubbletea/v2, lipgloss/v2, glamour, chroma/v2, colorprofile, fsnotify, x/term, x/ansi, and bubblezone/v2
- Attribution for ported parsing logic documented in ATTRIBUTION.md
