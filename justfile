# List available recipes
default:
    @just --list

# Run all checks
check: lint test check-fmt

# Lint all
lint: check-typos check-nix-fmt lint-actions golangci-lint

# Format all
fmt: fmt-go fmt-nix

# Build
build:
    go build ./...

# Run golangci-lint
golangci-lint:
    golangci-lint run ./...

# Run tests
test:
    go test ./...

# Run tests with race detector
test-race:
    go test -race ./...

# Check Go formatting
check-fmt:
    test -z "$(gofmt -l .)" || { echo "Files not formatted:"; gofmt -l .; exit 1; }

# Format Go code
fmt-go:
    gofmt -w .

# Check Nix formatting
check-nix-fmt:
    alejandra --check flake.nix

# Format Nix files
fmt-nix:
    alejandra flake.nix

# Check for typos
check-typos:
    typos

# Lint GitHub Actions
lint-actions:
    actionlint

# Generate protobuf
proto:
    buf generate

# Set up git hooks
setup-hooks:
    git config core.hooksPath .githooks
