# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Language & Philosophy

**Language**: Go

**Write idiomatic Go** - Keep it simple, flat, and direct.

- **Flat structure**: Avoid creating new directories/files unless essential
- **No wrappers**: Avoid unnecessary abstractions
- **No build tags**: Keep builds simple
- **Short, meaningful names**: Follow Go conventions
- **Inline comments**: Only for non-trivial code

## Common Commands

```bash
# Development
just run              # Run with debug logging (writes to ./asimi.log)
just build            # Build binary
just test             # Run all tests
just test-coverage    # Run tests with coverage report
just lint             # Run golangci-lint
just fmt              # Format code

# Infrastructure
just bootstrap        # Install dev tools (golangci-lint, goimports)
just build-sandbox    # Build container image for sandboxed execution
just clean-sandbox    # Clean up container image

# Performance analysis
just measure          # Profile startup performance

# Run single test
go test -v -run TestName ./...
```

## Architecture Overview

Asimi is a vi-inspired, terminal-based AI coding agent with containerized shell execution.

### Core Components

**TUI Layer** (`tui.go`, `models.go`)
- Bubble Tea-based terminal UI with vi-style keybindings
- Components: StatusComponent, PromptComponent, ContentComponent, CommandLineComponent
- Modes: Insert, Normal, Command (mimics vi/vim/neovim)
- Modal system for provider selection and code input

**Session Management** (`session.go`, `storage/`)
- Session: Manages LLM conversation state, tool execution, token counting
- SQLite-backed persistence with schema at `storage/schema.go`
- Hierarchy: Repository → Branch → Session → Messages
- Prompt and command history stored per-branch
- Uses langchaingo's `llms.Model` directly for streaming and tool calls

**Containerized Shell** (`podman_runner.go`)
- PodmanShellRunner: Executes agent commands in isolated container
- Persistent bash session for speed (>100x faster than alternatives)
- Shares host working directory as volume mount
- NEVER shell out to podman/docker commands - use podman bindings directly
- Container lifecycle: initialize → run commands → cleanup (unless --no-cleanup)

**Tool System** (`tools.go`, `scheduler.go`)
- Tools: ReadFileTool, WriteFileTool, EditFileTool, RunShellCommandTool, etc.
- CoreToolScheduler: Parallel tool execution with semaphore-based rate limiting
- Path validation prevents access outside project directory
- Shell commands run in container by default (configurable via run_on_host regex)

**Commands** (`commands.go`)
- Slash commands: /help, /new, /login, /models, /context, /resume, /export, /init, /compact
- Command registry supports vim-style prefix matching
- Two prompt files: `prompts/initialize.txt` (project setup), `prompts/compact.txt` (conversation compression)

**Configuration** (`config.go`)
- Koanf-based TOML configuration
- User config: `~/.config/asimi/asimi.toml`
- Project config: `.agents/asimi.toml`
- Environment variables override config (e.g., ANTHROPIC_OAUTH_TOKEN, ASIMI_LLM_VI_MODE)

**Authentication** (`login.go`, `keyring.go`)
- OAuth flow for Anthropic (browser-based)
- API keys stored in OS keyring
- Token refresh handled automatically during sessions

### Key Libraries

- `langchaingo` - LLM communications and tools (use llms.Model directly, not chains)
- `bubbletea` - Terminal UI framework
- `koanf` - Configuration (TOML files + env vars)
- `kong` - CLI parsing
- `go-git` - Git operations (NEVER shell out to git)
- `podman bindings` - Container management (NEVER shell out to podman/docker)
- `slog` - Structured logging (use --debug flag for verbose output)
- `glamour` - Markdown rendering (optional, disabled by default for performance)

## Testing Patterns

- Tests use `_test.go` suffix
- Mock keyring via `SetTestingKeyringService()` to avoid clearing production tokens
- Use `t.TempDir()` for isolated test environments
- Test files should test their corresponding implementation file (e.g., `scheduler_test.go` tests `scheduler.go`)

## Configuration Files

- **Logs**: `~/.local/share/asimi/asimi.log` (production) or `./asimi.log` (--debug mode)
- **Config**: `~/.config/asimi/asimi.toml` (user) or `.agents/asimi.toml` (project)
- **Database**: `~/.local/share/asimi/asimi.sqlite` (see `storage/schema.go`)
- **Project files**: All Asimi-specific files live under `.agents/` directory
- **Container image**: Configured in `.agents/asimi.conf` under `[run_shell_command]` section as `image_name`

## Release Workflow

1. Follow **SemVer** (major.minor.patch)
2. Update `CHANGELOG.md` with: Added, Changed, Fixed, Removed
3. Use present progressive tense in commits: "adding feature" not "added feature"
4. Update version in `main.go` (var `version`)
5. Git tag release to sync code and changelog
6. **DON'T MERGE** - ask user for approval

## Important Rules

- NEVER create new directories/files unless absolutely necessary
- NEVER shell out to git/podman/docker - use Go libraries
- ALWAYS run tests after changes: `just test`
- ALWAYS update CHANGELOG.md when changes are complete and tests pass
- Use `slog` for logging (avoid fmt.Println except for user-facing output)
- Container commands run in sandbox by default (see `run_shell_command.run_on_host` config to override)
