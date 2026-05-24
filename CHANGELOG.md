# Changelog

## Unreleased

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
