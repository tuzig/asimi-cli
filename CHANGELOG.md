# Changelog

All [Semantic Versions](https://semver.org/spec/v2.0.0.html) of this project and their notable changes are documented in the file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), with
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.5.0] - 2025-01-20

### Added

- Supporting `r` key in normal mode to replace character under cursor (#76)

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
