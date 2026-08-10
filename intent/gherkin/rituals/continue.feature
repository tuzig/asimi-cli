Feature: Continue Command — Resume or Enact Swift-Strike
  As a Ruler
  I want :continue to resume a paused ritual or enact a fresh swift-strike
  So that I can seamlessly continue work without remembering the :edict incantation

  Background:
    Given a ritual tab exists for edict 647

  # --- Paused: resume (existing behavior) ---

  Scenario: Resume paused ritual
    Given the ritual is paused (ChatMode = true)
    When the Ruler types ":continue"
    Then ResumeRitual is called for the channel
    And ChatMode is cleared
    And a system message says "▶ Ritual resuming..."

  # --- Not paused: enact swift-strike (new behavior) ---

  Scenario: Enact swift-strike when not paused
    Given the ritual is not paused
    And the edict is active (not sealed, not cancelled)
    When the Ruler types ":continue"
    Then EventRitualEnacted is published with ritual "swift-strike" for edict 647

  # --- Edge cases ---

  Scenario: Warn on sealed edict
    Given the ritual is not paused
    And the edict has the Ruler's seal
    When the Ruler types ":continue"
    Then a toast warning says "Edict 647 is already sealed"
    And no ritual event is published

  Scenario: Warn on cancelled edict
    Given the ritual is not paused
    And the edict is cancelled
    When the Ruler types ":continue"
    Then a toast warning says "Edict 647 is cancelled"
    And no ritual event is published

  Scenario: Warn on non-existent edict
    Given the ritual is not paused
    And the edict 999 does not exist
    When the Ruler types ":continue"
    Then a toast error says "Edict 999 not found"
    And no ritual event is published

  Scenario: Warn on non-ritual tab
    Given the active tab is not a ritual tab
    When the Ruler types ":continue"
    Then a toast warning says ":continue only works on a ritual tab"

  Scenario: Warn when court is not active
    Given the court is not active
    And the ritual is paused (ChatMode = true)
    When the Ruler types ":continue"
    Then a toast error says "Court not active"