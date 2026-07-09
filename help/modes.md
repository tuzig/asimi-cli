# Vi Modes

Asimi uses vi-style modal editing. Each mode has a different purpose and
different key bindings.

## INSERT Mode (Default)

This is the mode you start in. Type normally to compose your message.

  Status: -- INSERT --
  Border: Green (#00FF00)

  Enter INSERT mode from NORMAL mode:
    i    - Insert at cursor
    I    - Insert at beginning of line
    a    - Append after cursor
    A    - Append at end of line
    o    - Open new line below
    O    - Open new line above

  Exit INSERT mode:
    ESC  - Return to NORMAL mode

## NORMAL Mode

Navigation and command mode. Use this to move around and execute commands.

  Status: -- NORMAL --
  Border: Yellow (#F4DB53)

  Enter NORMAL mode:
    ESC  - From INSERT or COMMAND-LINE mode

  From NORMAL mode you can:
    - Navigate with h, j, k, l
    - Enter commands with :
    - Enter INSERT mode with i, a, o, etc.
    - Show quick help with ?

## SCROLL Mode

Scroll through chat history without affecting the prompt.

  Status: -- SCROLL --
  Border: Cyan

  Enter SCROLL mode:
    Ctrl+b       - From INSERT or NORMAL mode
    Mouse wheel  - Scrolling up automatically enters SCROLL mode

  In SCROLL mode:
    j/k or ↓/↑   - Scroll line by line
    Ctrl+d       - Scroll down half page
    Ctrl+u       - Scroll up half page
    Ctrl+f       - Scroll down full page
    Ctrl+b       - Scroll up full page
    G            - Go to bottom
    :            - Enter COMMAND-LINE mode (stays in history)
    ESC or i     - Exit SCROLL mode, return to INSERT

## COMMAND-LINE Mode

Execute commands at the bottom of the screen.

  Status: :
  Border: Magenta (#F952F9)

  Enter COMMAND-LINE mode:
    :    - From NORMAL mode

  In COMMAND-LINE mode:
    Type command and press Enter to execute
    ESC  - Cancel and return to NORMAL mode
    Tab  - Command completion (if available)

## LEARNING Mode

Special mode for adding notes to AGENTS.md.

  Status: -- LEARNING --
  Border: Purple

  Enter LEARNING mode:
    #    - From NORMAL mode

  In LEARNING mode:
    Type your note and press Enter to append to AGENTS.md
    ESC  - Cancel and return to NORMAL mode
