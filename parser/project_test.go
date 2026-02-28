package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectName(t *testing.T) {
	t.Run("normal git repo returns repo dir name", func(t *testing.T) {
		root := t.TempDir()
		repo := filepath.Join(root, "my-project")
		subdir := filepath.Join(repo, "internal", "pkg")

		mustMkdirAll(t, filepath.Join(repo, ".git"))
		mustMkdirAll(t, subdir)

		got := ProjectName(subdir, "")
		if got != "my-project" {
			t.Fatalf("ProjectName(%q, %q) = %q, want %q", subdir, "", got, "my-project")
		}
	})

	t.Run("worktree resolves to main repo name via commondir", func(t *testing.T) {
		root := t.TempDir()
		mainRepo := filepath.Join(root, "tail-claude")
		worktree := filepath.Join(root, "tail-claude-feature-x")
		worktreeGitDir := filepath.Join(mainRepo, ".git", "worktrees", "feature-x")

		mustMkdirAll(t, filepath.Join(mainRepo, ".git"))
		mustMkdirAll(t, worktreeGitDir)
		mustMkdirAll(t, worktree)

		mustWriteFile(t, filepath.Join(worktree, ".git"),
			"gitdir: "+worktreeGitDir+"\n")
		mustWriteFile(t, filepath.Join(worktreeGitDir, "commondir"), "../..\n")

		got := ProjectName(worktree, "feature-x")
		if got != "tail-claude" {
			t.Fatalf("ProjectName(%q, %q) = %q, want %q", worktree, "feature-x", got, "tail-claude")
		}
	})

	t.Run("worktree fallback without commondir", func(t *testing.T) {
		root := t.TempDir()
		mainRepo := filepath.Join(root, "my-repo")
		worktree := filepath.Join(root, "my-repo-experiment")
		worktreeGitDir := filepath.Join(mainRepo, ".git", "worktrees", "exp")

		mustMkdirAll(t, filepath.Join(mainRepo, ".git"))
		mustMkdirAll(t, worktreeGitDir)
		mustMkdirAll(t, worktree)

		// .git file points to worktrees dir, but no commondir file.
		mustWriteFile(t, filepath.Join(worktree, ".git"),
			"gitdir: "+worktreeGitDir+"\n")

		got := ProjectName(worktree, "experiment")
		if got != "my-repo" {
			t.Fatalf("ProjectName(%q, %q) = %q, want %q", worktree, "experiment", got, "my-repo")
		}
	})

	t.Run("branch suffix trimmed for offline worktree", func(t *testing.T) {
		// No .git exists anywhere -- simulates offline/stale worktree.
		root := t.TempDir()
		dir := filepath.Join(root, "agentsview-worktree-tool-call-arguments")
		mustMkdirAll(t, dir)

		got := ProjectName(dir, "worktree-tool-call-arguments")
		if got != "agentsview" {
			t.Fatalf("ProjectName(%q, %q) = %q, want %q", dir, "worktree-tool-call-arguments", got, "agentsview")
		}
	})

	t.Run("branch with slash normalized and trimmed", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "project-feature-worktree-support")
		mustMkdirAll(t, dir)

		got := ProjectName(dir, "feature/worktree-support")
		if got != "project" {
			t.Fatalf("ProjectName(%q, %q) = %q, want %q", dir, "feature/worktree-support", got, "project")
		}
	})

	t.Run("default branch not trimmed", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "project-main")
		mustMkdirAll(t, dir)

		got := ProjectName(dir, "main")
		if got != "project-main" {
			t.Fatalf("ProjectName(%q, %q) = %q, want %q", dir, "main", got, "project-main")
		}
	})

	t.Run("no git found falls back to filepath.Base", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "plain-dir")
		mustMkdirAll(t, dir)

		got := ProjectName(dir, "")
		if got != "plain-dir" {
			t.Fatalf("ProjectName(%q, %q) = %q, want %q", dir, "", got, "plain-dir")
		}
	})

	t.Run("empty cwd returns empty string", func(t *testing.T) {
		got := ProjectName("", "")
		if got != "" {
			t.Fatalf("ProjectName(%q, %q) = %q, want %q", "", "", got, "")
		}
	})

	t.Run("subdir of worktree resolves to main repo", func(t *testing.T) {
		root := t.TempDir()
		mainRepo := filepath.Join(root, "my-app")
		worktree := filepath.Join(root, "my-app-fix-bug")
		worktreeGitDir := filepath.Join(mainRepo, ".git", "worktrees", "fix-bug")

		mustMkdirAll(t, filepath.Join(mainRepo, ".git"))
		mustMkdirAll(t, worktreeGitDir)
		subdir := filepath.Join(worktree, "src", "components")
		mustMkdirAll(t, subdir)

		mustWriteFile(t, filepath.Join(worktree, ".git"),
			"gitdir: "+worktreeGitDir+"\n")
		mustWriteFile(t, filepath.Join(worktreeGitDir, "commondir"), "../..\n")

		got := ProjectName(subdir, "fix-bug")
		if got != "my-app" {
			t.Fatalf("ProjectName(%q, %q) = %q, want %q", subdir, "fix-bug", got, "my-app")
		}
	})

	t.Run("mismatched branch not trimmed", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "project-hotfix")
		mustMkdirAll(t, dir)

		got := ProjectName(dir, "feature/other")
		if got != "project-hotfix" {
			t.Fatalf("ProjectName(%q, %q) = %q, want %q", dir, "feature/other", got, "project-hotfix")
		}
	})
}

func TestFindGitRepoRoot(t *testing.T) {
	t.Run("normal repo from subdir", func(t *testing.T) {
		root := t.TempDir()
		repo := filepath.Join(root, "myrepo")
		sub := filepath.Join(repo, "a", "b")

		mustMkdirAll(t, filepath.Join(repo, ".git"))
		mustMkdirAll(t, sub)

		got := findGitRepoRoot(sub)
		if got != repo {
			t.Fatalf("findGitRepoRoot(%q) = %q, want %q", sub, got, repo)
		}
	})

	t.Run("worktree with commondir", func(t *testing.T) {
		root := t.TempDir()
		mainRepo := filepath.Join(root, "upstream")
		wt := filepath.Join(root, "upstream-wt")
		wtGitDir := filepath.Join(mainRepo, ".git", "worktrees", "wt")

		mustMkdirAll(t, filepath.Join(mainRepo, ".git"))
		mustMkdirAll(t, wtGitDir)
		mustMkdirAll(t, wt)

		mustWriteFile(t, filepath.Join(wt, ".git"), "gitdir: "+wtGitDir+"\n")
		mustWriteFile(t, filepath.Join(wtGitDir, "commondir"), "../..\n")

		got := findGitRepoRoot(wt)
		if got != mainRepo {
			t.Fatalf("findGitRepoRoot(%q) = %q, want %q", wt, got, mainRepo)
		}
	})

	t.Run("no .git returns empty", func(t *testing.T) {
		root := t.TempDir()
		dir := filepath.Join(root, "no-git")
		mustMkdirAll(t, dir)

		got := findGitRepoRoot(dir)
		if got != "" {
			t.Fatalf("findGitRepoRoot(%q) = %q, want %q", dir, got, "")
		}
	})

	t.Run("empty string returns empty", func(t *testing.T) {
		got := findGitRepoRoot("")
		if got != "" {
			t.Fatalf("findGitRepoRoot(%q) = %q, want %q", "", got, "")
		}
	})
}

func TestTrimBranchSuffix(t *testing.T) {
	tests := []struct {
		name   string
		dir    string
		branch string
		want   string
	}{
		{"simple match", "project-feature", "feature", "project"},
		{"slash branch", "project-feat-auth", "feat/auth", "project"},
		{"underscore sep", "project_bugfix", "bugfix", "project"},
		{"default branch main", "project-main", "main", "project-main"},
		{"default branch master", "project-master", "master", "project-master"},
		{"default branch develop", "project-develop", "develop", "project-develop"},
		{"no match", "project-hotfix", "other-branch", "project-hotfix"},
		{"empty branch", "project-x", "", "project-x"},
		{"empty name", "", "feature", ""},
		{"refs/heads prefix stripped", "project-feat", "refs/heads/feat", "project"},
		{"case insensitive", "Project-FeaTure", "feature", "Project"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimBranchSuffix(tt.dir, tt.branch)
			if got != tt.want {
				t.Fatalf("trimBranchSuffix(%q, %q) = %q, want %q", tt.dir, tt.branch, got, tt.want)
			}
		})
	}
}

func TestNormalizeBranchToken(t *testing.T) {
	tests := []struct {
		branch string
		want   string
	}{
		{"feature/auth", "feature-auth"},
		{"feat-auth", "feat-auth"},
		{"feat_auth", "feat-auth"},
		{"Feature.Auth", "feature-auth"},
		{"multiple///slashes", "multiple-slashes"},
		{"", ""},
		{"UPPERCASE", "uppercase"},
	}
	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			got := normalizeBranchToken(tt.branch)
			if got != tt.want {
				t.Fatalf("normalizeBranchToken(%q) = %q, want %q", tt.branch, got, tt.want)
			}
		})
	}
}

func TestIsDefaultBranch(t *testing.T) {
	for _, branch := range []string{"main", "master", "trunk", "develop", "dev", "Main", "MASTER"} {
		if !isDefaultBranch(branch) {
			t.Errorf("isDefaultBranch(%q) = false, want true", branch)
		}
	}
	for _, branch := range []string{"feature", "fix-bug", "release", ""} {
		if isDefaultBranch(branch) {
			t.Errorf("isDefaultBranch(%q) = true, want false", branch)
		}
	}
}

// mustMkdirAll and mustWriteFile are test helpers.
func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
