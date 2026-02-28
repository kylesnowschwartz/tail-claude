package parser

import (
	"bufio"
	"io"
)

const (
	// initialBufSize is the starting buffer capacity for the line reader.
	initialBufSize = 64 * 1024

	// maxLineSize is the maximum allowed line length. Lines exceeding this
	// are silently skipped rather than aborting the entire session.
	// 64 MiB accommodates even the largest Claude API responses.
	maxLineSize = 64 * 1024 * 1024
)

// lineReader reads JSONL files line by line, skipping lines that exceed
// maxLineSize rather than aborting. The buffer starts small and grows on
// demand. After iteration, call Err() to check for I/O errors (not EOF).
//
// Ported from agentsview's internal/parser/linereader.go with the addition
// of BytesRead() for incremental offset tracking.
type lineReader struct {
	r         *bufio.Reader
	maxLen    int // 0 means use maxLineSize constant
	buf       []byte
	err       error
	bytesRead int64
}

func newLineReader(r io.Reader) *lineReader {
	return &lineReader{
		r:   bufio.NewReaderSize(r, initialBufSize),
		buf: make([]byte, 0, initialBufSize),
	}
}

// next returns the next non-empty line (without trailing newline) and true,
// or ("", false) at EOF or I/O error. After the loop, call Err() to
// distinguish EOF from I/O failure.
func (lr *lineReader) next() (string, bool) {
	for {
		line, err := lr.readLine()
		if err != nil {
			if err != io.EOF {
				lr.err = err
			}
			return "", false
		}
		if line != "" {
			return line, true
		}
		// Empty line or skipped oversized line -- continue.
	}
}

// Err returns the first non-EOF I/O error encountered, or nil.
func (lr *lineReader) Err() error {
	return lr.err
}

// BytesRead returns the total bytes consumed from the reader, including
// skipped lines and newline delimiters. Used by ReadSessionIncremental
// for offset tracking during live tailing.
func (lr *lineReader) BytesRead() int64 {
	return lr.bytesRead
}

// readLine reads a full line, returning "" for blank/oversized lines and
// a non-nil error only at EOF or read failure.
//
// Uses bufio.Reader.ReadLine() which returns isPrefix=true for partial
// reads. When accumulated bytes exceed maxLineSize, the buffer is
// discarded and the rest of the line is consumed (to keep bytesRead
// accurate), then "" is returned so next() skips to the following line.
func (lr *lineReader) readLine() (string, error) {
	lr.buf = lr.buf[:0]
	oversized := false

	for {
		chunk, isPrefix, err := lr.r.ReadLine()
		// Count data bytes from every chunk, including oversized lines.
		lr.bytesRead += int64(len(chunk))

		if err != nil {
			if len(lr.buf) > 0 && err == io.EOF {
				// Final line data was accumulated in previous iterations.
				// No \n to count -- the line ended at EOF.
				break
			}
			return "", err
		}

		// bufio.ReadLine strips the \n delimiter but we still consumed it
		// from the underlying reader. Add +1 when the line is complete.
		//
		// Caveat: ReadLine can't distinguish "line ended with \n" from
		// "line ended at EOF without \n" -- both return (data, false, nil).
		// This means BytesRead may overcount by 1 on the final line if the
		// file lacks a trailing newline. That's harmless for JSONL tailing:
		// real entries always end with \n, and the overcount exactly skips
		// the \n that the next append will prepend.
		if !isPrefix {
			lr.bytesRead++
		}

		if oversized {
			if !isPrefix {
				return "", nil // done skipping
			}
			continue
		}

		lr.buf = append(lr.buf, chunk...)

		limit := maxLineSize
		if lr.maxLen > 0 {
			limit = lr.maxLen
		}
		if len(lr.buf) > limit {
			oversized = true
			lr.buf = lr.buf[:0]
			if !isPrefix {
				return "", nil
			}
			continue
		}

		if !isPrefix {
			break
		}
	}

	return string(lr.buf), nil
}
