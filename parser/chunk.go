package parser

import (
	"strings"
	"time"
)

// ChunkType discriminates the three chunk categories.
type ChunkType int

const (
	UserChunk ChunkType = iota
	AIChunk
	SystemChunk
)

// Chunk is the output of the pipeline. Each chunk represents one visible unit
// in the conversation timeline.
type Chunk struct {
	Type      ChunkType
	Timestamp time.Time

	// User chunk fields.
	UserText  string
	IsSlash   bool
	SlashName string

	// AI chunk fields.
	Model      string
	Text       string
	Thinking   int
	ToolCalls  []ToolCall
	Usage      Usage
	StopReason string
	DurationMs int64 // first to last message timestamp in chunk

	// System chunk fields.
	Output string
}

// BuildChunks folds classified messages into display chunks.
// The algorithm buffers consecutive AI messages and flushes them into a single
// AI chunk whenever a User or System message appears (or at end of input).
func BuildChunks(msgs []ClassifiedMsg) []Chunk {
	var chunks []Chunk
	var aiBuf []AIMsg

	flush := func() {
		if len(aiBuf) == 0 {
			return
		}
		chunks = append(chunks, mergeAIBuffer(aiBuf))
		aiBuf = aiBuf[:0]
	}

	for _, msg := range msgs {
		switch m := msg.(type) {
		case UserMsg:
			flush()
			chunks = append(chunks, Chunk{
				Type:      UserChunk,
				Timestamp: m.Timestamp,
				UserText:  m.Text,
				IsSlash:   m.IsSlash,
				SlashName: m.SlashName,
			})
		case SystemMsg:
			flush()
			chunks = append(chunks, Chunk{
				Type:      SystemChunk,
				Timestamp: m.Timestamp,
				Output:    m.Output,
			})
		case AIMsg:
			aiBuf = append(aiBuf, m)
		}
	}
	flush()

	return chunks
}

// mergeAIBuffer collapses a buffer of consecutive AI messages into one AI chunk.
func mergeAIBuffer(buf []AIMsg) Chunk {
	var (
		texts     []string
		thinking  int
		toolCalls []ToolCall
		usage     Usage
		model     string
		stop      string
	)

	for _, m := range buf {
		if m.Text != "" {
			texts = append(texts, m.Text)
		}
		thinking += m.Thinking
		toolCalls = append(toolCalls, m.ToolCalls...)
		usage.InputTokens += m.Usage.InputTokens
		usage.OutputTokens += m.Usage.OutputTokens
		usage.CacheReadTokens += m.Usage.CacheReadTokens
		usage.CacheCreationTokens += m.Usage.CacheCreationTokens

		// Model from first assistant-type (non-meta) message.
		if model == "" && !m.IsMeta && m.Model != "" {
			model = m.Model
		}
		// StopReason from last assistant-type (non-meta) message.
		if !m.IsMeta && m.StopReason != "" {
			stop = m.StopReason
		}
	}

	first := buf[0].Timestamp
	last := buf[len(buf)-1].Timestamp
	dur := last.Sub(first).Milliseconds()

	return Chunk{
		Type:       AIChunk,
		Timestamp:  first,
		Model:      model,
		Text:       strings.Join(texts, "\n"),
		Thinking:   thinking,
		ToolCalls:  toolCalls,
		Usage:      usage,
		StopReason: stop,
		DurationMs: dur,
	}
}
