# Asimi CLI - Agent Guide

**Language**: Go 1.25+ | **Build**: Go modules | **Test**: go test + testify

## Build & Run

```bash
just install          # Install dependencies & build
just run              # Run with debug logging
just build            # Build binary only
just test             # Run all tests
just test-coverage    # Tests with coverage report
just lint             # Run golangci-lint
just fmt              # Format code with go fmt
```

## Code Style

**Idiomatic Go** - Simple, flat, dense

- **Imports**: Standard lib → external → internal (goimports handles this)
- **Naming**: Short, meaningful (e.g., `cfg` not `configuration`)
- **Structure**: Flat - avoid new dirs/files unless necessary
- **Comments**: Only for non-obvious code
- **Errors**: Return errors, don't panic (except in main/init)
- **Types**: Prefer explicit over interface{} or any
- **No wrappers**: Avoid unnecessary abstractions
- **No build tags**: Keep builds simple

## Key Libraries

- `slog` - Structured logging (use `--debug` flag)
- `bubbletea` - Terminal UI framework
- `koanf` - Configuration (TOML)
- `kong` - CLI parsing
- `langchaingo` - LLM communications
- `go-git` - Git ops (NEVER shell out to git)
- `podman/v5` - Container management (NEVER shell out)
- `testify/require` - Test assertions

## Testing

```bash
go test ./...                    # Run all tests
go test -v ./... -run TestName   # Run specific test
just test-coverage               # Generate coverage report
```

## Project Conventions

- **Logs**: `./asimi.log` (debug mode) or `~/.local/share/asimi/`
- **Config**: `~/.config/asimi/asimi.toml` or `.agents/asimi.toml`
- **Scratch**: Use `test_tmp/` for temporary files
- **Commits**: Present progressive ("adding X", not "added X")
- **Releases**: SemVer + update CHANGELOG.md before tagging

## Workflow

1. Check `docs/` for context
2. Make changes, run tests: `just test`
3. Update CHANGELOG.md with Fixed/Changed/Added
4. **DON'T MERGE** - ask user for approval

## Infrastructure

```bash
just bootstrap    # Install golangci-lint, setup podman
just infrabuild   # Build dev container (asimi-shell:latest)
just infraclean   # Clean container resources
```

- Logs: ./asimi.log
- Config: `~/.config/asimi/conf.toml` or `.asimi/conf.toml`
- long term Asimi memory is stored in sqlite with the schema at `storage/schema.go`
