# Changelog

## Unreleased

## v0.17.0 (2026-07-06)

- Show real session names in the picker: overlay live-registry names (`~/.claude/sessions/{pid}.json`), the only place a `/rename` survives Claude Code's custom-title re-stamping; arbitration is custom title > AI title > registry name
- Resolve registry names as `tail-claude <name>` arguments alongside transcript titles; exact matches beat substring matches across both sources
- Route all picker session-list builds through a single discovery path so watcher rescans keep the name overlay
- Bump agent-ouija to v1.2.0 and consume its display-name API (`claude.NameResolver`, `claude.FindNameMatches`)

## v0.16.0 (2026-07-05)

## v0.15.0 (2026-07-05)

## v0.14.2 (2026-07-04)

- Fix file-descriptor exhaustion when tailing workflow-heavy sessions: workflow activity is now polled instead of fsnotify-watched (macOS kqueue opens one fd per file in a watched directory, crashing the TUI past 1024)

## v0.14.1 (2026-07-04)

- Add expand chevron to picker session rows, signalling the tab-expandable first/last prompt preview

## v0.14.0 (2026-07-04)

Quality release: a full-codebase bug, performance, and maintainability hunt — 65 findings confirmed by cross-model audit, all applied — plus UX refinements from hands-on QA.

- Fix session content search missing assistant messages; debounce scans and guard cached results against stale queries
- Fix incremental reads dropping the trailing line while a session file is mid-write
- Fix subagent durations measured short, team sessions linked to the wrong parent run, and team task updates applied out of order
- Fix stale tail updates bleeding into a newly switched session; stop leaking picker refresh goroutines
- Fix Bash tool results dropping stderr when stdout is present
- Fix rune-unsafe truncation and match highlighting on non-ASCII text
- Fix footer height drift, search-result scrolling past the viewport, duration rollover past 24h, and watcher errors vanishing silently
- Show the expand caret only on detail rows that actually have expandable content
- Show a red "dev-mode" footer label on local (non-release) builds
- Color the Fable/Mythos model family violet; handle model ids without a minor version (e.g. sonnet-5)
- Performance: skip full re-renders on cursor moves and idle ticks, cache markdown renderers per width, render only visible debug entries, decode assistant content once during classification

## v0.13.0 (2026-07-04)

- Adapt parser to Claude Code 2.1.19x session format (compact boundaries, per-iteration usage, session-metadata entries)
- Show compaction dividers with trigger and pre-compaction token count (e.g. "Conversation compacted (auto, 165k tokens)")
- Resolve externalized tool results from `tool-results/*.txt` companion files (256KB cap, UTF-8-safe truncation)
- Count thinking blocks whose content the API omits (Opus 4.7+/Claude 5) so thought badges and ongoing detection stay accurate
- Link subagents via `agent-*.meta.json` sidecars for exact parent-task matching; harden team session discovery
- Surface background Workflow runs: ongoing spinner stays alive and info bar shows "workflow running · N agents"
- Recognize Fable/Mythos model names with dedicated color; categorize Agent/Skill/Workflow tools alongside Task
- Recover from in-place session file rewrites that shrink the file under the watcher's read offset

## v0.12.0 (2026-05-24)

## v0.11.1 (2026-04-20)

- Fix detail-view subagent spinners stalling to ~5s cadence
- Fix picker session spinners only advancing when the session is actively writing

## v0.11.0 (2026-04-18)

## v0.10.0 (2026-04-16)

## v0.9.0 (2026-04-12)

## v0.8.0 (2026-04-12)

## v0.7.0 (2026-04-11)

## v0.6.0 (2026-04-04)

## v0.5.0 (2026-03-27)

## v0.4.0 (2026-03-26)

- Add `--version`/`-v` flag (resolves #2)
- Add `y` to copy transcript path from picker
- Add `D` to delete sessions with confirmation popup overlay
- Add clickable fingerprint icon to copy session UUID
- Add clickable `?` help icon in footer across all views
- Add mouse scroll and click support in picker via bubblezone
- Show raw context tokens (e.g. "162.3k ctx") instead of inferred percentage
- Fix post-compaction context display showing pre-compaction peak
- Hide context tokens on picker (session-specific, not meaningful there)
- Display model name on subagent rows
