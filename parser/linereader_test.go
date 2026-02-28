package parser

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
)

func TestLineReader(t *testing.T) {
	tests := []struct {
		name string
		// maxLen overrides maxLineSize for testing. 0 means use a default
		// large enough that no test line triggers the limit.
		maxLen int
		input  string
		want   []string
	}{
		{
			name:   "normal lines",
			input:  "aaa\nbbb\nccc\n",
			maxLen: 100,
			want:   []string{"aaa", "bbb", "ccc"},
		},
		{
			name:   "skips oversized line",
			input:  "short\n" + strings.Repeat("x", 50) + "\nafter\n",
			maxLen: 30,
			want:   []string{"short", "after"},
		},
		{
			name:   "all lines oversized",
			input:  strings.Repeat("a", 50) + "\n" + strings.Repeat("b", 50) + "\n",
			maxLen: 30,
			want:   nil,
		},
		{
			name:   "empty input",
			input:  "",
			maxLen: 100,
			want:   nil,
		},
		{
			name:   "blank lines skipped",
			input:  "aaa\n\n\nbbb\n",
			maxLen: 100,
			want:   []string{"aaa", "bbb"},
		},
		{
			name:   "line without trailing newline",
			input:  "aaa\nbbb",
			maxLen: 100,
			want:   []string{"aaa", "bbb"},
		},
		{
			name:   "exact limit kept",
			input:  strings.Repeat("x", 30) + "\n",
			maxLen: 30,
			want:   []string{strings.Repeat("x", 30)},
		},
		{
			name:   "one over limit skipped",
			input:  strings.Repeat("x", 31) + "\n",
			maxLen: 30,
			want:   nil,
		},
		{
			name:   "mixed normal and oversized",
			input:  "first\n" + strings.Repeat("x", 50) + "\nmiddle\n" + strings.Repeat("y", 50) + "\nlast\n",
			maxLen: 30,
			want:   []string{"first", "middle", "last"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lr := newLineReaderWithMax(strings.NewReader(tt.input), tt.maxLen)
			var got []string
			for {
				line, ok := lr.next()
				if !ok {
					break
				}
				got = append(got, line)
			}
			if err := lr.Err(); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLineReaderIOError(t *testing.T) {
	ioErr := errors.New("disk read failed")
	r := io.MultiReader(
		strings.NewReader("aaa\nbbb\n"),
		iotest.ErrReader(ioErr),
	)

	lr := newLineReaderWithMax(r, 100)
	var got []string
	for {
		line, ok := lr.next()
		if !ok {
			break
		}
		got = append(got, line)
	}

	if len(got) != 2 {
		t.Fatalf("got %d lines, want 2: %v", len(got), got)
	}
	if lr.Err() == nil {
		t.Fatal("expected non-nil Err() after I/O failure")
	}
	if !errors.Is(lr.Err(), ioErr) {
		t.Fatalf("Err() = %v, want %v", lr.Err(), ioErr)
	}
}

func TestLineReaderBytesRead(t *testing.T) {
	input := "aaa\nbbb\nccc\n"
	lr := newLineReaderWithMax(strings.NewReader(input), 100)

	var lines []string
	for {
		line, ok := lr.next()
		if !ok {
			break
		}
		lines = append(lines, line)
	}

	if lr.Err() != nil {
		t.Fatalf("unexpected error: %v", lr.Err())
	}
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	// "aaa\n" = 4, "bbb\n" = 4, "ccc\n" = 4 = 12 total
	if lr.BytesRead() != int64(len(input)) {
		t.Errorf("BytesRead() = %d, want %d", lr.BytesRead(), len(input))
	}
}

func TestLineReaderBytesReadWithSkippedLines(t *testing.T) {
	oversized := strings.Repeat("x", 50)
	input := "short\n" + oversized + "\nafter\n"

	lr := newLineReaderWithMax(strings.NewReader(input), 30)

	var lines []string
	for {
		line, ok := lr.next()
		if !ok {
			break
		}
		lines = append(lines, line)
	}

	if lr.Err() != nil {
		t.Fatalf("unexpected error: %v", lr.Err())
	}
	want := []string{"short", "after"}
	if !slices.Equal(lines, want) {
		t.Errorf("got %q, want %q", lines, want)
	}
	// All bytes consumed including the oversized line and its newline.
	if lr.BytesRead() != int64(len(input)) {
		t.Errorf("BytesRead() = %d, want %d", lr.BytesRead(), len(input))
	}
}

func TestLineReaderBytesReadNoTrailingNewline(t *testing.T) {
	input := "aaa\nbbb"
	lr := newLineReaderWithMax(strings.NewReader(input), 100)

	for {
		_, ok := lr.next()
		if !ok {
			break
		}
	}

	if lr.Err() != nil {
		t.Fatalf("unexpected error: %v", lr.Err())
	}
	// "aaa\n" = 4 bytes, "bbb" = 3 bytes. bufio.ReadLine can't distinguish
	// "line ended with \n" from "line ended at EOF", so the final line
	// overcounts by 1 (adds +1 for a nonexistent \n). This matches the
	// old bufio.Scanner behavior and is harmless for JSONL files which
	// always end with \n.
	wantBytes := int64(len(input)) + 1
	if lr.BytesRead() != wantBytes {
		t.Errorf("BytesRead() = %d, want %d", lr.BytesRead(), wantBytes)
	}
}

// newLineReaderWithMax creates a lineReader with a custom max line size
// for testing. Production code uses newLineReader which defaults to maxLineSize.
func newLineReaderWithMax(r io.Reader, max int) *lineReader {
	lr := newLineReader(r)
	lr.maxLen = max
	return lr
}
