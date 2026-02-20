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

### DisplayItem and DisplayItemType (`chunk.go`)

Structured elements within an AI chunk's detail view. Built during `mergeAIBuffer` from ContentBlocks.

Five item types:

- **ItemThinking** -- thinking block content.
- **ItemOutput** -- text output block.
- **ItemToolCall** -- tool invocation with matched result. Fields: `ToolName`, `ToolID`, `ToolInput`, `ToolSummary`, `ToolResult`, `ToolError`, `DurationMs`, `TokenCount`.
- **ItemSubagent** -- Task tool invocation (detected when `ToolName == "Task"`). Extra fields: `SubagentType`, `SubagentDesc`.
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
| `summary.go` | Per-tool one-line summaries, `Truncate` helper |
| `last_output.go` | Last visible output detection for collapsed view |

## Tests

Test files live alongside source (`*_test.go`). Fixtures in `parser/testdata/`:
- `minimal.jsonl` -- basic session for integration tests
- `noise.jsonl` -- noise filtering edge cases
