package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// updateAvailableMsg carries the latest version when it's newer than the
// running version. Empty version means no update or check failed/skipped.
type updateAvailableMsg struct{ version string }

const (
	goProxyURL       = "https://proxy.golang.org/github.com/kylesnowschwartz/tail-claude/@latest"
	versionCheckTime = 2 * time.Second
)

// checkLatestVersionCmd returns a Bubble Tea command that queries the Go module
// proxy for the latest version. Skips silently for dev builds, CI, and when
// the user has opted out via environment variable.
func checkLatestVersionCmd(currentVersion string) tea.Cmd {
	return func() tea.Msg {
		if shouldSkipUpdateCheck(currentVersion) {
			return updateAvailableMsg{}
		}
		latest, err := fetchLatestVersion()
		if err != nil || latest == "" || !isNewer(latest, currentVersion) {
			return updateAvailableMsg{}
		}
		return updateAvailableMsg{version: latest}
	}
}

// shouldSkipUpdateCheck returns true when the version check should not run.
func shouldSkipUpdateCheck(currentVersion string) bool {
	if currentVersion == "dev" || currentVersion == "" {
		return true
	}
	if os.Getenv("TAIL_CLAUDE_NO_UPDATE_CHECK") != "" {
		return true
	}
	if os.Getenv("CI") != "" {
		return true
	}
	return false
}

// goProxyResponse is the JSON shape returned by proxy.golang.org/{module}/@latest.
type goProxyResponse struct {
	Version string `json:"Version"`
}

// fetchLatestVersion queries the Go module proxy for the latest tagged version.
func fetchLatestVersion() (string, error) {
	client := &http.Client{Timeout: versionCheckTime}
	resp, err := client.Get(goProxyURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("proxy returned %d", resp.StatusCode)
	}

	var info goProxyResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return "", err
	}
	return info.Version, nil
}

// isNewer returns true when latest is a strictly higher semver than current.
// Both must be "vMAJOR.MINOR.PATCH" format; malformed input returns false.
func isNewer(latest, current string) bool {
	latParts, latOk := parseSemver(latest)
	curParts, curOk := parseSemver(current)
	if !latOk || !curOk {
		return false
	}
	for i := 0; i < 3; i++ {
		if latParts[i] > curParts[i] {
			return true
		}
		if latParts[i] < curParts[i] {
			return false
		}
	}
	return false // equal
}

// parseSemver splits "v1.2.3" into [1, 2, 3]. Returns false for malformed input.
func parseSemver(v string) ([3]int, bool) {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var result [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		result[i] = n
	}
	return result, true
}
