# Rituals

Rituals are the operational workflows of Asimi. Each ritual defines a sequence of steps that ministers execute to accomplish a task. Users can define custom rituals in `.agents/rituals.yaml` to extend Asimi's capabilities.

## Table of Contents

- [Quick Start](#quick-start)
- [Ritual Structure](#ritual-structure)
- [Triggers](#triggers)
- [Inputs](#inputs)
- [Steps](#steps)
- [Context Variables](#context-variables)
- [The Fork/Join Pattern](#the-forkjoin-pattern)
- [Failure Handling](#failure-handling)
- [Full Example](#full-example)
- [Built-in Rituals](#built-in-rituals)

---

## Quick Start

Create `.agents/rituals.yaml` with your custom ritual:

```yaml
- name: my-ritual
  description: What this ritual does
  steps:
    - name: my-step
      minister: forge
      act: |
        Perform the action here.
```

---

## Ritual Structure

Each ritual is a YAML object with these fields:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Unique identifier for the ritual |
| `description` | string | Human-readable description |
| `triggers` | array | What activates this ritual |
| `inputs` | object | Input parameters (optional) |
| `max_retries` | integer | Max retry attempts on failure (default: 1) |
| `background` | array | Context pre-loaded before steps (see [Background](#background)) |
| `then` | array | Outcomes the ritual as a whole produces |
| `steps` | array | Ordered sequence of steps to execute |

### Background

The `background` field pre-loads context variables before any steps run. This is useful when multiple steps need the same context:

```yaml
background:
  - the edict details
  - the earth status
```

### Ritual-level `then`

A ritual can declare outcomes at the top level, separate from step-level `then`. These describe what the ritual as a whole produces once all steps complete:

```yaml
- name: swift-strike
  steps:
    # ...
  then:
    - the edict awaits ruler's seal
```

---

## Triggers

Triggers define when a ritual activates. If omitted, the ritual is manual by default.

### Event Trigger

```yaml
triggers:
  - event: shogunate_started
```

The ritual activates when a specific event occurs.

---

## Inputs

Define parameters the ritual accepts:

```yaml
inputs:
  edict_id:
    type: string
    required: true
  verbose:
    type: boolean
    required: false
    default: false
```

| Field | Description |
|-------|-------------|
| `type` | Parameter type (string, boolean, integer) |
| `required` | Whether the parameter must be provided |
| `default` | Default value if not required |

Inputs become available as context variables, prefixed with `.`:

```
{{ .edict_id }}
{{ .verbose }}
```

---

## Steps

Steps are the core of a ritual. Each step specifies:

```yaml
steps:
  - name: step-name           # Unique identifier
    minister: forge           # Which ministry executes this
    act: |                    # The action/prompt to execute
      Your prompt here.
    given:                    # Context to provide (optional)
      - the manifests
      - the verdicts
    then:                     # Outcomes produced (optional)
      - the verdicts are passed
    out: |                    # Output format template (optional)
      {{ .result }}
    on_failure: retry        # Failure handling (optional)
```

### Ministers

Each minister has a specific role:

| Minister | Role |
|----------|------|
| `chancellor` | Coordinates the court, reports status, requests guidance |
| `strategist` | Analyzes requirements, produces technical plans |
| `forge` | Implements changes, writes code |
| `judge` | Runs tests, verifies correctness |
| `sage` | Reviews code quality, records precedents |

### Step Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Step identifier (unique within ritual) |
| `minister` | string | Ministry that executes this step |
| `act` | string | The prompt/action to execute |
| `given` | array | Context variables to provide |
| `then` | array | Outcomes this step produces |
| `out` | string | Output format template |
| `on_failure` | string | Action on failure: `retry`, `goto`, `zhengming` |
| `on_failure_target` | string | Target step name for `goto` |

---

## Context Variables

Context variables provide information to steps. They are referenced in templates using `{{ .variable }}`.

### Built-in Context Variables

| Variable | Description |
|----------|-------------|
| `the edict` / `the edict details` | The current edict being processed |
| `the manifests` | List of file changes (manifests) |
| `the borderlands` | Unstaged changes (git status) |
| `the middle kingdom` | Staged changes |
| `the capital` | Committed changes |
| `the verdicts` | Test results from the judge |
| `the earth status` | Full git status (borderlands, middle kingdom, capital) |
| `manifests for the borderlands` | File-level diffs for unstaged changes |
| `the court status` | Status of the court and ministers |
| `the unsealed edicts` | Edicts awaiting approval |
| `a heaven's snapshot` | Heaven's current state |
| `the project metadata` | Project language, structure info |
| `the infrastructure templates` | Template files for project init |
| `Asimi's versions` | Current and latest version info |

### Providing Context with `given`

Use `given` to explicitly pass context to a step:

```yaml
steps:
  - name: review-code
    minister: judge
    given:
      - the manifests
      - the verdicts
    act: |
      Review these changes:
      {{ .manifests }}
```

Prefix an entry with `!` to run a shell command and inject its output as context:

```yaml
given:
  - the manifests
  - "!just test"
```

### System Steps (No Minister)

A step without a `minister` field is a system step — it is handled by the runtime rather than dispatched to a minister. System steps are useful for built-in checks like sandbox health or version checks:

```yaml
- name: check-sandbox
  then:
    - the sandbox is healthy
- name: check-asimi-version
  given:
    - Asimi's versions
  out: |
    {{- if .asimi_version.has_update }}
    Update available: {{ .asimi_version.latest_version }} (current {{ .asimi_version.current_version }})
    {{- else }}
    Running latest Asimi version {{ .asimi_version.current_version }}
    {{- end }}
```

### Producing Context with `then`

Use `then` to declare what a step produces:

```yaml
steps:
  - name: run-tests
    minister: judge
    act: |
      Run the test suite.
    then:
      - the verdicts are passed
```

---

## The Fork/Join Pattern

For parallel execution over a list of items, use `fork`:

```yaml
steps:
  - name: lint
    minister: forge
    act: |
      Run the linter and return a JSON array of files with errors.
      Format: [{"file": "path.go", "errors": ["error1"]}]
    then:
      - the edict is blocked

  - name: fix-all-files
    fork:
      over: lint          # Iterate over the "lint" step's results
      batch_size: 5       # Process 5 items in parallel
    work:
      - name: fix-file
        minister: forge
        act: |
          Fix all linter errors in file {{ .item.file }}.
          The errors to fix are: {{ .item.errors }}.
      - name: verify-file
        minister: judge
        act: |
          Run tests for file {{ .item.file }}.
        on_failure: retry
        on_failure_target: fix-file

  - name: commit-all
    minister: forge
    act: |
      Stage and commit all changes.
```

Within fork work steps, use `{{ .item }}` to access the current item being processed.

---

## Failure Handling

### Retry

Retry the same step:

```yaml
on_failure: retry
```

### Goto

Jump to a specific step:

```yaml
on_failure: goto
on_failure_target: forging
```

### Zhengming

Request clarification from the ruler:

```yaml
on_failure: zhengming
```

This is used when requirements are unclear and the minister needs guidance to proceed.

---

## Full Example

Here's `swift-strike`, one of the built-in rituals — a tight forge/judge/sage loop for small changes:

```yaml
- name: swift-strike
  description: A tight loop for enacting small changes in 地
  inputs:
    edict_id:
      type: string
      required: true
  max_retries: 3
  background:
    - the edict details
  steps:
    - name: forging
      minister: forge
      act: |
        Implement the changes for the edict below.
        {{ .edict }}
        Focus on minimal, targeted changes to fulfill the intent.
      on_failure: retry

    - name: judging
      minister: judge
      given:
        - the manifests
        - "!just test"
      act: |
        If any tests were changed, verify they were not weakened.
        {{ .manifests }}
        If tests fail, provide clear feedback for the Forge.
      then:
        - the verdicts are passed
        - record the judge's seal
      on_failure: goto
      on_failure_target: forging

    - name: censoring
      minister: sage
      given:
        - the manifests
        - the verdicts
      act: |
        Review the code changes for the edict.
        Check for style violations, security concerns, and architectural issues.
        Record your insights using record_precedent.
      on_failure: goto
      on_failure_target: forging
  then:
    - the edict awaits ruler's seal
```

---

## Built-in Rituals

Asimi ships with six built-in rituals in [`builtin_rituals.yaml`](../shogunate/builtin_rituals.yaml).

### swift-strike

A tight forge/judge/sage loop for small, targeted changes. The forge implements the edict, the judge runs tests and verifies coverage, and the sage reviews for style and security. On failure, control loops back to forging.

**Steps:** forging → judging → censoring

### castle-siege

A medium-complexity workflow that adds a strategist planning phase before forging. The strategist analyzes the edict and produces a Battle Plan with phases and dependencies. The forge then executes the plan, followed by judging and censoring.

**Steps:** strategizing → forging → judging → censoring

### dawn-audience

The startup ritual, triggered automatically by the `shogunate_started` event. It pings the model, checks sandbox health, checks for Asimi updates, and then the chancellor summarizes the state of the three realms and requests zhengming for the next step. Contains system steps (no minister) for sandbox and version checks.

**Steps:** ping-model → check-sandbox → check-asimi-version → summarize-and-next

### review-borderland

A code review ritual for unstaged changes. The judge verifies test coverage for the borderlands, then the sage reviews for style, security, and architecture. Neither step stages or commits — the ruler decides what to do after review.

**Steps:** judging → censoring

### lint-fix

Fixes linter errors in parallel using the [fork/join pattern](#the-forkjoin-pattern). The forge runs the linter and returns a JSON list of files with errors, then a fork fans out over those files — fixing each one and verifying with tests. Finally, all changes are committed together.

**Steps:** lint → fix-all-files (fork: fix-file → verify-file) → commit-all

### project-init

Initializes a project for Asimi. The forge customizes infrastructure templates (Justfile, Dockerfile, bashrc, asimi.conf, AGENTS.md) for the detected language. The judge verifies the sandbox is operational (must return Linux from `uname -a`). The sage reviews the infrastructure files for quality. Accepts an optional `clearMode` boolean and `agentsFile` string input.

**Steps:** establish-infrastructure → sandbox-ready → sage-reviews

---

## See Also

- [builtin_rituals.yaml](../shogunate/builtin_rituals.yaml) — Reference implementation of all built-in rituals
- [Edicts](./edicts.md) — How edicts flow through the seal chain
- [The Three Realms](./three-realms.md) — Understanding 天, 地, and 人
