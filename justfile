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
