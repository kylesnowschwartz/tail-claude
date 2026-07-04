# Changelog

## Unreleased

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
