package main

import (
	"os"
	"testing"
)

func TestIsNewer(t *testing.T) {
	tests := []struct {
		latest  string
		current string
		want    bool
	}{
		// Newer
		{"v0.9.0", "v0.8.0", true},
		{"v1.0.0", "v0.9.0", true},
		{"v0.8.1", "v0.8.0", true},
		{"v2.0.0", "v1.99.99", true},

		// Equal
		{"v0.8.0", "v0.8.0", false},
		{"v1.0.0", "v1.0.0", false},

		// Older
		{"v0.7.0", "v0.8.0", false},
		{"v0.8.0", "v0.9.0", false},
		{"v0.8.0", "v1.0.0", false},

		// Without v prefix
		{"0.9.0", "0.8.0", true},
		{"0.8.0", "0.8.0", false},

		// Mixed prefix
		{"v0.9.0", "0.8.0", true},
		{"0.9.0", "v0.8.0", true},

		// Malformed
		{"", "v0.8.0", false},
		{"v0.8.0", "", false},
		{"not-a-version", "v0.8.0", false},
		{"v0.8.0", "not-a-version", false},
		{"v0.8", "v0.8.0", false},
		{"v0.8.0.1", "v0.8.0", false}, // extra dot — SplitN(3) keeps "0.1" as third part → Atoi fails
	}

	for _, tt := range tests {
		t.Run(tt.latest+"_vs_"+tt.current, func(t *testing.T) {
			got := isNewer(tt.latest, tt.current)
			if got != tt.want {
				t.Errorf("isNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
			}
		})
	}
}

func TestShouldSkipUpdateCheck(t *testing.T) {
	t.Run("skips dev builds", func(t *testing.T) {
		if !shouldSkipUpdateCheck("dev") {
			t.Error("should skip dev builds")
		}
	})

	t.Run("skips empty version", func(t *testing.T) {
		if !shouldSkipUpdateCheck("") {
			t.Error("should skip empty version")
		}
	})

	t.Run("skips when TAIL_CLAUDE_NO_UPDATE_CHECK is set", func(t *testing.T) {
		t.Setenv("TAIL_CLAUDE_NO_UPDATE_CHECK", "1")
		if !shouldSkipUpdateCheck("v0.8.0") {
			t.Error("should skip when env var is set")
		}
	})

	t.Run("skips in CI", func(t *testing.T) {
		t.Setenv("CI", "true")
		// Clear the opt-out var so we're only testing CI detection.
		os.Unsetenv("TAIL_CLAUDE_NO_UPDATE_CHECK")
		if !shouldSkipUpdateCheck("v0.8.0") {
			t.Error("should skip in CI")
		}
	})

	t.Run("does not skip normal version", func(t *testing.T) {
		os.Unsetenv("TAIL_CLAUDE_NO_UPDATE_CHECK")
		os.Unsetenv("CI")
		if shouldSkipUpdateCheck("v0.8.0") {
			t.Error("should not skip normal version")
		}
	})
}
