---
name: write-tests
description: Patterns and conventions for writing tests
ministers: [judge]
---

# Gherkin Feature Files — Specification & Convention

The project uses [Godog](https://github.com/cucumber/godog) (v0.16.0) with
[Gherkin](https://cucumber.io/docs/gherkin/) to write behavioral specifications
that are also executable tests.

## Feature File Location

Features live in `intent/gherkin/`:

```
intent/gherkin/
├── e729_chat_session_restore.feature
├── e739_peer_credential_verification.feature
└── rituals/
    ├── continue.feature
    ├── ctrl_c_between_steps.feature
    └── ctrl_c_simple.feature
```

- **Root level** — Features covering a single edict: `e<edict_id>_<short_name>.feature`
- **`rituals/`** — Features about ritual behavior (multiple edicts)
- One feature file per **spec scope** (not per test file)

## Anatomy of a Feature

```gherkin
Feature: <Title> — <Subtitle>
  As a <role>
  I want <capability>
  So that <benefit>

  Background:                                    # optional; runs before each scenario
    Given some shared precondition
    And another precondition

  Scenario: <description of one behavior>
    Given a specific precondition
    When something happens
    Then verify the outcome
    And verify another outcome
```

### Background

The `Background` block (if present) sets up the test state before **every**
Scenario. Step definitions use `ctx.Before(...)` to reset state per-scenario.

### Scenario Structure

Every scenario follows this flow:
1. **Given** — Arrange: set up the test model, mocks, and preconditions
2. **When** — Act: trigger the behavior under test
3. **Then** — Assert: verify outputs, mock calls, state mutations

For the judge, feature files should be **verifiable** — each `Then` step
should have a corresponding assertion in the step definition code.

## Step Definitions

Step definitions live in `intent_gherkin_*_test.go` at the project root:

```
intent_gherkin_test.go               # Main test runner (TestIntentGherkin)
intent_gherkin_steps_test.go         # Step defs for edict chat sessions
intent_gherkin_continue_steps_test.go # Step defs for :continue command
```

### Adding Step Definitions for a New Feature

1. Create your `.feature` file in `intent/gherkin/`
2. Create a new `intent_gherkin_<topic>_steps_test.go` file
3. Define a state struct for the test:

```go
type myTestState struct {
    model *TUIModel
    mock  *mockCourtClient
}
```

4. Register step definitions in a `register<Name>StepDefs` function:

```go
func registerMyFeatureStepDefs(ctx *godog.ScenarioContext, t *testing.T) {
    s := &myTestState{}

    ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
        *s = *newMyFeatureState(t)  // Fresh state per scenario
        return ctx, nil
    })

    ctx.Step(`^a precondition exists$`, func() error {
        // Arrange
        return nil
    })

    ctx.Step(`^the user does something$`, func() error {
        // Act
        return nil
    })

    ctx.Step(`^the result should be "([^"]*)"$`, func(expected string) error {
        // Assert
        if actual != expected {
            return fmt.Errorf("expected %q, got %q", expected, actual)
        }
        return nil
    })
}
```

5. Register it in `intent_gherkin_test.go`:

```go
func TestIntentGherkin(t *testing.T) {
    suite := godog.TestSuite{
        Name: "intent-gherkin",
        ScenarioInitializer: func(ctx *godog.ScenarioContext) {
            registerEdictChatStepDefs(ctx, t)
            registerContinueStepDefs(ctx, t)
            registerMyFeatureStepDefs(ctx, t)     // <-- Add here
        },
        Options: &godog.Options{
            Format:   "pretty",
            Paths:    []string{"intent/gherkin"},
            TestingT: t,
        },
    }

    if suite.Run() != 0 {
        t.Fatal("non-zero status returned, failed to run feature tests")
    }
}
```

## Key Infrastructure

The test infrastructure uses:

- **`mockCourtClient`** — a mock for the `CourtClient` interface with
  configurable function fields (`getEdictFn`, `pauseRitualFn`, `sealsFn`, `submitPromptFn`, etc.)
  and tracking counters (`submitPromptCalls`, `pausedChannels`, `resumedChannels`, `publishedEvents`)
- **`TUIModel`** — the main TUI model created via `newTestModel(t)` which includes
  tabs, chat, and mock court wiring
- **`newTestModel(t)`** — returns a `*TUIModel` with `DismissWelcome()` already called
- **`handleContinueCommand(model, args)`** — processes `:continue` commands and
  returns a command function `func() runners.Msg`

## Writing Effective Feature Files

### Good

```gherkin
Scenario: PauseRitual returns true when no step is running
  Given no step cancel is registered for the channel
  When PauseRitual is called for the channel
  Then PauseRitual returns true
  And a pause channel is created for the channel
  And the ritual goroutine is not cancelled
```

### Avoid

- **Implementation details** — focus on behavior, not line numbers or internal
  variable names visible to the test
- **Underspecified steps** — "something happens" without defining what "something" is
- **Too many scenarios per file** — split by feature area

## Running Feature Tests

```bash
# Run all Gherkin feature tests
go test -v -run TestIntentGherkin ./...

# Run with coverage
go test -v -run TestIntentGherkin -coverprofile=coverage.out ./...
```

Feature files are **executable specifications** — they are run as Go tests via
the Godog test suite runner. A failing scenario means either the code doesn't
match the specification, or the specification is stale.
