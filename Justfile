PROJECT_NAME := `git config --get remote.origin.url | sed -E 's/.*[:\/]([^\/]+)\/([^\/]+)\.git$/\1\/\2/'`

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
    pkill -9 asimi || true
    rm -f asimi.log asimi-daemon.log
    go build .
    ./asimi --debug

# Run all tests (skips git-altering tests locally)
test:
    go test -timeout 1m ./... | tee test.out

# Run all tests including git-altering tests (CI mode)
test-ci:
    CI=1 go test -timeout 5m -v ./...

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

# Freeze the log files to asimi.<suffix>.log and asimi-daemon.<suffix>.log
freeze-log suffix:
    cp asimi.log asimi.{{suffix}}.log
    cp asimi-daemon.log asimi-daemon.{{suffix}}.log

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

# Init and start the podman machine 
init-podman:
    podman machine init --disk-size 30 >/dev/null 2>&1 || true
    podman machine start >/dev/null 2>&1 || true
# Build the sandbox container
build-sandbox: init-podman
    podman build -t localhost/asimi/sandbox/{{PROJECT_NAME}}:latest -f .agents/sandbox/Dockerfile .

# Clean up the sandbox container
clean-sandbox:
    podman rmi localhost/asimi/sandbox/{{PROJECT_NAME}}:latest

# Measure run_shell_command tool performance
measure:
    @echo "=== Measuring run_shell_command Tool Performance ==="
    @echo ""
    @echo "Sending performance test prompt to asimi..."
    @echo ""
    go run . -p 'Performance test: measure the run_shell_command tool overhead by executiing exactly 12 run_shell_command commands in a SINGLE function_calls block (all at once, not sequentially): 1. First command: date +%s%N, 2-11. Ten commands: : (colon command, does nothing), 12. Last command: date +%s%N. After receiving both the timestamps, calculates the per call overhead'
