# parser package

Pure data transformation library. No TUI dependencies, no side effects except file IO in `ReadSession`/`ReadSessionIncremental`.

## Pipeline

```
JSONL bytes -> ParseEntry -> Classify -> BuildChunks -> []Chunk
```

Each stage is a pure function (or close to it). The TUI layer calls `chunksToMessages` to map `[]Chunk` into display structs.

## Types

### Entry (`entry.go`)

Raw JSONL deserialization. Fields map 1:1 to the on-disk format: `Type`, `UUID`, `Timestamp`, `IsSidechain`, `IsMeta`, and a nested `Message` struct with `Role`, `Content` (json.RawMessage), `Model`, `StopReason`, and `Usage`.

`ParseEntry(line []byte) (Entry, bool)` -- rejects invalid JSON and entries without a UUID.

### ClassifiedMsg (`classify.go`)

Sealed interface (unexported `classifiedMsg()` method). Five implementations:

- **UserMsg** -- genuine user input. Fields: `Timestamp`, `Text` (sanitized).
- **AIMsg** -- assistant responses and internal flow (tool results when `IsMeta=true`). Fields: `Timestamp`, `Model`, `Text`, `ThinkingCount`, `ToolCalls`, `Blocks` ([]ContentBlock), `Usage`, `StopReason`, `IsMeta`.
- **SystemMsg** -- command output (extracted from `<local-command-stdout>`/`<local-command-stderr>` XML). Fields: `Timestamp`, `Output`.
- **TeammateMsg** -- messages from teammate agents (detected by `<teammate-message>` XML wrapper). Fields: `Timestamp`, `Text`, `TeammateID`. Folded into AI buffer during chunk building, not a separate chunk type.
- **CompactMsg** -- context compression boundaries (`type=summary` entries). Fields: `Timestamp`, `Text`. Rendered as horizontal dividers.

### Supporting types (`classify.go`)

- **ContentBlock** -- a single block from an assistant or tool result message. `Type` is one of: `"thinking"`, `"text"`, `"tool_use"`, `"tool_result"`, `"teammate"`. Fields vary by type (`Text`, `ToolID`, `ToolName`, `ToolInput`, `Content`, `IsError`).
- **ToolCall** -- tool invocation reference: `ID`, `Name`.
- **Usage** -- token counts: `InputTokens`, `OutputTokens`, `CacheReadTokens`, `CacheCreationTokens`. Method `TotalTokens()` returns the sum.

### Chunk and ChunkType (`chunk.go`)

Output of the pipeline. Each `Chunk` is one visible unit in the conversation timeline.

Four chunk types: `UserChunk`, `AIChunk`, `SystemChunk`, `CompactChunk`.

AI chunks carry: `Model`, `Text`, `ThinkingCount`, `ToolCalls`, `Items` ([]DisplayItem), `Usage`, `StopReason`, `DurationMs`.

`Usage` is the **last non-meta assistant message's** context-window snapshot, not the sum of all messages. The Claude API reports `input_tokens` as the full context window per API call, so summing across tool-call round trips would overcount. Session-level totals (picker) are computed separately from raw entries in `scanSessionMetadata`.

### DisplayItem and DisplayItemType (`chunk.go`)

Structured elements within an AI chunk's detail view. Built during `mergeAIBuffer` from ContentBlocks.

Five item types:

- **ItemThinking** -- thinking block content.
- **ItemOutput** -- text output block.
- **ItemToolCall** -- tool invocation with matched result. Fields: `ToolName`, `ToolID`, `ToolInput`, `ToolSummary`, `ToolResult`, `ToolError`, `DurationMs`, `TokenCount`.
- **ItemSubagent** -- subagent spawner invocation (detected when `ToolName == "Task"` or `"Agent"`). Extra fields: `SubagentType`, `SubagentDesc`.
- **ItemTeammateMessage** -- teammate agent message. Extra field: `TeammateID`.

Tool results are matched to their originating `tool_use` via `ToolID`. Unmatched `tool_result` blocks become `ItemOutput`.

### LastOutput (`last_output.go`)

Represents the final visible output from an AI turn. Used by the TUI collapsed view to show "the answer."

`FindLastOutput(items []DisplayItem) *LastOutput` scans items in reverse:
1. Last `ItemOutput` with non-empty `Text` -> `LastOutputText`
2. Last `ItemToolCall` or `ItemSubagent` with non-empty `ToolResult` -> `LastOutputToolResult`
3. `nil` (no output found)

### SessionInfo (`session.go`)

Metadata for the session picker: `Path`, `SessionID`, `ModTime`, `FirstMessage` (preview), `MessageCount`.

## Key Invariants

- **No TUI imports.** The parser package depends only on stdlib + `encoding/json`. Keep it that way.
- **Sealed ClassifiedMsg.** The unexported `classifiedMsg()` method prevents external implementations. All message categories are handled by the five types above.
- **Noise filtering in Classify.** Three layers:
  1. `noiseEntryTypes` map: `system`, `file-history-snapshot`, `queue-operation`, `progress`
  2. `hardNoiseTags`: messages wrapped entirely in `<local-command-caveat>` or `<system-reminder>`
  3. Synthetic assistant messages: `model == "<synthetic>"`
  4. Empty stdout/stderr, interruption messages
  5. Sidechain messages (`IsSidechain=true`) are dropped unconditionally
- **AI buffer merging.** `BuildChunks` buffers consecutive `AIMsg` entries and flushes them into a single `AIChunk` when a `UserMsg` or `SystemMsg` appears (or at end of input). `TeammateMsg` folds into the buffer as a synthetic `AIMsg` with a `"teammate"` content block.
- **Tool result matching.** `mergeAIBuffer` tracks pending `tool_use` blocks by `ToolID`. When a `tool_result` block arrives in a meta message, it fills in `ToolResult`, `ToolError`, and `DurationMs` on the matching `DisplayItem`.
- **Classify is destructive.** `Classify` and `SanitizeContent` strip XML tags, attributes, and structural markers from raw entry content. Data that any downstream consumer needs (subagent metadata, session metadata, team summaries) must be extracted at the Entry layer -- either in `ParseEntry`, `ReadSession`/`ReadSessionIncremental`, or `readSubagentSession` -- before `Classify` runs. Never write a function that regexes chunk text for data that `Classify` strips. The `teammateSummaryRe` regex is applied in `readSubagentSession` on raw entry content, not on chunks, for exactly this reason.

## Subagent Discovery and Linking (`subagent.go`)

Two discovery paths find subagent sessions:

- **`DiscoverSubagents(sessionPath)`** -- scans `{session}/subagents/agent-*.jsonl`. Sets `ID` from the filename (hex UUID like `ab2c50e2c9d4dbf49`). Filters warmup, compact, and empty agents.
- **`DiscoverTeamSessions(sessionPath, parentChunks)`** -- scans the project directory for `.jsonl` files whose first entry has `teamName`/`agentName` fields matching team Task calls in the parent. Sets `ID = "agentName@teamName"` (e.g. `"planner@analysis"`) to match the `agent_id` format in the parent's `toolUseResult`.

Both return `[]SubagentProcess`. Callers merge them before linking:

```go
allProcs := append(subagents, teamProcs...)
colorMap := LinkSubagents(allProcs, chunks, path)
```

**`LinkSubagents`** connects processes to parent Task tool calls in three phases:
1. **Result-based** (Phase 1): `scanAgentLinks` maps `agentId` → `tool_use_id` from parent JSONL. Works for both hex UUIDs and `name@team` format IDs.
2. **Team summary** (Phase 2): matches `TeamSummary` attribute from `<teammate-message summary="...">` to `SubagentDesc`. For older team files in `subagents/`.
3. **Positional fallback** (Phase 3): remaining non-team processes matched to remaining non-team Task calls by order.

**`ReadTeamSessionMeta(path)`** -- cheap first-line-only read returning `(teamName, agentName)`. Used by `DiscoverTeamSessions` to identify team sessions without full parsing.

Key types:
- **SubagentProcess** -- parsed subagent with `ID`, `FilePath`, `Chunks`, timing, usage, and link metadata (`ParentTaskID`, `Description`, `SubagentType`, `TeamSummary`, `TeammateColor`).

## Tool Summary Coverage (`summary.go`)

`ToolSummary(name, input)` generates one-line summaries. Covered tools:

Read, Write, Edit, Bash, Grep, Glob, Task, LSP, WebFetch, WebSearch, TodoWrite, NotebookEdit, TaskCreate, TaskUpdate, SendMessage.

Unknown tools fall back to common parameter names (`name`, `path`, `file`, `query`, `command`), then first string value, then the tool name.

`Truncate(s, maxLen)` collapses newlines and truncates with ellipsis. Used across summaries and display strings.

## File Layout

| File | Responsibility |
|------|----------------|
| `entry.go` | JSONL line -> `Entry` struct |
| `classify.go` | `Entry` -> `ClassifiedMsg` (noise filtering, content block extraction) |
| `sanitize.go` | XML tag stripping, command display formatting, text extraction |
| `chunk.go` | `[]ClassifiedMsg` -> `[]Chunk` with `DisplayItem` building |
| `session.go` | File IO, session discovery, preview scanning |
| `subagent.go` | Subagent/team session discovery and linking (see below) |
| `summary.go` | Per-tool one-line summaries, `Truncate` helper |
| `last_output.go` | Last visible output detection for collapsed view |

## Tests

Test files live alongside source (`*_test.go`). Fixtures in `parser/testdata/`:
- `minimal.jsonl` -- basic session for integration tests
- `noise.jsonl` -- noise filtering edge cases
- `team-parent.jsonl` + `team-parent/subagents/` -- integration test for subagents/-based team agents (Phase 2 summary matching via `DiscoverSubagents` -> `ReadSession` -> `LinkSubagents`)
- `team-project/` -- integration test for project-dir team sessions (Phase 1 name@team matching via `DiscoverTeamSessions` -> `LinkSubagents`). Contains a parent session + two team session files + one unrelated session that must be skipped
- `test-session.jsonl` + `test-session/subagents/` -- regular subagent discovery (filtering warmup, compact, empty agents)
