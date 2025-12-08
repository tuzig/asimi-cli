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

# Profile startup performance
measure: build
    #!/bin/bash
    # Profile startup performance of asimi
    set -e
    PROFILE_DIR="./profiles"
    mkdir -p "$PROFILE_DIR"
    echo "=== Profiling Asimi Startup ==="
    echo ""
    # Clean up old profiles
    rm -f "$PROFILE_DIR"/*
    echo "1. Running with CPU profiling and execution trace..."
    echo "   (Press Ctrl+C after the UI appears to stop profiling)"
    echo ""
    # Run with profiling - use timeout to auto-quit after 3 seconds
    timeout 3s ./asimi --debug --cpu-profile="$PROFILE_DIR/cpu.prof" --trace="$PROFILE_DIR/trace.out" --mem-profile="$PROFILE_DIR/mem.prof" 2>&1 | tee "$PROFILE_DIR/timing.log" || true
    echo ""
    echo "=== Profile Analysis ==="
    echo ""
    if [ -f "$PROFILE_DIR/cpu.prof" ]; then
        echo "2. CPU Profile Top Functions:"
        echo "   (Functions taking the most CPU time)"
        echo ""
        go tool pprof -top -cum "$PROFILE_DIR/cpu.prof" | head -20
        echo ""
        echo "3. CPU Profile - Startup Critical Path:"
        echo "   (Looking at main and initialization functions)"
        echo ""
        go tool pprof -list="main\.(main|Run|getLLMClient|NewTUIModel)" "$PROFILE_DIR/cpu.prof" 2>/dev/null || echo "   No detailed listing available"
        echo ""
    fi
    if [ -f "$PROFILE_DIR/mem.prof" ]; then
        echo "4. Memory Profile Top Allocations:"
        echo ""
        go tool pprof -top -alloc_space "$PROFILE_DIR/mem.prof" | head -20
        echo ""
    fi
    if [ -f "$PROFILE_DIR/trace.out" ]; then
        echo "5. Execution Trace:"
        echo "   Opening trace viewer in browser..."
        echo "   Look for goroutine blocking and scheduler delays"
        echo ""
        go tool trace "$PROFILE_DIR/trace.out" &
        TRACE_PID=$!
        echo "   Trace viewer started (PID: $TRACE_PID)"
        echo "   Press Enter to continue and close trace viewer..."
        read
        kill $TRACE_PID 2>/dev/null || true
    fi
    echo ""
    echo "=== Timing Summary from Debug Output ==="
    if [ -f "$PROFILE_DIR/timing.log" ]; then
        grep "\[TIMING\]" "$PROFILE_DIR/timing.log" || echo "No timing data found"
    fi
    echo ""
    echo "=== Profile files saved in $PROFILE_DIR ==="
    echo "To analyze interactively:"
    echo "  go tool pprof -http=:8080 $PROFILE_DIR/cpu.prof"
    echo "  go tool pprof -http=:8080 $PROFILE_DIR/mem.prof"
    echo "  go tool trace $PROFILE_DIR/trace.out"
    ./profile_startup.sh

# Build the sandbox container
build-sandbox:
    @podman machine init --disk-size 30 >/dev/null 2>&1 || true
    @podman machine start >/dev/null 2>&1 || true
    podman build -t localhost/asimi-sandbox-{{PROJECT_NAME}}:latest -f .agents/sandbox/Dockerfile .

# Measure run_in_shell tool performance
measure-shell:
    @echo "=== Measuring run_in_shell Tool Performance ==="
    @echo ""
    @echo "Sending performance test prompt to asimi..."
    @echo ""
    go run . -p 'Performance test: measure the run_in_shell tool overhead by executiing exactly 12 run_in_shell commands in a SINGLE function_calls block (all at once, not sequentially): 1. First command: date +%s%N, 2-11. Ten commands: : (colon command, does nothing), 12. Last command: date +%s%N. After receiving both the timestamps, calculates the per call overhead'
