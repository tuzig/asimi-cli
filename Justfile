run: modules
    go run .

modules:
    go mod vendor

# run the tests
test: modules
    go test -v ./...

build: modules
    go build -o asimi .

dlv:
    go install github.com/go-delve/delve/cmd/dlv@latest

debug: dlv
    dlv --listen=:2345 --headless=true --api-version=2 --accept-multiclient exec ./asimi -- --config config/default.toml

# profile startup performance
profile: build
    @echo "=== Profiling Asimi Startup ==="
    @echo ""
    @mkdir -p profiles
    @rm -f profiles/*
    @echo "Running with profiling (auto-exits after 2 seconds)..."
    @echo ""
    ./asimi --debug --cpu-profile=profiles/cpu.prof --mem-profile=profiles/mem.prof --trace=profiles/trace.out --profile-exit-ms=2000 2>&1 | tee profiles/timing.log || true
    @echo ""
    @echo "=== Timing Analysis ==="
    @echo ""
    @grep "\[TIMING\]" profiles/timing.log 2>/dev/null || echo "No timing data found"
    @echo ""
    @echo "=== CPU Profile - Top 20 Functions ==="
    @echo ""
    @go tool pprof -top -cum profiles/cpu.prof 2>/dev/null | head -25 || echo "No CPU profile data"
    @echo ""
    @echo "=== Memory Profile - Top 15 Allocations ==="
    @echo ""
    @go tool pprof -top -alloc_space profiles/mem.prof 2>/dev/null | head -20 || echo "No memory profile data"
    @echo ""
    @echo "=== Profile files saved in profiles/ ==="
    @echo ""
    @echo "For interactive analysis:"
    @echo "  go tool pprof -http=:8080 profiles/cpu.prof"
    @echo "  go tool pprof -http=:8080 profiles/mem.prof"
    @echo "  go tool trace profiles/trace.out"

# open CPU profile in web browser
profile-cpu: profile
    go tool pprof -http=:8080 profiles/cpu.prof

# open memory profile in web browser
profile-mem: profile
    go tool pprof -http=:8080 profiles/mem.prof

# open execution trace viewer
profile-trace: profile
    go tool trace profiles/trace.out
