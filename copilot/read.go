package copilot

import (
	"os"
	"strings"

	"github.com/kylesnowschwartz/agent-ouija/claude/transcript"
	"github.com/kylesnowschwartz/agent-ouija/jsonl"
)

// Session is the result of reading a Copilot events.jsonl file. The Reader
// retains classification state so the watcher can continue with subsequent
// incremental reads.
type Session struct {
	Msgs   []transcript.ClassifiedMsg
	Offset int64
	Reader *Reader
}

// ReadSession reads an events.jsonl file from the beginning.
func ReadSession(path string) (Session, error) {
	r := NewReader()
	msgs, offset, err := r.ReadIncremental(path, 0)
	return Session{Msgs: msgs, Offset: offset, Reader: r}, err
}

// ReadIncremental reads new lines from an events.jsonl file starting at the
// given byte offset. Returns newly classified messages, the updated offset,
// and any error. Mirrors ouija's ReadSessionIncremental, including the
// partial-last-line rule: an unterminated final line is KEPT when it already
// parses as complete JSON (a resident watcher may get no further file event
// for it); an unparseable tail is an append in progress and is excluded from
// the returned offset so the next read picks up the completed line intact.
// The caller (watcher) owns the truncation guard and must swap in a fresh
// Reader when it resets the offset to 0.
func (r *Reader) ReadIncremental(path string, offset int64) ([]transcript.ClassifiedMsg, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()

	if _, err := f.Seek(offset, 0); err != nil {
		return nil, offset, err
	}

	lr := jsonl.NewReader(f)

	var msgs []transcript.ClassifiedMsg
	for {
		line, ok := lr.Next()
		if !ok {
			break
		}
		if !lr.LastLineTerminated() {
			ev, ok := ParseEvent([]byte(line))
			if !ok {
				break
			}
			if msg, ok := r.ClassifyEvent(ev); ok {
				msgs = append(msgs, msg)
			}
			msgs = append(msgs, r.TakeDeferred()...)
			return msgs, offset + lr.BytesRead(), nil
		}
		ev, ok := ParseEvent([]byte(line))
		if !ok {
			continue
		}
		if msg, ok := r.ClassifyEvent(ev); ok {
			msgs = append(msgs, msg)
		}
		// Drain after every event, not just classified ones: a dropped
		// event (e.g. the compaction itself, or a turn boundary) can
		// release a deferred CompactMsg.
		msgs = append(msgs, r.TakeDeferred()...)
	}
	if err := lr.Err(); err != nil {
		return msgs, offset + lr.TerminatedBytesRead(), err
	}
	return msgs, offset + lr.TerminatedBytesRead(), nil
}

// BuildChunks folds classified messages into display chunks via
// transcript.BuildChunks, then rewrites tool summaries for Copilot tool
// names (ouija's ToolSummary switch only knows Claude names; the
// ToolCategory it computes is already correct for Copilot names).
func BuildChunks(msgs []transcript.ClassifiedMsg) []transcript.Chunk {
	chunks := transcript.BuildChunks(msgs)
	rewriteToolSummaries(chunks)
	return chunks
}

// IsConversationLine reports whether a raw events.jsonl line is a
// conversation message (user or assistant), for search filtering.
func IsConversationLine(line string) bool {
	return strings.Contains(line, `"type":"user.message"`) ||
		strings.Contains(line, `"type":"assistant.message"`)
}
