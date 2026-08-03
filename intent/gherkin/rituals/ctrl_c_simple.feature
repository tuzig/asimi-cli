Feature: CTRL-C Simple — Pause/Resume Primitive
  As a ritual runner
  I want PauseRitual and ResumeRitual to work correctly
  So that the contract between TUI and runner is clear

  Background:
    Given a ritual runner exists

  Scenario: PauseRitual returns true when no step is running
    Given no step cancel is registered for the channel
    When PauseRitual is called for the channel
    Then PauseRitual returns true
    And a pause channel is created for the channel
    And the ritual goroutine is not cancelled

  Scenario: PauseRitual returns false when already paused
    Given a pause channel already exists for the channel
    When PauseRitual is called again for the same channel
    Then PauseRitual returns false

  Scenario: PauseRitual still cancels active step when step is running
    Given a step cancel is registered for the channel
    And a step is actively running
    When PauseRitual is called for the channel
    Then PauseRitual returns true
    And the active step's context is cancelled
    And a pause channel is created

  Scenario: ResumeRitual returns false when nothing is paused
    Given no pause channel exists for the channel
    When ResumeRitual is called for the channel
    Then ResumeRitual returns false

  Scenario: waitIfPaused returns nil when resumed
    Given the ritual is paused between steps
    When ResumeRitual is called for the channel
    Then waitIfPaused unblocks and returns nil
    And clearPause removes the pause channel
    And the ritual continues to execute the next step

  Scenario: waitIfPaused returns ctx.Err when context is cancelled
    Given the ritual is paused between steps
    When the parent context is cancelled (abort)
    Then waitIfPaused unblocks and returns ctx.Err
    And the ritual execution state is set to aborted
    And the execution is saved