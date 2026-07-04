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
	r               *bufio.Reader
	maxLen          int // 0 means use maxLineSize constant
	buf             []byte
	err             error
	bytesRead       int64
	terminatedBytes int64 // bytes consumed through the last \n-terminated line
	lastTerminated  bool  // whether the most recently read line ended with \n
}

func newLineReader(r io.Reader) *lineReader {
	return &lineReader{
		r:   bufio.NewReaderSize(r, initialBufSize),
		buf: make([]byte, 0, initialBufSize),
	}
}

// ScanLines calls fn for each non-empty line of r, stopping early when fn
// returns false. Lines exceeding maxLineSize are skipped rather than
// aborting the scan — unlike bufio.Scanner, whose ErrTooLong permanently
// kills iteration, so one huge line (e.g. pasted image data) would hide
// every line after it. Returns the first I/O error encountered, or nil.
func ScanLines(r io.Reader, fn func(line string) bool) error {
	lr := newLineReader(r)
	for {
		line, ok := lr.next()
		if !ok {
			return lr.Err()
		}
		if !fn(line) {
			return nil
		}
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
// skipped lines, newline delimiters, and any EOF-truncated trailing line.
func (lr *lineReader) BytesRead() int64 {
	return lr.bytesRead
}

// TerminatedBytesRead returns the bytes consumed through the last
// newline-terminated line, excluding an EOF-truncated tail. Used by
// ReadSessionIncremental for offset tracking during live tailing: a
// half-written trailing line must be re-read intact on the next call.
func (lr *lineReader) TerminatedBytesRead() int64 {
	return lr.terminatedBytes
}

// LastLineTerminated reports whether the most recently returned line ended
// with a newline. False means the line was cut off at EOF -- during live
// tailing that is typically an append still in progress.
func (lr *lineReader) LastLineTerminated() bool {
	return lr.lastTerminated
}

// readLine reads a full line, returning "" for blank/oversized lines and
// a non-nil error only at EOF or read failure.
//
// Uses bufio.Reader.ReadSlice('\n') rather than ReadLine because ReadSlice
// reports whether the delimiter was actually found: a complete line returns
// err == nil, while an EOF-truncated final line (a JSONL append still in
// progress) returns io.EOF alongside the partial data. That distinction
// drives lastTerminated/terminatedBytes so incremental readers can exclude
// a half-written tail from their offset and re-read it intact later.
//
// When accumulated bytes exceed maxLineSize, the buffer is discarded and
// the rest of the line is consumed (to keep bytesRead accurate), then ""
// is returned so next() skips to the following line.
func (lr *lineReader) readLine() (string, error) {
	lr.buf = lr.buf[:0]
	oversized := false
	var lineBytes int64

	limit := maxLineSize
	if lr.maxLen > 0 {
		limit = lr.maxLen
	}

	for {
		chunk, err := lr.r.ReadSlice('\n')
		// Count data bytes from every chunk, including oversized lines.
		lineBytes += int64(len(chunk))

		switch err {
		case bufio.ErrBufferFull:
			// Partial read -- keep accumulating.
			if !oversized {
				lr.buf = append(lr.buf, chunk...)
				if len(lr.buf) > limit {
					oversized = true
					lr.buf = lr.buf[:0]
				}
			}
			continue

		case nil:
			// Delimiter found; chunk includes the trailing \n.
			lr.bytesRead += lineBytes
			lr.terminatedBytes = lr.bytesRead
			lr.lastTerminated = true
			if oversized {
				return "", nil // done skipping
			}
			data := chunk[:len(chunk)-1]
			if len(data) > 0 && data[len(data)-1] == '\r' {
				data = data[:len(data)-1]
			}
			lr.buf = append(lr.buf, data...)
			if len(lr.buf) > limit {
				return "", nil
			}
			return string(lr.buf), nil

		case io.EOF:
			if lineBytes == 0 {
				// Clean EOF at a line boundary.
				return "", io.EOF
			}
			// Final line ended at EOF without \n. Count the bytes but
			// leave terminatedBytes at the last complete line.
			lr.bytesRead += lineBytes
			lr.lastTerminated = false
			if oversized {
				return "", nil
			}
			lr.buf = append(lr.buf, chunk...)
			if len(lr.buf) > limit {
				return "", nil
			}
			return string(lr.buf), nil

		default:
			return "", err
		}
	}
}
