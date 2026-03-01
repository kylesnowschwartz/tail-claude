# tail-claude development commands

# Build the binary
build:
    go build -o ./tail-claude .

# Build, vet, and static analysis
check:
    go build ./... && go vet ./... && staticcheck ./...

# Run tests
test:
    go test ./...

# Dump the current session (collapsed)
dump: build
    ./tail-claude --dump

# Dump the current session (expanded)
dump-expand: build
    ./tail-claude --dump --expand

# Dump a specific session file
dump-file path: build
    ./tail-claude --dump "{{path}}"

# Run the TUI against the current session
run: build
    ./tail-claude

# Run the TUI against a specific session file
run-file path: build
    ./tail-claude "{{path}}"

# Build and run with race detector
race:
    go build -race -o ./tail-claude . && ./tail-claude

# Bump version (patch, minor, or major)
bump version:
    #!/usr/bin/env zsh
    set -e

    # Parse current version
    v=$(cat VERSION)
    IFS='.' read -r M m p <<< "$v"

    # Calculate new version
    case {{version}} in
        patch) new="$M.$m.$((p+1))" ;;
        minor) new="$M.$((m+1)).0" ;;
        major) new="$((M+1)).0.0" ;;
        *) echo "Usage: just bump patch|minor|major" && exit 1 ;;
    esac

    echo "Bumping $v → $new"

    # Update VERSION file
    echo "$new" > VERSION

    # Stage the change
    git add VERSION

    echo "Version bumped to $new. Changes staged and ready. Run 'just release' to commit, tag, and push."

# Commit, tag, and push the release. Pass a notes file for custom release notes.
release notes="":
    #!/usr/bin/env zsh
    set -e

    v=$(cat VERSION)

    # Safety: ensure we're on main and up to date
    branch=$(git branch --show-current)
    if [[ "$branch" != "main" ]]; then
        echo "Error: must be on main branch (currently on $branch)"
        exit 1
    fi

    git fetch origin main
    behind=$(git rev-list HEAD..origin/main --count)
    if [[ "$behind" -gt 0 ]]; then
        echo "Error: $behind commit(s) behind origin/main"
        echo "Run 'git pull --rebase' first"
        exit 1
    fi

    # Check for uncommitted changes (should have version bump staged)
    if git diff --cached --quiet; then
        echo "Error: nothing staged. Run 'just bump' first."
        exit 1
    fi

    # Commit, tag, push, release
    git commit -m "chore: Bump version to $v"
    git tag "$v"
    git push && git push --tags

    # Create GitHub Release — use notes file if provided, otherwise auto-generate.
    notes="{{notes}}"
    if [[ -n "$notes" && -f "$notes" ]]; then
        gh release create "$v" --title "$v" --notes-file "$notes" --latest
    else
        gh release create "$v" --title "$v" --generate-notes --latest
    fi

    # Prime the Go module proxy cache so `go install ...@latest` resolves immediately
    GOPROXY=https://proxy.golang.org go list -m "github.com/kylesnowschwartz/tail-claude@$v" || true

    echo "Released $v"
