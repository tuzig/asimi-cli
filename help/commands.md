# Commands

Commands are executed from COMMAND-LINE mode. Press : in NORMAL mode to enter
COMMAND-LINE mode, then type the command and press Enter.

  :new              - Start a new conversation
  :resume           - Resume a previous session
  :edicts            - Manage edicts (read, enact, seal, resume, cancel)
  :continue         - Resume a paused ritual on the current tab
  :abort            - Abort a paused ritual on the current tab
  :quit / :q       - Close current tab (quit app if last tab)
  :qa / :quitall    - Quit the application (all tabs)
  :update           - Check for and install updates

## Information

  :help [topic]     - Show help (optionally for a specific topic)
  :context          - Show context usage and token information

## History

  :export [type]    - Export conversation to file and open in $EDITOR
                      Types: conversation (default), full

## Configuration

  :models           - Select AI model
  :login            - Authenticate with an AI provider

  :init [clean]     - Initialize project with infrastructure files
                      Creates: AGENTS.md, Justfile, .agents/Sandbox

## Examples

  :help modes       - Show help about vi modes
  :new              - Start fresh conversation
  :export           - Export and edit conversation
  :context          - Check token usage
