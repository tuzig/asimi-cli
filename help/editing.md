# Editing Commands

Editing commands work in NORMAL mode.

## Entering INSERT Mode

  i        - Insert before cursor
  I        - Insert at beginning of line
  a        - Append after cursor
  A        - Append at end of line
  o        - Open new line below and insert
  O        - Open new line above and insert

## Multi-line Input

  Alt+Enter (Option+Enter on Mac) - Insert newline without submitting
  Enter    - Submit prompt

## Deletion

  x        - Delete character under cursor
  X        - Delete character before cursor
  dw       - Delete word
  dd       - Delete line
  D        - Delete to end of line

## Copying and Pasting

  p        - Paste after cursor
  P        - Paste before cursor

## Undo and Redo

  u        - Undo last change
  Ctrl+r   - Redo

## Special Features

  @        - Start file reference (triggers file completion)
  #        - Enter LEARNING mode (add note to AGENTS.md)

## File References

Type @ followed by a filename to reference a file in your prompt:

  @main.go          - Reference main.go
  @src/utils.go     - Reference file in subdirectory

A completion dialog will appear showing matching files. Use:
  ↓/↑ or Tab       - Navigate completions
  Enter            - Select file
  ESC              - Cancel

## Learning Mode

Press # in NORMAL mode to enter LEARNING mode. Type a note and press Enter
to append it to AGENTS.md. This is useful for teaching Asimi about your
project conventions and preferences.

Example:
  # We use snake_case for function names in this project
