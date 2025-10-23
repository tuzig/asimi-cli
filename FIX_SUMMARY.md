# Fix: Arrow Key History Navigation in Vi Normal Mode

## Problem
Arrow keys (↑/↓) were not cycling through prompt history when in vi normal mode. They only worked in insert mode.

## Root Cause
In `tui.go`, the `handleViNormalMode` function was handling all key presses in vi normal mode, including arrow keys. The arrow keys were being passed directly to the textarea for cursor navigation, bypassing the history navigation logic that was only checked in the default case of `handleKeyMsg`.

## Solution
Modified `handleViNormalMode` to check for history navigation first before passing arrow keys to the textarea:

1. Added explicit handling for "up", "down", "k", and "j" keys at the beginning of `handleViNormalMode`
2. These keys now check if history navigation should be triggered (based on cursor position)
3. If history navigation is handled, it returns early
4. If not handled by history, the keys are passed to the textarea for normal cursor navigation

## Changes Made

### `tui.go`
- Modified `handleViNormalMode` function to handle arrow keys and vi navigation keys (k/j) for history navigation
- History navigation is checked first when on the first line (up/k) or last line (down/j)
- Falls back to normal cursor navigation if history navigation doesn't apply

### `vi_mode_test.go`
- Added `TestViModeHistoryNavigation` to verify arrow keys work for history in vi normal mode
- Added `TestViModeHistoryNavigationWithKJ` to verify k/j keys also work for history navigation
- Tests verify that history navigation works correctly and state is properly saved/restored

## Testing
The fix includes comprehensive tests that verify:
- Arrow keys navigate through history in vi normal mode
- Vi keys (k/j) also work for history navigation
- History state is properly saved and restored
- Cursor position is correctly checked before triggering history navigation

## User Impact
Users can now use arrow keys (↑/↓) or vi keys (k/j) to navigate through prompt history while in vi normal mode, making the vi mode experience more consistent and intuitive.
