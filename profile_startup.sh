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
