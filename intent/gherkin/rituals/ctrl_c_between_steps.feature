Feature: CTRL-C Between Ritual Steps — Graceful Pause, Not Hard Kill
  As a Ruler running a ritual
  I want pressing CTRL-C between steps to pause the ritual gracefully
  So that I can chat with the minister and then :continue or :abort

  Background:
    Given a ritual with multiple steps
    And the ritual is running on a channel
    And the first step has completed
    And the second step has not yet started

  Scenario: Main loop checks pause between steps
    Given the ritual is between steps (CurrentStep < len(Steps))
    And a pause channel exists for the ritual's channel
    When the main loop reaches the next iteration
    Then waitIfPaused blocks before executing the next step
    And the next step is not executed until the pause is cleared

  Scenario: CTRL-C between steps enters chat mode
    Given the ritual is between steps
    When the Ruler presses CTRL-C
    Then the TUI calls PauseRitual which returns true
    And the TUI enters chat mode
    And the Ruler can type messages to the minister

  Scenario: :continue resumes the ritual after between-step pause
    Given the ritual is paused between steps
    And the Ruler is chatting with the minister
    When the Ruler types ":continue"
    Then ResumeRitual is called for the channel
    And waitIfPaused unblocks
    And the ritual resumes executing the next step

  Scenario: :abort cleanly aborts the ritual after between-step pause
    Given the ritual is paused between steps
    And the Ruler is chatting with the minister
    When the Ruler types ":abort"
    Then the parent context is cancelled
    And waitIfPaused returns ctx.Err
    And the ritual execution state is set to aborted
    And the execution is saved
    And the TUI shows the ritual as aborted