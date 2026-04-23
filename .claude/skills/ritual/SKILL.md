---
name: ritual
description: Author a new Asimi ritual or edit an existing one in .agents/rituals.yaml. Use when the user says "create a ritual", "add a ritual", "new ritual", "edit ritual", or similar.
user-invocable: true
argument-hint: [ritual description or name]
---

You are authoring an Asimi ritual. The user's request is: `$ARGUMENTS`

## Gather Requirements

Before writing YAML, clarify with the user:

1. **Goal** — What should this ritual accomplish?
2. **Steps** — What ministers are involved and in what order? Available ministers:
   - `chancellor` — coordinates, reports status, requests guidance
   - `strategist` — analyzes requirements, produces plans
   - `forge` — implements changes, writes code
   - `judge` — runs tests, verifies correctness
   - `sage` — reviews code quality, records precedents
3. **Inputs** — Does it need parameters (edict_id, file path, etc.)?
4. **Trigger** — Should it activate on an event (e.g. `shogunate_started`) or be manual (no trigger)?
5. **Failure handling** — Should failing steps retry, goto an earlier step, or request zhengming?

If `$ARGUMENTS` already provides enough detail, skip the questions and proceed.

## Write the Ritual

Add the ritual to `.agents/rituals.yaml` (create the file if it doesn't exist). Follow these rules:

### Structure
- Every ritual needs `name`, `description`, and `steps`
- Use `inputs` for parameters the ritual accepts
- Use `background` to pre-load context shared across multiple steps
- Use ritual-level `then` for outcomes the whole ritual produces

### Steps
- Each step needs a unique `name` and a `minister`
- Use `act` for the prompt — write clear, specific instructions
- Use `given` to pass context variables (e.g. `the manifests`, `the verdicts`, `the earth status`)
- Use `"!command"` in `given` to inject shell command output (e.g. `"!just test"`)
- Use `then` to declare what a step produces
- Steps execute sequentially in array order
- Use `on_failure: retry`, `on_failure: goto` (with `on_failure_target`), or `on_failure: zhengming`

### Context Variables
Available variables for `given` and templates:
- `the edict` / `the edict details` — current edict
- `the manifests` — file changes
- `the borderlands` — unstaged changes
- `the middle kingdom` — staged changes
- `the capital` — committed changes
- `the verdicts` — test results
- `the earth status` — full git status (expands to `.earth_borderlands`, `.earth_middle_kingdom`, `.earth_capital`)
- `manifests for the borderlands` — diffs for unstaged changes
- `the court status` — minister status
- `the unsealed edicts` — edicts awaiting approval
- `a heaven's snapshot` — heaven's state
- `the project metadata` — language, structure info

### Patterns
- **Forge/Judge/Sage loop** — forge implements, judge tests, sage reviews. On failure, loop back to forging.
- **Strategist first** — add a planning step before forging for medium-complexity work.
- **Fork/Join** — for parallel work over a list of items. Use `fork: { over: step-name, batch_size: N }` with a `work:` block containing sub-steps. Access current item with `{{ .item }}`.
- **System steps** — omit `minister` for runtime-handled checks (e.g. sandbox health, version checks).

### Style
- Step names should be gerunds (forging, judging, censoring) or descriptive nouns
- Keep `act` prompts focused — one clear task per step
- Use `max_retries` at the ritual level (default: 1, typical: 2-3)
- Reference template variables with `{{ .variable_name }}`

## Validate

After writing the ritual:
1. Verify the YAML is valid
2. Check that all `on_failure_target` references point to real step names
3. Show the user the complete ritual and ask for confirmation before finalizing
