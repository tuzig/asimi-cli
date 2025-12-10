# Changelog

All [Semantic Versions](https://semver.org/spec/v2.0.0.html) of this project and their notable changes are documented in the file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), with
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- Markdown renderer is on by defaul
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
- Configuration option `run_in_shell.timeout_minutes` to set shell command timeout (default: 10 minutes)
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
