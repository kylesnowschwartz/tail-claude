# tail-claude

A terminal UI for reading Claude Code session JSONL files. Built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea).

Reads session logs from `~/.claude/` and renders them as a scrollable conversation with expandable tool calls, token counts, and live tailing.

<table>
<tr>
<td><img src="screenshots/picker.png" alt="Session picker" width="360" /></td>
<td><img src="screenshots/detail.png" alt="Detail view" width="360" /></td>
<td><img src="screenshots/conversation.png" alt="Conversation view" width="360" /></td>
</tr>
<tr>
<td align="center"><sub>Session picker</sub></td>
<td align="center"><sub>Detail view</sub></td>
<td align="center"><sub>Conversation view</sub></td>
</tr>
</table>

## Requirements

- Go 1.25+
- A [Nerd Font](https://www.nerdfonts.com/) patched terminal font

## Install

```bash
go install github.com/kylesnowschwartz/tail-claude@latest
```

Or build from source:

```bash
git clone git@github.com:kylesnowschwartz/tail-claude.git
cd tail-claude
go build -o tail-claude .
```

## Usage

Run `tail-claude` to open the session picker. Select a session to view it.

### Keybindings

| Key | Action |
|-----|--------|
| `j` / `k` | Move cursor down / up |
| `Enter` | Expand message or toggle detail view |
| `Esc` | Back to list / close |
| `p` | Open session picker |
| `e` | Expand all items in current message |
| `G` / `g` | Jump to bottom / top |
| `q` | Quit |

## Attribution

Parsing heuristics ported from [claude-devtools](https://github.com/matt1398/claude-devtools). See [ATTRIBUTION.md](ATTRIBUTION.md).

## License

[MIT](LICENSE)
