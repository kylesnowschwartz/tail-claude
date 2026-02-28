package main

import (
	"bytes"
	"encoding/json"
	"os"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/colorprofile"
)

// jsonHL syntax-highlights JSON strings for terminal display.
// Mirrors mdRenderer: constructed once with hasDarkBg, caches chroma objects,
// exposes a single method.
type jsonHL struct {
	hasDarkBg bool
	lexer     chroma.Lexer
	formatter chroma.Formatter
	style     *chroma.Style
}

// newJSONHL creates a highlighter with the pre-detected background color
// and terminal color profile. Chroma objects are safe for reuse.
func newJSONHL(hasDarkBg bool) *jsonHL {
	lexer := chroma.Coalesce(lexers.Get("json"))

	styleName := "github"
	if hasDarkBg {
		styleName = "dracula"
	}
	style := styles.Get(styleName)

	profile := colorprofile.Detect(os.Stderr, os.Environ())
	formatterName := chromaFormatter(profile)
	formatter := formatters.Get(formatterName)

	return &jsonHL{
		hasDarkBg: hasDarkBg,
		lexer:     lexer,
		formatter: formatter,
		style:     style,
	}
}

// highlight detects JSON, pretty-prints if needed, and returns
// syntax-highlighted text. Returns ("", false) for non-JSON input
// so the caller can fall back to plain rendering.
func (h *jsonHL) highlight(s string) (string, bool) {
	raw := []byte(s)
	if !json.Valid(raw) {
		return "", false
	}

	// Normalize formatting (idempotent on already-indented input).
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return "", false
	}
	indented := buf.String()

	iterator, err := h.lexer.Tokenise(nil, indented)
	if err != nil {
		return "", false
	}

	var out bytes.Buffer
	if err := h.formatter.Format(&out, h.style, iterator); err != nil {
		return "", false
	}

	return out.String(), true
}

// chromaFormatter maps colorprofile profiles to chroma terminal formatter names.
func chromaFormatter(profile colorprofile.Profile) string {
	switch profile {
	case colorprofile.TrueColor:
		return "terminal16m"
	case colorprofile.ANSI256:
		return "terminal256"
	case colorprofile.ANSI:
		return "terminal16"
	default:
		return "terminal"
	}
}
