package parser

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Noise tag patterns - system-generated metadata stripped from display content.
var noiseTagPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)<local-command-caveat>.*?</local-command-caveat>`),
	regexp.MustCompile(`(?is)<system-reminder>.*?</system-reminder>`),
}

// Command tag patterns - removed after extracting display form.
var commandTagPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)<command-name>.*?</command-name>`),
	regexp.MustCompile(`(?is)<command-message>.*?</command-message>`),
	regexp.MustCompile(`(?is)<command-args>.*?</command-args>`),
}

// SanitizeContent removes noise XML tags and converts command tags into
// a human-readable slash command format for display.
func SanitizeContent(s string) string {
	// Command output messages: extract the inner content.
	if IsCommandOutput(s) {
		if out := ExtractCommandOutput(s); out != "" {
			return out
		}
	}

	// Command messages: convert to "/name args" form.
	if strings.HasPrefix(s, "<command-name>") || strings.HasPrefix(s, "<command-message>") {
		if display := extractCommandDisplay(s); display != "" {
			return display
		}
	}

	// Strip noise tags.
	result := s
	for _, pat := range noiseTagPatterns {
		result = pat.ReplaceAllString(result, "")
	}

	// Strip remaining command tags.
	for _, pat := range commandTagPatterns {
		result = pat.ReplaceAllString(result, "")
	}

	// Strip bash-input tags but keep inner content (the command text).
	result = reBashInput.ReplaceAllString(result, "$1")

	return strings.TrimSpace(result)
}

// extractCommandDisplay converts <command-name>/foo</command-name><command-args>bar</command-args>
// into "/foo bar".
func extractCommandDisplay(s string) string {
	m := reCommandName.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	name := "/" + strings.TrimSpace(m[1])
	if am := reCommandArgs.FindStringSubmatch(s); am != nil {
		if args := strings.TrimSpace(am[1]); args != "" {
			return name + " " + args
		}
	}
	return name
}

// ExtractText pulls display text from a json.RawMessage that is either a
// JSON string or an array of content blocks. Text blocks are joined with newlines.
func ExtractText(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}

	// Try string first (the common case for user messages).
	var s string
	if err := json.Unmarshal(content, &s); err == nil {
		return s
	}

	// Array of content blocks.
	var blocks []textBlockJSON
	if err := json.Unmarshal(content, &blocks); err != nil {
		return ""
	}

	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// ExtractCommandOutput returns the inner text from <local-command-stdout> or
// <local-command-stderr> wrapper tags. Returns empty string if neither tag is found.
func ExtractCommandOutput(s string) string {
	if m := reStdout.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	if m := reStderr.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// IsCommandOutput returns true when content starts with a local-command output tag.
func IsCommandOutput(s string) bool {
	return strings.HasPrefix(s, localCommandStdoutTag) || strings.HasPrefix(s, localCommandStderrTag)
}

// extractBashOutput returns the inner text from <bash-stdout> or <bash-stderr>
// wrapper tags. Tries stdout first, falls back to stderr. Same pattern as
// ExtractCommandOutput but for inline !bash mode execution.
func extractBashOutput(s string) string {
	if m := reBashStdout.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	if m := reBashStderr.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// extractTaskNotification pulls the human-readable summary from a
// <task-notification> XML wrapper. Falls back to stripping all XML tags.
func extractTaskNotification(s string) string {
	if m := reTaskNotifySummary.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	// Fallback: strip all XML-like tags and return what's left.
	stripped := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, " ")
	return strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(stripped, " "))
}
