# Shogunate User Guide

The Shogunate is Asimi's autonomous agent framework, inspired by the imperial bureaucracy of ancient China. It orchestrates multiple specialized AI ministers to handle complex software engineering tasks from inception to deployment.

## Overvie

When you interact with Asimi in Shogunate mode, you assume the role of the **Ruler**.
You issue **Edicts** (work orders) which the Chancellor classifies and enact a right-sized ritual to handle it.

Large changes go through the "Grand Campaign" ritual:
```
Ruler (You)
    |
    v
Chancellor  <---> Zhengming (clarification requests)
    |
    +-- Strategist (planning)
    +-- Forge (implementation)
    +-- Judge (testing)
    +-- Censor (review)
```

## Core Concepts

### Edicts

An **Edict** is a work order issued by the Ruler. It captures your intent and tracks the entire lifecycle of a task from request to completion.

**Phases:**
- `classifying` - Chancellor determines the scope and approach
- `planning` - Strategist breaks down the work into Lings
- `forging` - Forge implements the code changes
- `judging` - Judge runs tests and validates changes
- `censoring` - Censor reviews for quality and standards
- `deploying` - Changes are deployed (future)
- `sealed` - Edict successfully completed
- `cancelled` - Edict was cancelled

TODO: add "halted" waiting on user feddback

### Ministers

Each minister is a specialized AI agent with a specific role:

| Minister | Role | Tools |
|----------|------|-------|
| **Chancellor** | Coordinates all ministers, manages edict lifecycle, interfaces with the Ruler | `create_edict`, `request_zhengming`, `get_edict_status`, `list_edicts`, `invoke_minister`, `asimisql` |
| **Strategist** | Analyzes edicts, creates execution plans, decomposes work into Lings | - |
| **Forge** | Implements code changes according to plans | - |
| **Judge** | Write tests and validates changes through test coverage | - |
| **Censor** | Reviews code for ethics, quality, and standards compliance | - |
| **Marshal** | Handles production incidents and performs root cause analysis | - |

### Lings

A **Ling** is a sub-task within an edict's execution plan. The Strategist creates Lings during the planning phase, breaking down the edict intent into actionable work items with dependencies.

```
Edict: "Add user authentication"
  |
  +-- Ling 1: "Create user model"
  +-- Ling 2: "Add login endpoint" (depends on Ling 1)
  +-- Ling 3: "Add logout endpoint" (depends on Ling 1)
  +-- Ling 4: "Write authentication tests" (depends on Lings 2, 3)
```

### Zhengming (Clarification)

**Zhengming** (正名, "rectification of names") is the protocol for requesting clarification when requirements are ambiguous. When a minister encounters uncertainty that could impact the work, they invoke Zhengming to ask the Ruler for guidance.

The edict is paused until you respond. This ensures the Shogunate never guesses at requirements - it always seeks clarity before proceeding.

**Priority levels:**
- `normal` - Standard timeout (24 hours)
- `urgent` - Short timeout (1 hour)

### Rituals

**Rituals** are YAML-defined workflows that orchestrate ministers through a series of steps. They provide reusable patterns for common tasks.

**Built-in rituals:**

#### Swift Strike (S)
For small, focused changes. A tight loop between Forge and Judge:
```yaml
steps:
  - name: build
    minister: forge
    task: Implement the changes for edict {{ .edict_id }}
    on_failure: retry
    max_retries: 3
  - name: test
    minister: judge
    depends_on: [forge]
    on_failure_target: forge  # Loop back on failure
```

#### Grand Campaign (L)
For larger architectural work with strict gatekeeping:
```yaml
steps:
  - name: strategist  # Create battle plan
  - name: forge       # Execute the plan
  - name: judge       # Run trials
  - name: censor      # Final review
```

**Creating custom rituals:**

Place YAML files in `.agents/rituals/` to define project-specific rituals:

```yaml
# .agents/rituals/my-ritual.yaml
name: my-ritual
description: "Custom workflow for my project"
triggers:
  - manual: true
inputs:
  edict_id:
    type: string
    required: true
steps:
  - name: step-1
    minister: forge
    task: |
      Do something for {{ .edict_id }}
    on_failure: retry
    max_retries: 2
```


**Step types:**
- `minister` - Invoke a minister with a task
- `prompt` - Execute an LLM prompt directly
- `cmd` - Run a shell command
- `gate` - Conditional check before proceeding

TODO: replace the types with a "before" and "after" for shell commands wrapping of the response

**Failure handling:**
- `retry` - Retry the step up to max_retries
- `zhengming` - Request clarification from the Ruler
- `goto` - Jump to a specific step (use `on_failure_target`)
- `abort` - Stop the ritual entirely

TODO: remove zhengming as it's like panic and does flow through the normak failure handling

## Data Model

### Forge Manifests

When the Forge implements changes, it creates **Manifests** tracking each file modification:

**Statuses:**
- `staged` - Change created but not committed
- `live` - Committed to the repository
- `quenched` - Validated by the Judge
- `rejected` - Failed review

### Judge Verdicts

The Judge creates **Verdicts** after running tests:
- `passed` - Tests succeeded
- `failed` - Tests failed

### Censor Precedents

The Censor records **Precedents** from ethics reviews:
- `approved` - Code meets standards
- `rejected` - Code violates principles

### Tian Events

The **Tian** (天, Heaven) ledger records all events in the edict lifecycle for auditing and debugging:
- `edict_created`
- `edict_assigned`
- `phase_changed`
- `forge_committed`
- `ritual_started`
- `ritual_completed`
- `ritual_failed`
- `zhengming_needed`
- `zhengming_answered`
- `edict_cancelled`

TODO: add ritual_step_started/completed/failed

## Using the Shogunate

### Starting the Shogunate

The Shogunate starts automatically when Asimi initializes. Ministers begin their processing loops and await tasks.

### Issuing Edicts

Simply describe what you want in natural language. The Chancellor will:
1. Create an edict capturing your intent
2. Classify the scope (S, M, L, XL)
3. Enact an appropriate ritual

When a ritual is on the Ritual Guard takes over and executes the workflow.

Example:
```
You: Add a logout button to the header that clears the session and redirects to /login

Chancellor: [Creates edict, invokes swift-strike ritual]
  Forge: [Implements the button]
  Judge: [Runs tests]
  -> Edict sealed
```

### Responding to Zhengming

When a minister needs clarification, you'll be prompted:

```
Zhengming from Chancellor:
"Should the logout confirmation use a modal dialog or inline confirmation?"

> Use a modal dialog with "Cancel" and "Logout" buttons
```

Your response is appended to the edict's intent and processing continues.

### Checking Status

Ask about edict status:
```
You: What's the status of the logout feature?

Chancellor: [Uses get_edict_status tool]
  Edict issue-123: phase=judging, all tests passing
```

### Cancelling Edicts

If you change your mind:
```
You: Cancel the logout feature edict

Chancellor: [Updates phase to cancelled]
  Edict issue-123 cancelled
```

## Configuration

### Shogunate Settings

In `.agents/asimi.toml`:

```toml
[shogunate]
poll_interval = "5s"     # Ritual Guard polling interval
ritual_timeout = "30s"   # Max time per ritual step
```

### Project Rituals

Create custom rituals in `.agents/rituals/*.yaml` to define project-specific workflows.

## Architecture Notes

### Event-Driven Design

The Shogunate uses an event-driven architecture:
1. Events are recorded to the Tian ledger
2. The Ritual Guard polls for events
3. Rituals react to relevant events
4. Ministers process tasks asynchronously

### Session Isolation

Each edict gets its own LLM session, maintaining context for the duration of the task. This allows multiple edicts to be processed concurrently without context contamination.

### Streaming

All minister responses stream back to the TUI in real-time, so you can follow progress as the Shogunate works.

## Troubleshooting

### "LLM not configured"
The model hasn't connected yet. Wait for authentication to complete.

### Edict stuck in a phase
Check for pending Zhengming requests - the Shogunate may be waiting for your input.

### Ritual failures
Check the Tian events to see what happened:
```sql
SELECT * FROM tian_events WHERE edict_id = 'your-edict-id' ORDER BY created_at;
```

Use the `asimisql` tool or direct database access to inspect state.

## Philosophy

The Shogunate embodies these principles:

1. **Zhengming** (正名) - Rectification of Names
   Never guess at requirements. When ambiguity threatens, stop and ask.

2. **Dao** (道) - The Way
   Follow established patterns. Use rituals for repeatable workflows.

3. **De** (德) - Virtue
   The Censor ensures ethical behavior. Code must meet standards.

4. **Tian** (天) - Heaven
   All actions are recorded. The ledger provides accountability.

The metaphor isn't just aesthetic - it encodes a philosophy of careful, principled software development where clarity precedes action and quality is non-negotiable.
