# tail-claude development commands

# Build the binary
build:
    go build -ldflags "-X main.version=$(cat VERSION)" -o ./tail-claude .

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
    go build -race -ldflags "-X main.version=$(cat VERSION)" -o ./tail-claude . && ./tail-claude

# Update to the latest released version via go install
update:
    go install github.com/kylesnowschwartz/tail-claude@latest

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

# Commit, tag, and push the release.
# Pass a release notes file or omit for auto-generated notes.
# Example: just release notes.md
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

    notes="{{notes}}"

    # Update CHANGELOG.md: replace "## Unreleased" with the version + date,
    # then add a fresh "## Unreleased" section above it.
    if [[ -f CHANGELOG.md ]]; then
        date=$(date +%Y-%m-%d)
        sed -i '' "s/^## Unreleased$/## Unreleased\n\n## $v ($date)/" CHANGELOG.md
        git add CHANGELOG.md
    fi

    # Commit, tag, push, release
    git commit -m "chore: Bump version to $v"
    git tag "$v"
    git push && git push --tags

    # Create GitHub Release with notes file or auto-generated.
    if [[ -n "$notes" && -f "$notes" ]]; then
        gh release create "$v" --title "$v" --notes-file "$notes" --latest
    else
        gh release create "$v" --title "$v" --generate-notes --latest
    fi

    # Prime the Go module proxy cache so `go install ...@latest` resolves immediately.
    # Both the explicit version AND @latest must be primed — they have separate caches.
    GOPROXY=https://proxy.golang.org go list -m "github.com/kylesnowschwartz/tail-claude@$v" || true
    GOPROXY=https://proxy.golang.org go list -m "github.com/kylesnowschwartz/tail-claude@latest" || true

    echo "Released $v"
