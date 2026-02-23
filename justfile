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
    #!/usr/bin/env bash
    if [[ "{{version}}" != @(patch|minor|major) ]]; then
        echo "Usage: just bump <patch|minor|major>"
        exit 1
    fi

    # Read current version from VERSION file (default to v0.0.0)
    current=$(cat VERSION 2>/dev/null || echo "v0.0.0")
    current=${current#v}  # Remove 'v' prefix

    # Parse version
    IFS='.' read -r major minor patch <<< "$current"
    major=${major:-0}
    minor=${minor:-0}
    patch=${patch:-0}

    # Bump the requested part
    case "{{version}}" in
        patch) ((patch++)) ;;
        minor) minor=$((minor+1)); patch=0 ;;
        major) major=$((major+1)); minor=0; patch=0 ;;
    esac

    new_version="v${major}.${minor}.${patch}"
    echo "$new_version" > VERSION
    echo "Bumped version: $current → $new_version"

# Create a release tag
release: check
    #!/usr/bin/env bash
    version=$(cat VERSION 2>/dev/null || echo "v0.0.0")

    # Check if tag already exists
    if git rev-parse "$version" >/dev/null 2>&1; then
        echo "Tag $version already exists"
        exit 1
    fi

    # Create annotated tag
    git tag -a "$version" -m "Release $version"
    echo "Created tag: $version"
    echo ""
    echo "Push with: git push origin $version"
