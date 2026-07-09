# Navigation

Navigation commands work in NORMAL mode.

## Basic Movement

  h        - Move left
  j        - Move down
  k        - Move up
  l        - Move right
  ←↓↑→     - Arrow keys also work

## Word Movement

  w        - Move forward to start of next word
  b        - Move backward to start of previous word
  e        - Move forward to end of word

## Line Movement

  0        - Move to beginning of line
  ^        - Move to first non-blank character
  $        - Move to end of line

## Document Movement

  gg       - Go to first line
  G        - Go to last line

## History Navigation

  ↑        - Previous prompt in history (when on first line)
  ↓        - Next prompt in history (when on last line)
  k        - Previous prompt (in NORMAL mode, when on first line)
  j        - Next prompt (in NORMAL mode, when on last line)

## Chat Scrolling (SCROLL Mode)

  Ctrl+b       - Enter SCROLL mode (from INSERT/NORMAL)
  Mouse wheel  - Scroll chat history (auto-enters SCROLL mode)
  Touch gestures - Scroll on touch devices

  In SCROLL mode:
    j/k or ↓/↑   - Scroll line by line
    Ctrl+d       - Scroll down half page
    Ctrl+u       - Scroll up half page
    Ctrl+f       - Scroll down full page
    Ctrl+b       - Scroll up full page
    G            - Go to bottom
    :            - Enter COMMAND-LINE mode (stays in history)
    ESC or i     - Exit SCROLL mode, return to INSERT

## Help Navigation

When viewing help:
  j/k or ↓/↑       - Scroll line by line
  Ctrl+d           - Scroll down half page
  Ctrl+u           - Scroll up half page
  Ctrl+f           - Scroll down full page
  Ctrl+b           - Scroll up full page
  g                - Go to top
  G                - Go to bottom
