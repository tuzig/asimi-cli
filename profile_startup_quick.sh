#!/bin/bash
# Quick startup profiling - automatically exits after 2 seconds

set -e

PROFILE_DIR="./profiles"
mkdir -p "$PROFILE_DIR"

echo "=== Quick Startup Profiling ==="
echo ""

# Clean up old profiles
rm -f "$PROFILE_DIR"/*

echo "Running asimi with profiling (will auto-exit after 2 seconds)..."
echo ""

# Run with profiling and auto-kill after 2 seconds
timeout 2s ./asimi --debug --cpu-profile="$PROFILE_DIR/cpu.prof" --trace="$PROFILE_DIR/trace.out" --mem-profile="$PROFILE_DIR/mem.prof" 2>&1 | tee "$PROFILE_DIR/timing.log" || true

echo ""
echo "=== Timing Analysis ==="
echo ""

if [ -f "$PROFILE_DIR/timing.log" ]; then
    echo "Startup timing breakdown:"
    grep "\[TIMING\]" "$PROFILE_DIR/timing.log" | while read line; do
        echo "  $line"
    done
    echo ""
fi

if [ -f "$PROFILE_DIR/cpu.prof" ]; then
    echo "=== CPU Profile - Top 20 Functions ==="
    echo ""
    go tool pprof -top -cum "$PROFILE_DIR/cpu.prof" 2>/dev/null | head -25
    echo ""
    
    echo "=== CPU Profile - Main/Init Functions ==="
    echo ""
    go tool pprof -list="main\.(main|Run|getLLMClient|NewTUIModel|LoadConfig)" "$PROFILE_DIR/cpu.prof" 2>/dev/null | head -50 || echo "No detailed listing available"
    echo ""
fi

if [ -f "$PROFILE_DIR/mem.prof" ]; then
    echo "=== Memory Profile - Top 15 Allocations ==="
    echo ""
    go tool pprof -top -alloc_space "$PROFILE_DIR/mem.prof" 2>/dev/null | head -20
    echo ""
fi

echo "=== Summary ==="
echo ""
echo "Profile files saved in $PROFILE_DIR/"
echo ""
echo "To analyze interactively:"
echo "  go tool pprof -http=:8080 $PROFILE_DIR/cpu.prof"
echo "  go tool pprof -http=:8080 $PROFILE_DIR/mem.prof"
echo "  go tool trace $PROFILE_DIR/trace.out"
echo ""
echo "To see what's slow during startup:"
echo "  go tool pprof -web $PROFILE_DIR/cpu.prof"
