#!/bin/bash
# Profile startup with auto-exit

set -e

PROFILE_DIR="./profiles"
mkdir -p "$PROFILE_DIR"

echo "=== Asimi Startup Profiling ==="
echo ""

# Clean up old profiles
rm -f "$PROFILE_DIR"/*

echo "Running asimi with profiling (will auto-exit after 2000ms)..."
echo ""

# Run with profiling and auto-exit
./asimi --debug --cpu-profile="$PROFILE_DIR/cpu.prof" --trace="$PROFILE_DIR/trace.out" --mem-profile="$PROFILE_DIR/mem.prof" --profile-exit-ms=2000 2>&1 | tee "$PROFILE_DIR/timing.log"

echo ""
echo "=== Timing Analysis ==="
echo ""

if [ -f "$PROFILE_DIR/timing.log" ]; then
    echo "Startup timing breakdown:"
    grep "\[TIMING\]" "$PROFILE_DIR/timing.log" 2>/dev/null | while read line; do
        echo "  $line"
    done
    echo ""
fi

if [ -f "$PROFILE_DIR/cpu.prof" ]; then
    echo "=== CPU Profile - Top 25 Functions (by cumulative time) ==="
    echo ""
    go tool pprof -top -cum "$PROFILE_DIR/cpu.prof" 2>/dev/null | head -30
    echo ""
    
    echo "=== CPU Profile - Initialization Functions ==="
    echo ""
    go tool pprof -list="main\.(main|Run|getLLMClient|NewTUIModel|LoadConfig)" "$PROFILE_DIR/cpu.prof" 2>/dev/null || echo "No detailed listing available"
    echo ""
    
    echo "=== CPU Profile - Config and Keyring ==="
    echo ""
    go tool pprof -list="main\.(LoadConfig|.*Keyring.*)" "$PROFILE_DIR/cpu.prof" 2>/dev/null || echo "No config/keyring functions in profile"
    echo ""
fi

if [ -f "$PROFILE_DIR/mem.prof" ]; then
    echo "=== Memory Profile - Top 20 Allocations ==="
    echo ""
    go tool pprof -top -alloc_space "$PROFILE_DIR/mem.prof" 2>/dev/null | head -25
    echo ""
fi

echo "=== Summary ==="
echo ""
echo "Profile files saved in $PROFILE_DIR/"
echo ""
echo "For interactive analysis:"
echo "  # CPU profile (shows what's taking time):"
echo "  go tool pprof -http=:8080 $PROFILE_DIR/cpu.prof"
echo ""
echo "  # Memory profile (shows what's allocating):"
echo "  go tool pprof -http=:8080 $PROFILE_DIR/mem.prof"
echo ""
echo "  # Execution trace (shows goroutine scheduling, blocking):"
echo "  go tool trace $PROFILE_DIR/trace.out"
echo ""
echo "  # Generate a call graph:"
echo "  go tool pprof -pdf $PROFILE_DIR/cpu.prof > $PROFILE_DIR/cpu_graph.pdf"
