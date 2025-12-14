# AGENTS.md

## Language
Go 1.25+

## Build Commands
```bash
just install          # Install dependencies
just build            # Build binary
just run              # Run with debug logging
just bootstrap        # Install dev tools (golangci-lint, goimports)
```

## Test Commands
```bash
just test             # Run all tests
just test-coverage    # Run tests with coverage
go test -v -run TestName ./...  # Run single test
```

## Lint/Format
```bash
just lint             # Run golangci-lint
just fmt              # Format with go fmt + goimports
```

## Code Style
- **Imports**: stdlib first, then external, then local (goimports handles this)
- **Formatting**: `go fmt` standard
- **Types**: Use strong typing, avoid `interface{}` unless necessary
- **Naming**: camelCase for private, PascalCase for exported, short meaningful names
- **Errors**: Return errors, don't panic; wrap with context using `fmt.Errorf("context: %w", err)`
- **Structure**: Flat layout, avoid creating new directories/files unless essential

## Conventions
- Use `slog` for logging (not fmt.Println)
- Never shell out to git/podman - use Go libraries
- All project files under `.agents/` directory
- Present progressive tense in commits: "adding feature" not "added feature"

## Container
Configure sandbox image in `.agents/asimi.conf` under `[run_shell_command]` section.
