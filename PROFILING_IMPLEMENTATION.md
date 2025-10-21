# Profiling Implementation Summary

## What Was Done

Added comprehensive Go profiling support to Asimi CLI to identify and analyze startup performance bottlenecks.

## Changes Made

### 1. Command-Line Flags (main.go)

Added profiling flags:
- `--cpu-profile=FILE` - Write CPU profile for performance analysis
- `--mem-profile=FILE` - Write memory allocation profile
- `--trace=FILE` - Write execution trace for goroutine/scheduler analysis
- `--profile-exit-ms=N` - Auto-exit after N milliseconds (for automated profiling)
- `--debug` - Enhanced with detailed timing instrumentation

### 2. Timing Instrumentation

Added detailed timing logs throughout the startup path:
- `main()` entry point
- `initLogger()` 
- `LoadConfig()`
- `NewTUIModel()`
- `tea.NewProgram()`
- `getLLMClient()` (async)
- `NewSession()` (async)
- `program.Run()`

### 3. Helper Scripts

Created automated profiling scripts:
- `profile_startup_auto.sh` - Automated profiling with analysis output
- `profile_startup.sh` - Interactive profiling with trace viewer
- `profile_startup_quick.sh` - Quick profiling without PTY

### 4. Documentation

Created `PROFILING_RESULTS.md` with:
- Detailed timing breakdown
- Memory allocation analysis
- CPU profile findings
- Recommendations for optimization
- Usage examples

## Key Findings

### Timing Breakdown
```
initLogger():     ~0.8ms
LoadConfig():     ~0.2ms
NewTUIModel():    ~10ms   ⚠️ Main bottleneck
tea.NewProgram(): ~0.004ms
getLLMClient():   ~28ms   (async - doesn't block UI)
NewSession():     ~0.4ms  (async)
Total to UI:      ~10ms   ✅ Actually quite fast!
```

### Memory Allocations

Top allocators during startup:
1. **8.2MB** - ANSI parser (charmbracelet/x/ansi)
2. **2.6MB** - Buffer allocations
3. **1.8MB** - Runtime trace overhead
4. **1.2MB** - CPU profiler overhead
5. **1MB+** - Chroma syntax highlighting

### CPU Usage

- Only 60ms CPU time in 2.2s duration (2.7% CPU)
- Most time is I/O and initialization, not CPU-bound
- Async LLM initialization is working well

## Recommendations

### Immediate Optimizations

1. **Lazy-load Chroma styles** - Don't initialize all syntax highlighting upfront
2. **Reduce ANSI parser buffer** - 8.2MB seems excessive for initial allocation
3. **Defer non-critical init** - Move history/session store to background

### Further Investigation

1. Use `go tool trace` to analyze goroutine blocking
2. Add benchmarks for critical functions
3. Profile with different terminal sizes

## Usage

### Quick Profiling
```bash
./profile_startup_auto.sh
```

### Manual Profiling
```bash
./asimi --debug \
  --cpu-profile=cpu.prof \
  --mem-profile=mem.prof \
  --trace=trace.out \
  --profile-exit-ms=2000
```

### Interactive Analysis
```bash
# CPU profile (web UI)
go tool pprof -http=:8080 cpu.prof

# Memory profile (web UI)
go tool pprof -http=:8080 mem.prof

# Execution trace
go tool trace trace.out

# Generate call graph
go tool pprof -pdf cpu.prof > cpu_graph.pdf
```

## Conclusion

The async LLM initialization (already implemented) is working excellently. The UI appears in ~10ms, which is actually quite fast for a feature-rich TUI application. The remaining time is mostly unavoidable initialization overhead from terminal setup, UI components, and syntax highlighting.

The profiling infrastructure is now in place for ongoing performance monitoring and optimization.
