Feature: Skills Discovery — Progressive Disclosure for Ministers
  As a minister
  I want skills relevant to my role to appear in my scratchpad
  So that I can load specialized knowledge on demand without context bloat

  Background:
    Given a project root with a ".agents/skills" directory

  # --- No skills directory ---

  Scenario: Empty skills directory produces no skills
    Given the skills directory is empty
    When Discover is called
    Then the result map is non-nil and empty

  # --- Skill scoped to a specific minister ---

  Scenario: Minister-specific skill appears only for that minister
    Given a skill "go-testing" with ministers "[forge]"
    When Discover is called
    Then ForMinister("forge") returns 1 skill named "go-testing"
    And ForMinister("judge") returns 0 skills
    And the result map has no "*" key

  # --- Unscoped skill appears for all ---

  Scenario: Skill without ministers field appears for every minister
    Given a skill "general-skill" without ministers
    When Discover is called
    Then the result map has a "*" key with 1 skill named "general-skill"
    And ForMinister("forge") returns 1 skill named "general-skill"
    And ForMinister("judge") returns 1 skill named "general-skill"
    And ForMinister("war") returns 1 skill named "general-skill"

  # --- Mixed scoped and unscoped skills ---

  Scenario: Mixed skills combine correctly per minister
    Given a skill "go-testing" without ministers
    And a skill "deployment" with ministers "[forge]"
    When Discover is called
    Then ForMinister("forge") returns 2 skills
    And ForMinister("forge") includes both "go-testing" and "deployment"
    And ForMinister("judge") returns 1 skill named "go-testing"

  # --- Frontmatter parsing ---

  Scenario: SKILL.md with YAML frontmatter is parsed correctly
    Given a SKILL.md with:
      """
      ---
      name: go-testing
      description: Patterns for writing Go tests
      ministers: [forge]
      ---
      # Instructions
      """
    When ParseFrontmatter is called
    Then the parsed name is "go-testing"
    And the parsed description is "Patterns for writing Go tests"
    And the parsed ministers are ["forge"]

  # --- SKILL.md without frontmatter ---

  Scenario: SKILL.md without frontmatter returns an error
    Given a SKILL.md with content "# Just markdown"
    When ParseFrontmatter is called
    Then parsing returns an error

  # --- FormatIndex ---

  Scenario: FormatIndex renders skills as readable markdown
    Given 2 skills: "go-testing" and "deployment"
    When FormatIndex is called
    Then the index contains "go-testing"
    And the index contains "deployment"
    And the index contains "read the full SKILL.md"

  # --- Empty skills and nil map ---

  Scenario: ForMinister with empty or nil map returns nil
    Given a nil skills map
    When ForMinister("forge") is called
    Then the result is nil

  # --- Content SHA stability ---

  Scenario: Same skill content produces identical SHA
    Given a skill "go-testing" with ministers "[forge]" created twice in different temp dirs
    When both skills are discovered
    Then the two SkillInfo structs are deeply equal