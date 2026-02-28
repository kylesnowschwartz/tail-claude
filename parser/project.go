package parser

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// ProjectName returns a display name for a project directory.
//
// If cwd is inside a git repository (including worktrees and submodules),
// resolves to the main repository root directory name. For worktree
// directories named "project-branch", trims the branch suffix so the
// display shows the canonical project name.
//
// Falls back to filepath.Base(cwd) if no .git is found.
func ProjectName(cwd, gitBranch string) string {
	if cwd == "" {
		return ""
	}
	cleaned := filepath.Clean(cwd)

	if root := findGitRepoRoot(cleaned); root != "" {
		return filepath.Base(root)
	}

	// No git repo found. Try trimming branch suffix from the directory name
	// (handles offline worktree paths where the worktree directory exists
	// but its .git file points to a non-existent main repo).
	name := filepath.Base(cleaned)
	name = trimBranchSuffix(name, gitBranch)
	return name
}

// findGitRepoRoot walks up from dir looking for .git. Handles both .git
// directories (normal repos) and .git files (worktrees/submodules). For
// .git files, reads the gitdir reference and resolves via commondir to
// find the main repository root.
//
// Returns empty string if no .git is found.
func findGitRepoRoot(dir string) string {
	if dir == "" {
		return ""
	}

	current := dir
	// If dir isn't a directory (e.g. a file path), start from its parent.
	if info, err := os.Stat(current); err == nil {
		if !info.IsDir() {
			current = filepath.Dir(current)
		}
	} else {
		// Path doesn't exist -- avoid walking non-paths.
		if !strings.ContainsRune(current, filepath.Separator) {
			return ""
		}
		current = filepath.Dir(current)
	}

	for {
		gitPath := filepath.Join(current, ".git")
		info, err := os.Stat(gitPath)
		if err == nil {
			if info.IsDir() {
				// Normal git repo -- this directory is the root.
				return current
			}
			if info.Mode().IsRegular() {
				// .git file -- worktree or submodule.
				if root := repoRootFromGitFile(current, gitPath); root != "" {
					return root
				}
				// Conservative fallback: treat the worktree directory as root.
				return current
			}
		}

		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

// repoRootFromGitFile resolves the main repository root from a .git file.
// Reads the gitdir reference, then checks commondir to find the real .git
// directory. Falls back to parsing the worktrees path structure.
func repoRootFromGitFile(repoDir, gitFilePath string) string {
	gitDir := readGitDirFromFile(gitFilePath)
	if gitDir == "" {
		return ""
	}
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Clean(filepath.Join(filepath.Dir(gitFilePath), gitDir))
	}

	commonDir := readCommonDir(gitDir)
	if commonDir != "" {
		if filepath.Base(commonDir) == ".git" {
			return filepath.Dir(commonDir)
		}
	}

	// Fallback: parse the worktrees path structure.
	// gitDir looks like /repo/.git/worktrees/<name>
	marker := string(filepath.Separator) + ".git" +
		string(filepath.Separator) + "worktrees" +
		string(filepath.Separator)
	if root, _, found := strings.Cut(gitDir, marker); found {
		if root != "" {
			return filepath.Clean(root)
		}
	}

	return repoDir
}

// readGitDirFromFile reads the "gitdir: <path>" reference from a .git file.
func readGitDirFromFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		const prefix = "gitdir:"
		if strings.HasPrefix(strings.ToLower(line), prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	return ""
}

// readCommonDir reads the commondir file from a git directory (used by
// worktrees to reference the main repo's .git).
func readCommonDir(gitDir string) string {
	b, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return ""
	}
	value := strings.TrimSpace(string(b))
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(gitDir, value))
}

// trimBranchSuffix strips a git branch name suffix from a directory name.
// Worktree directories are commonly named "project-branch-name". This
// normalizes back to the project name. Default branches (main, master,
// trunk, develop, dev) are not trimmed -- a directory called "project-main"
// is likely named intentionally.
func trimBranchSuffix(name, gitBranch string) string {
	branch := strings.TrimSpace(gitBranch)
	if name == "" || branch == "" {
		return name
	}
	branch = strings.TrimPrefix(branch, "refs/heads/")
	branchToken := normalizeBranchToken(branch)
	if branchToken == "" {
		return name
	}
	if isDefaultBranch(branchToken) {
		return name
	}

	for _, sep := range []string{"-", "_"} {
		suffix := sep + branchToken
		if strings.HasSuffix(strings.ToLower(name), strings.ToLower(suffix)) {
			base := strings.TrimRight(name[:len(name)-len(suffix)], "-_")
			if base != "" {
				return base
			}
		}
	}
	return name
}

// normalizeBranchToken converts a branch name to a comparable token.
// Slashes, dashes, underscores, dots, and spaces become single dashes.
// Letters are lowered. Other characters become dashes.
func normalizeBranchToken(branch string) string {
	var b strings.Builder
	b.Grow(len(branch))

	lastDash := false
	for _, r := range branch {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			lastDash = false
		case r == '/' || r == '-' || r == '_' || r == '.' || unicode.IsSpace(r):
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// isDefaultBranch returns true for common default branch names that should
// not be trimmed from directory names.
func isDefaultBranch(branch string) bool {
	switch strings.ToLower(strings.TrimSpace(branch)) {
	case "main", "master", "trunk", "develop", "dev":
		return true
	default:
		return false
	}
}
