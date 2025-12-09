PROJECT_NAME := "afittestide-asimi-cli"

# List all available recipes
default:
    @just --list

# Install dependencies
install:
    go mod download
    go mod vendor

# Build the binary
build:
    go build -o asimi .

# Run with debug logging
run:
    go run . --debug

# Run all tests
test:
    go test -v ./...

# Run tests with coverage
test-coverage:
    go test -v -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html

# Run linting
lint:
    golangci-lint run

# Format code
fmt:
    go fmt ./...
    goimports -w .

# Clean build artifacts
clean:
    rm -f asimi
    rm -f coverage.out coverage.html
    rm -f asimi.log
    rm -rf test_tmp
    rm -rf profiles

# Install development tools
bootstrap:
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
    go install golang.org/x/tools/cmd/goimports@latest

# Build the sandbox container
build-sandbox:
    @podman machine init --disk-size 30 >/dev/null 2>&1 || true
    @podman machine start >/dev/null 2>&1 || true
    podman build -t localhost/asimi-sandbox-{{PROJECT_NAME}}:latest -f .agents/sandbox/Dockerfile .

# Clean up the sandbox container
clean-sandbox:
    podman rmi localhost/asimi-sandbox-{{PROJECT_NAME}}:latest

# Measure run_in_shell tool performance
measure:
    @echo "=== Measuring run_in_shell Tool Performance ==="
    @echo ""
    @echo "Sending performance test prompt to asimi..."
    @echo ""
    go run . -p 'Performance test: measure the run_in_shell tool overhead by executiing exactly 12 run_in_shell commands in a SINGLE function_calls block (all at once, not sequentially): 1. First command: date +%s%N, 2-11. Ten commands: : (colon command, does nothing), 12. Last command: date +%s%N. After receiving both the timestamps, calculates the per call overhead'
