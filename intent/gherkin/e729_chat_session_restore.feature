Feature: Edict Chat Session Restoration — User Prompt Visibility
  As a Ruler browsing edicts
  I want my typed prompts to remain visible in the chat after session restoration
  So that I can see my own messages in the conversation history

  Background:
    Given an edict with a birth session ID
    And the Ruler is on the ritual tab for that edict

  # --- Chat action (dispatchEdictAction "Chat") ---

  Scenario: Chat action restores session without submitting a prompt
    Given the Ruler selects "Chat" from the edict action menu
    When the edict's birth session is restored
    Then the active tab stays on the ritual tab
    And the session messages are rebuilt in the chat
    And no prompt is submitted to the minister
    And the current edict key is set
    And pending edict fields are cleared
    And a toast confirms "Resumed session for edict"

  # --- Typed prompt on restored session (THE BUG) ---

  Scenario: Ruler's typed prompt stays visible after session restore
    Given the edict's birth session was restored via the Chat action
    When the Ruler types "fix the tests" and presses Enter
    Then AddUserMessage is called with "fix the tests" (line 1984)
    And the court detects an edict with a birth session
    And pendingEdictPrompt is set to "fix the tests"
    And LoadSession triggers session restoration
    And handleSessionSelected is called with hasPrompt=true
    And Chat.Clear() wipes the chat
    And rebuildChatFromMessages restores saved messages
    And AddUserMessage("fix the tests") is called to restore the prompt visibility
    And the prompt "fix the tests" is submitted to the minister
    And the minister's response streams to the edict tab
    And pendingEdictPrompt is cleared

  # --- No birth session ---

  Scenario: Prompt on edict tab without birth session creates a fresh session
    Given the edict has no birth session
    When the Ruler types a prompt on the ritual tab
    Then the prompt is submitted to the minister directly
    And a system message says "Started new session for edict"
    And the response streams to the edict tab

  # --- Active ritual interjection ---

  Scenario: Prompt on ritual tab with active ritual pauses the ritual
    Given an active ritual is running on the edict tab
    When the Ruler types a prompt
    Then the ritual is paused
    And a chat mode system message shows "Now chatting with <minister>"
    And the prompt is submitted to the current ritual minister
    And the response streams to the ritual tab

  # --- Follow-up prompt ---

  Scenario: Follow-up prompt after restore continues the conversation
    Given the edict's birth session was restored
    And the Ruler's prompt was answered by the minister
    When the Ruler types a follow-up prompt
    Then the follow-up is submitted to the same minister session
    And the prior messages remain visible in the chat

  # --- Normal resume (no edict context) ---

  Scenario: Normal session resume switches tab and clears edict context
    Given no pending edict prompt or key
    And the current active tab is the secretary tab
    When a session is selected with TabType "chancellor"
    Then the active tab switches to the chancellor tab
    And currentEdictKey is cleared
    And RestoreMinisterSession is called without a channel ID
    And no prompt is submitted
    And a toast confirms "Resumed session from"