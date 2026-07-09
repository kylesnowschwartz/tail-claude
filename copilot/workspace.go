package copilot

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kylesnowschwartz/agent-ouija/claude/transcript"
)

// Workspace holds the metadata Copilot writes to a session dir's
// workspace.yaml. The file is flat key: value pairs, so it is hand-parsed
// (no YAML dependency).
type Workspace struct {
	ID         string
	Cwd        string
	GitRoot    string
	Branch     string
	Name       string
	ClientName string
	Summary    string
	UserNamed  bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ParseWorkspaceYAML parses flat key: value YAML. Values may be quoted and
// may contain colons (paths, timestamps); the key is split on the FIRST
// ": " occurrence. Unknown keys are ignored; no nesting support (the
// schema is flat).
func ParseWorkspaceYAML(data []byte) Workspace {
	var w Workspace
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, val, ok := splitKeyValue(trimmed)
		if !ok {
			continue
		}
		switch key {
		case "id":
			w.ID = val
		case "cwd":
			w.Cwd = val
		case "git_root":
			w.GitRoot = val
		case "branch":
			w.Branch = val
		case "name":
			w.Name = val
		case "client_name":
			w.ClientName = val
		case "summary":
			w.Summary = val
		case "user_named":
			w.UserNamed = val == "true"
		case "created_at":
			w.CreatedAt = parseYAMLTime(val)
		case "updated_at":
			w.UpdatedAt = parseYAMLTime(val)
		}
	}
	return w
}

// ReadWorkspace reads {sessionDir}/workspace.yaml.
// Returns false when the file is missing or unreadable.
func ReadWorkspace(sessionDir string) (Workspace, bool) {
	data, err := os.ReadFile(filepath.Join(sessionDir, "workspace.yaml"))
	if err != nil {
		return Workspace{}, false
	}
	return ParseWorkspaceYAML(data), true
}

// splitKeyValue splits a line on the first ": " (values contain colons in
// timestamps and paths). A line ending in a bare ":" yields an empty value.
func splitKeyValue(line string) (string, string, bool) {
	if i := strings.Index(line, ": "); i >= 0 {
		return strings.TrimSpace(line[:i]), stripQuotes(strings.TrimSpace(line[i+2:])), true
	}
	if strings.HasSuffix(line, ":") {
		return strings.TrimSpace(strings.TrimSuffix(line, ":")), "", true
	}
	return "", "", false
}

// stripQuotes removes one matching pair of surrounding single or double quotes.
func stripQuotes(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// parseYAMLTime parses a workspace.yaml timestamp. Copilot writes RFC3339
// with milliseconds; ParseTimestamp adds RFC3339 and no-timezone fallbacks.
// A final "date time" layout covers hypothetical space-separated values.
func parseYAMLTime(s string) time.Time {
	if t := transcript.ParseTimestamp(s); !t.IsZero() {
		return t
	}
	if t, err := time.Parse("2006-01-02 15:04:05.999999999 -0700", s); err == nil {
		return t
	}
	return time.Time{}
}
