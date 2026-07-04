package parser

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Claude Code 2.1.19x+ externalizes large tool results: the tool_result
// content block is replaced with a placeholder like
//
//	<persisted-output>
//	Output too large (820.5KB). Full output saved to: /path/to/{session}/tool-results/{id}.txt
//
// and the real output lives in the referenced companion file. Without
// resolution, tail-claude renders the placeholder instead of the output.
//
// Resolution happens here — at the file-IO edge, after Classify — because
// Classify must stay a pure transformation.

const persistedOutputTag = "<persisted-output>"

// maxPersistedRead caps how much of a companion file is inlined. Persisted
// outputs can be hundreds of KB; the detail view truncates for display, but
// holding multi-MB strings per tool call is waste. Oversize files keep a
// pointer to the original.
const maxPersistedRead = 256 * 1024

var persistedPathRe = regexp.MustCompile(`Full output saved to:\s*(.+)`)

// resolvePersistedOutputs replaces persisted-output placeholders in
// tool_result blocks with the contents of their companion files. Mutates the
// AIMsg blocks in place. trustedRoot bounds which files may be read: a
// placeholder path outside it is left as-is (the placeholder text still
// names the file, so nothing is lost — just not inlined).
//
// For a main session, trustedRoot is the project directory (companion files
// live at {projectDir}/{sessionUUID}/tool-results/). For a subagent file at
// {projectDir}/{session}/subagents/agent-x.jsonl, it's the same project
// directory — subagents share the parent session's tool-results dir.
func resolvePersistedOutputs(msgs []ClassifiedMsg, trustedRoot string) {
	if trustedRoot == "" {
		return
	}
	for _, m := range msgs {
		ai, ok := m.(AIMsg)
		if !ok || !ai.IsMeta {
			continue
		}
		// ai is a copy, but Blocks is a slice whose backing array is shared
		// with the message stored in msgs — mutating elements is enough.
		for j := range ai.Blocks {
			b := &ai.Blocks[j]
			if b.Type != "tool_result" || !strings.Contains(b.Content, persistedOutputTag) {
				continue
			}
			if resolved, ok := readPersistedOutput(b.Content, trustedRoot); ok {
				b.Content = resolved
			}
		}
	}
}

// readPersistedOutput extracts the companion-file path from placeholder text
// and returns the file's contents. Returns ("", false) when the path is
// missing, escapes trustedRoot, or can't be read.
func readPersistedOutput(placeholder, trustedRoot string) (string, bool) {
	m := persistedPathRe.FindStringSubmatch(placeholder)
	if m == nil {
		return "", false
	}
	path := filepath.Clean(strings.TrimSpace(m[1]))
	if !pathWithinDir(path, trustedRoot) {
		return "", false
	}

	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()

	buf, err := io.ReadAll(io.LimitReader(f, maxPersistedRead+1))
	if err != nil || len(buf) == 0 {
		return "", false
	}
	if len(buf) > maxPersistedRead {
		// Back up to a rune boundary so the cut never emits invalid UTF-8.
		cut := maxPersistedRead
		for cut > 0 && !utf8.RuneStart(buf[cut]) {
			cut--
		}
		return string(buf[:cut]) +
			fmt.Sprintf("\n… (truncated at %dKB; full output: %s)", maxPersistedRead/1024, path), true
	}
	return string(buf), true
}

// pathWithinDir reports whether path is inside dir (or equal to it).
// Both are cleaned; relies on lexical containment, which is sufficient here
// because the candidate path comes from Claude Code's own placeholder text.
func pathWithinDir(path, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
