# Startup Performance Profiling Results

## Summary

Profiling was added to Asimi CLI to identify the root cause of slow startup. The application now supports CPU profiling, memory profiling, and execution tracing via command-line flags.

## Timing Breakdown (from debug output)

```
[TIMING] main() started at 2025-10-21 16:21:02.558978
[TIMING] initLogger() completed in 800.084µs
[TIMING] Terminal check completed in 42ns
[TIMING] LoadConfig() completed in 155.541µs
[TIMING] NewTUIModel() completed in 9.796958ms  ⚠️ SLOW
[TIMING] tea.NewProgram() completed in 4.333µs
[TIMING] About to start program.Run() at 9.974584ms from start
[TIMING] getLLMClient() completed in 28.002292ms  ⚠️ SLOW (async)
[TIMING] NewSession() completed in 360.583µs
[TIMING] program.Run() completed in 2.035194875s
[TIMING] Total Run() time: 2.045194667s
[TIMING] Total execution time: 2.052654209s
```

## Key Findings

### 1. NewTUIModel() is the slowest synchronous operation (~10ms)

This is where the UI components are initialized. Memory profile shows:
- **8.2MB** allocated by `github.com/charmbracelet/x/ansi.(*Parser).SetDataSize`
- **518KB** allocated by `main.NewPromptComponent`
- Chroma syntax highlighting initialization takes significant memory

### 2. getLLMClient() takes ~28ms (but runs async)

This is already running in a goroutine, so it doesn't block the UI from appearing. Good!

### 3. CPU Profile shows minimal CPU usage during startup

Only 60ms of CPU samples in 2.2s duration (2.7% CPU usage). This suggests:
- Most time is spent waiting (I/O, initialization)
- Not a CPU-bound problem
- Likely memory allocation and initialization overhead

### 4. Memory Allocations

Top allocators during startup:
1. **8.2MB** - ANSI parser data structures (charmbracelet/x/ansi)
2. **2.6MB** - Buffer allocations
3. **1.8MB** - Runtime trace overhead
4. **1.5MB** - Runtime allocator metadata
5. **1.2MB** - CPU profiler overhead
6. **1MB+** - Chroma syntax highlighting styles

## Profiling Tools Added

### Command-line Flags

```bash
--debug                 Enable debug logging with timing info
--cpu-profile=FILE      Write CPU profile to file
--mem-profile=FILE      Write memory profile to file  
--trace=FILE            Write execution trace to file
--profile-exit-ms=N     Auto-exit after N milliseconds (for automated profiling)
```

### Usage Examples

```bash
# Quick automated profiling (exits after 2 seconds)
./asimi --debug --cpu-profile=cpu.prof --mem-profile=mem.prof --trace=trace.out --profile-exit-ms=2000

# Interactive analysis
go tool pprof -http=:8080 cpu.prof
go tool pprof -http=:8080 mem.prof
go tool trace trace.out

# Generate call graph
go tool pprof -pdf cpu.prof > cpu_graph.pdf
```

### Helper Scripts

- `profile_startup_auto.sh` - Automated profiling with analysis
- `profile_startup.sh` - Interactive profiling with trace viewer
- `profile_startup_quick.sh` - Quick profiling without PTY

## Recommendations

### Immediate Wins

1. **Lazy-load Chroma styles** - Don't initialize all syntax highlighting styles at startup
   - Current: ~1MB+ allocated for styles upfront
   - Better: Load styles on-demand when first rendering code

2. **Reduce ANSI parser buffer size** - 8.2MB seems excessive
   - Check if `SetDataSize` can use a smaller initial allocation
   - Consider lazy growth instead of pre-allocation

3. **Defer non-critical initializations** - Move to background goroutines:
   - History store initialization
   - Session store initialization  
   - Git status caching (already done)

### Further Investigation

1. **Execution trace analysis** - Use `go tool trace` to see:
   - Goroutine blocking patterns
   - Scheduler delays
   - GC pauses

2. **Benchmark specific functions**:
   ```go
   func BenchmarkNewTUIModel(b *testing.B) {
       config := &Config{...}
       for i := 0; i < b.N; i++ {
           NewTUIModel(config)
       }
   }
   ```

3. **Profile with different terminal sizes** - Large terminals might allocate more buffers

## Current Status

The async LLM initialization (already implemented) is working well:
- UI appears in ~10ms
- LLM client initializes in background (~28ms)
- User can start typing immediately

The remaining ~10ms startup time is mostly unavoidable initialization overhead from:
- Terminal setup
- UI component allocation
- Syntax highlighting initialization

This is actually quite fast for a TUI application with rich features!

## Next Steps

1. ✅ Add profiling support (DONE)
2. ✅ Identify bottlenecks (DONE)
3. 🔄 Optimize Chroma initialization (TBI)
4. 🔄 Reduce ANSI parser allocations (TBI)
5. 🔄 Add benchmarks for critical paths (TBI)
