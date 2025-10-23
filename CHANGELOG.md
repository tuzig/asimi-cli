# Changelog

All [Semantic Versions](https://semver.org/spec/v2.0.0.html) of this project and their notable changes are documented in the file. 

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), with 
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).



## [Unreleased]

### Fixed
- Respecting `PodmanAllowHostFallback` configuration in non-Podman builds so shell commands fail when Podman is unavailable and fallback is disabled

### Changed
- Reorganized persistent data under `~/.local/share/asimi/repo/<slug>/` so each repository has isolated history and session storage with automatic migration from the legacy layout
- Moved the shell tool into a Podman-managed container that mounts the worktree, runs `just bootstrap`, and captures output safely with a host fallback when Podman is unavailable
- Added a `merge` tool that reviews changes in lazygit, squashes the feature branch onto main, and cleans up the worktree automatically
- Reformatted the system prompt environment details into a markdown section so tooling context is clearer to read
- Updated chat to display user messages without the `You:` label and indent them for a cleaner conversation view
- Prevented keystroke lag in the TUI by refreshing go-git status only when prompts are sent or responses complete, instead of recalculating on every render
- Fixed slow startup by initializing LLM client and session asynchronously. The UI now appears immediately and is responsive while the LLM client is being set up in the background
- Fixed arrow keys not working in vi NORMAL mode - navigation keys (arrow keys, h/j/k/l, w/b, etc.) now properly work for cursor movement in NORMAL mode
- Restored history  defaults (enabled by default, preserving max history settings) and added a bool pointer helper so configuration and TUI tests build again
- Fixed arrow keys not working in vi NORMAL mode - navigation keys (arrow keys, h/j/k/l, w/b, etc.) now properly work for cursor movement in NORMAL mode
- Clearing toast notifications when starting a new prompt so stale messages from previous operations do not linger
- Restored history  defaults (enabled by default, preserving max history settings) and added a bool pointer helper so configuration and TUI tests build again
- Added `/export` command with support for two export types: `/export conversation` (default) provides a slimmer output with just user/assistant exchanges; `/export full` includes system prompt, context files, and full conversation with tool calls
- Prompt history now persists across sessions and app activations. History is stored in `~/.local/share/asimi/history.json` (up to 1000 entries). Added `/clear-history` command to clear all saved history. Duplicate consecutive prompts are automatically filtered out
- Status bar left section now shows the condensed git status beside the branch name (e.g. `[$!?⇡]`) so repository state is always visible at a glance
- Replaced shell git calls with an asynchronous go-git manager, eliminating external git command executions while keeping the status bar responsive
- Converted the new waiting and history helper functions in `tui.go` into `TUIModel` methods to keep the model API consistent across the codebase
- Status bar now recomputes the current git branch during render, reflecting branch changes without restarting the TUI
- Added `TUIModel.initHistory()` helper so prompt history resets stay consistent when starting new sessions
- Cached Git status data for the status bar so the TUI no longer freezes on startup while repeatedly invoking `git`
- Fixed arrow key history navigation to only trigger when cursor is on the first line (up arrow) or last line (down arrow), allowing proper multi-line editing in the prompt
- Fixed tool activation reporting so every `tool_use` is followed by a matching `tool_result`, preventing Anthropic 400 errors and keeping execution logs consistent, and added configurable `max_loop` (default 999) to tune the loop guard without breaking Anthropic conversations.
- Tool activation messages now swap the hollow status icon for a filled indicator on success and show assistant replies with the `Asimi:` prefix in the chat window.
- Added a status bar wait timer that appears after three seconds of silence and counts how long we have been waiting for the next model response.
- Arrow keys now browse prompt history, letting you roll back the conversation to earlier prompts, edit them, and resend without losing context.
- Help command output now shows the active command leader (`:` in vi mode, `/` otherwise) so users always see the correct prefix for commands
- Vi mode submode indicator now lives in the status bar instead of beneath the prompt, reducing prompt clutter while still showing `-- INSERT --`, `-- NORMAL --`, etc.
- Vi mode Command-line mode now uses normal (non-vi) editing keybindings, making it behave like when vi mode is disabled. This allows for easier command editing with standard keybindings (arrow keys, backspace, etc.)
- Fixed command completion dialog to work with both `/` and `:` prefixes. When typing `:` in vi mode, the completion dialog now properly filters and displays commands with the `:` prefix, and updates as you type
- Pressing `:` in vi normal mode now shows the completion dialog immediately with all available commands
- Add configuration option `vi_mode` to disable vi mode (default: enabled). Can be set in config file with `vi_mode = false` under `[llm]` section or via environment variable `ASIMI_LLM_VI_MODE=false`
- Implementing proper modal vi mode with insert and normal modes. Press `Esc` to switch from insert to normal mode, and `i`, `a`, `I`, `A`, `o`, `O` to enter insert mode. Visual indicators show current mode: green border for insert mode (-- INSERT --), yellow border for normal mode (-- NORMAL --). Vi normal mode supports navigation keys (h/j/k/l, w/b, 0/$, gg/G) and editing commands (x, d, D). Commands can be entered with `:` in vi mode
- Add markdown styling to the chat window using glamour library. AI messages now render markdown with proper formatting (bold, italic, code blocks, etc.)
- Support CTRL-Z for sending asimi to work in the background. Displays message "Asimi will be running in the background now. Use `fg` to restore"
- Add `/vi` command for switching line editing to vi mode. When enabled, use `:` instead of `/` to specify commands. Border color changes to yellow to indicate vi mode is active. Vi mode now includes full vi-style keybindings for text editing (h/j/k/l for navigation, w/b for word movement, 0/$ for line start/end, etc.)
- Feature: `/resume` command that lists last sessions and lets the user choose which session to resume. Session persistence is now enabled by default.
- Enhancement: added visible "Cancel" option to the resume dialog that can be navigated to and selected with Enter
- Bug fix: sessions are now automatically saved when quitting (via `/quit` or Ctrl+C) to ensure they can be resumed later
- Enhancement: store sessions under project-specific directories to enforce per-project limits
- Bug fix: restoring saved session message parts so resumes rebuild chat history
- Bug fix: when the user scrolls the chat window stop autoscrolling
- bug fix: deleting the current prompt line uses the new textarea cursor metadata instead of the removed `CursorPosition`, avoiding runtime errors
- Feature: display thinking
- replace the color scheme with Terminal7's colors:

```css
:root {
    --prompt-border: #F952F9;
    --chat-border: #F4DB53;
    --text-color: #01FAFA;
    --warning: #F4DB53;
    --error: #F54545;
    --prompt-background: #271D30;
    --chat-background: #11051E;
    --text-error: #004444;
    --pane-background: black;
    --dark-border: #373702;
}
```

- bug fix: Status bar should not overflow so complete refactoring:
-- left: 🪾<branch_name> main branch should be collored yellow, green otherwise
-- mid: <shorten git status> e.g, `[$!?]`
-- right: <provider status icon><shorten provider and model. e.g. "Claude-4"
- 
- Support touch gesture for scrolling the chat
- bug fix: status bar shows distinct labels for Claude 4 and Claude 4.5 models
- Move the log file in `~/.local/share/asimi/log` and prevent it from exploding by adding `gopkg.in/natefinch/lumberjack.v2`
- Fix the /new command so it'll clear the context and not just the screen
- Implement the `/context` command with token breakdown, bar visualization, and command output per `specs/context_command.md`
- Integrate langchaingo's model context size database for more accurate OpenAI model support
- Use langchaingo's tiktoken-based token counting for OpenAI models when available
