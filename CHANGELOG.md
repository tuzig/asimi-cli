# Changelog

All [Semantic Versions](https://semver.org/spec/v2.0.0.html) of this project and their notable changes are documented in the file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), with
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Tab and Shift+Tab keyboard shortcuts for navigating between TUI tabs (e498)

## [0.6.0-rc.1] - 2025-07-11

### Added

- **Daemon mode** — `asimi daemon` subcommand serves the Shogunate over a Unix socket with msgpack-RPC, enabling persistent sessions, multi-project support, and TUI reconnection
- **TUI ↔ Daemon bridge** — bidirectional RPC with `ASIMI_DAEMON=1` auto-start and `ASIMI_DAEMON_SOCKET` for connecting a running TUI to an existing daemon
- **Shogunate (幕府)** — the six-minister governance system with strategist, forge, judge, censor, sage, and marshal roles, orchestrated by the chancellor
- **Rituals** — structured workflows (swift-strike, castle-siege, dawn-audience, review-borderlands, fix-lint) with step-level execution, recovery, and zhengming checkpoints
- **Edict system** — intent-driven work orders with seal chain ascension (judge → sage → ruler), zhengming (正名) clarification flow, and active/blocked/sealed/cancelled status
- **Fork/Join DAG execution** — parallel minister orchestration with dependency resolution
- **Tool Registry & Permission System Phase 1** — governable tool access for ministers
- **Bifrost migration** — replaced langchaingo with Bifrost for LLM provider routing, supporting OpenAI, Anthropic, Bedrock, and Minimax via a unified interface
- **Platform overlay volumes** — mount host directories into the sandbox with proper isolation
- **Sandbox isolation hardening** — ping liveness probe, host network support, and improved container lifecycle
- **Per-tab streaming** — origin-based stream routing with per-tab CTRL-C context cancellation
- **Tab system** — `gt`/`gT` navigation across chancellor, forge, judge, and shogunate tabs
- **`:seal` command** — seal chain verification with edict validation and fresh seal chain display
- **`:export` over the wire** — session export via `GetSessionExport` RPC
- **SetContext handshake** — multi-project daemon support with per-client project context
- **Ritual recovery** — resume interrupted rituals with zhengming confirmation
- **Welcome message** — onboarding greeting on startup
- **Ruler guide** — documentation for the ruler (user) role in the Shogunate
- **RPC protocol reference** — docs for the msgpack-RPC wire protocol
- **Li enforcement** — block destructive git flags (`--no-verify`, `--overwrite-ignore`, `git clean`) in sandbox
- **Asimi attribution headers** — send identification headers to LLM providers
- **Environment variable base URLs** — configure LLM provider endpoints via env vars

### Changed

- **Wire protocol migrated to msgpack-RPC** — standard msgpack-RPC for Neovim compatibility, replacing custom streaming
- **Persistent minister sessions** — ministers reuse conversation context across ritual steps with per-step effort control
- **Sage tools refactor** — consolidated censor into sage, reducing ministers from 6 to 5
- **Init workflow** — converted to ritual-native `project-init` with `:init clear` support
- **Message serialization** — extracted from storage layer to adapter pattern
- **Shogunate branding** — court-themed chat messages and tab styling
- **Config loader** — cleaned up and simplified
- **Stream chunk handling** — folded `StreamReasoningChunkMsg` into `StreamChunkMsg` for unified streaming
- **Podman bumped to 5.8.2**

### Fixed

- Daemon correctly receives working directory and environment variables from client
- RPC connection dropping and automatic reconnection
- Per-client runner isolation — each client uses its own sandbox runner
- Ritual step output rendering and completion handling
- Zhengming routing carries caller MinisterID correctly
- Provider status indicator reflects current state
- Resuming works on all tabs, not just the active one
- Bifrost log messages formatting
- Edict ID assignment on creation
- Bedrock connection initialization
- Court status limited to the current project
- Tool output truncation at proper boundaries
- Context sanitization and partial tool call cleanup
- Daemon liveness probing and eviction of wedged daemons
- Ritual step retry without reloading context
- Multi-project and multi-user session isolation
- Read file size limiting for safety
- Grep tool output correctness
- Tab name color and stream routing to correct tab
- `IsClean()` refreshes status cache before checking
- ANTHROPIC_API_KEY no longer leaks between tests
- Context canceled errors replaced with user-friendly 'aborted by user' message

## [0.5.0] - 2025-01-20

### Added

- Supporting `r` key in normal mode to replace character under cursor (#76)
- Adding tool execution and streaming support to Shogunate Session

### Changed

- Merging `[container]` and `[run_shell_command]` config sections into unified `[sandbox]` section
- Moving config loading functions to `internal/config` package for better code organization
- Replacing Chancellor's `Send()` method with `AskWithStreaming()` for proper tool execution

### Fixed

- Init workflow pre-checks now abort instead of retry when uncommitted changes are detected
- Skiping gitlinks and symlinks when checking for file changes
- Fixing :compact showing duplicate "Compacting..." messages (#121)
- Fixing arrow keys not working in normal mode with empty prompt (#120)
- Ignoring ghost CTRL-C, fix #115

## [0.4.2] - 2026-01-06

### Fixed

- Simplifying Justfile creation on init
- Using the latest langchaingo for OpenAI thoughts
- Retrying in workflow and error handling improvements
- Improving shell runner initialization
- Skipping inaccessible dirs instead of erroring
- Turning on live scroll at bottom
- Preventing file overwrite (#93)

### Changed

- Shortening sandbox id on the status line

## [0.4.1] - 2025-12-29

### Fixed

- .agents/asimi.conf is now properly created, even when there's an exiting CLAUDE.md

## [0.4.0] - 2025-12-28

### Added
- A one char gutter with top & bottom indicators for hidden lines
- One-liner installer script (`curl -fsSL https://asimi.dev/installer | bash`)
- Prompt now grows to `ui.prompt_expanded_height` (default:10) lines when user input is more than one line (including wrapped long text), and returns to 2-line height when in scroll mode or cleared (#31)

### Fixed
- Starting a new session with `:new` now picks up newly built sandbox images without requiring program restart
- Renewing OAuth token on 403 errors
- Clearing message queue on `:init` reinitialize (#107)
- Mouse scroll now properly enters SCROLL mode (#103)
- Background color removed from tool output for cleaner display
- Thinking output now styled consistently with tool output
- Tool denial output formatting improved

## [0.3.2] - 2025-12-15

### Fixed
- CTRL-C debounce time - default shortened to 100ms, added to conf

## [0.3.1] - 2025-12-14

### Added
- Alt+Enter (Option+Enter on Mac) now inserts a newline in the prompt instead of submitting (#101)
- `:logout` command to sign out from the current provider

### Fixed
- Login URL can now be copied
- Status line now fills the entire terminal width when sandbox is showing (#100)
- Resumed sessions now display tool calls exactly as they appeared during the original session (#102)

## [0.3.0] - 2025-01-27

### Changed
- Markdown renderer is on by default
- When no sandbox, use host as fallback
- Command approval now prints the command

### Fixed

- OAuth token refresh now works when server rejects token with 401 error before local expiry time
- Command line prompt now properly dismissed when switching modes

## [0.2.1] - 2025-01-15

### Fixed

- Closing instructions of :init
- Unified tool output
- Init verfication output format and strings

## [0.2.0] - 2025-12-04

### Added

- `:update` command to check for and install updates from GitHub releases
- Automatic update checking on startup (non-blocking, 5-second timeout)
- Self-update capability using go-github-selfupdate
- Support for `ANTHROPIC_OAUTH_TOKEN` environment variable to bypass keyring authentication
  - Accepts raw access token format
  - Accepts full JSON format with refresh token and expiry
  - Accepts base64-encoded JSON (useful when copying from macOS Keychain)
- Configuration option `run_shell_command.timeout_minutes` to set shell command timeout (default: 10 minutes)
- `:!` command prefix to run shell commands in the container (e.g., `:!uname -a`)
- `:resume` command to resume previous sessions
- `:init` command to analyze project and generate infrastructure files (AGENTS.md, Justfile, .agents/asimi.conf, Dockerfile)
  - Automatic verification with retry logic (up to 5 attempts)
  - `:init clear` to regenerate all files from scratch
- Per-branch prompt & command history
- `ui.markdown_enabled` configuration toggle to re-enable Glamour-based markdown rendering (defaults to off for faster resizing) (#53)
- Ctrl-B SCROLL mode for the chat viewport with vi-style paging and `:1` to jump to the first message without re-pinning
- Toast notification when container is launched (#77)

### Changed
- `:init` command now automatically retries with AI-generated fixes when verification fails

### Fixed
- OAuth token now automatically refreshes during chat sessions to prevent 401 errors when token expires mid-conversation
- Context validation error when interrupting tool execution (issue #37)
- Shell command timeouts now properly reported (exit code 124)
- Container connection failures now trigger automatic restart and retry
- Enter now submits prompts directly from vi normal mode when the prompt is non-empty (#32)
- ESC in NORMAL mode now switches to INSERT mode (#70)
- Prompt placeholder now shows helpful navigation hints in RESUME & MODEL modes (#69)
- Conversation history is now automatically compacted when context usage exceeds 90% (#54)
- Model thinking/reasoning messages are now displayed in the chat (e.g., Claude extended thinking) (#38)
- Status line now shows error emoji (❌) when model errors occur mid-conversation (#65)
- Current prompt text is now preserved when navigating history with up/down arrows (#71)

### Removed

- non-vi mode is no longer supported - vi FTW!
- `/` is just a slash. Use `:` to enter command mode



## [0.1.0] - 2025/11/1

A development snapshort made for a friend. Not production ready at all.
