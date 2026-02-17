-----------
# Shogunate User Guide (v3)
## Bakground - Confucian Foundations                                                                            儒學基礎 
                                                                                                              
The Shogunate framework is built upon the bedrock of Confucian philosophy,
which emphasizes moral cultivation, social harmony, and the rectification of
names. This is not merely aesthetic metaphor—it is a deliberate encoding of
timeless principles into the craft of software development.                          
The Five Constant Virtues (五常, Wǔcháng):                                                                     
• 仁 (Rén) - Benevolence/Humaneness: The Censor embodies 仁, ensuring code serves users with compassion        
• 义 (Yì) - Righteousness: The Judge upholds 义, validating that implementations meet their proper purpose     
• 礼 (Lǐ) - Ritual Propriety: Rituals themselves are 礼, the formal patterns that maintain order               
• 智 (Zhì) - Wisdom: The Strategist exercises 智, discerning the proper path through complexity                
• 信 (Xìn) - Trustworthiness: The Tian ledger maintains 信, providing immutable accountability                 

As Confucius taught in the Analects (論語): "If names be not correct, language is not in accordance with       
the truth of things. If language be not in accordance with the truth of things, affairs cannot be carried on     
to success." (名不正，則言不順；言不順，則事不成。) This is the essence of Zhengming (正名).    

## Overview                                                                                                    
                                                                                                              
When you interact with Asimi's Shogunate, you assume the role of the Ruler (君主, Jūnzhǔ). You issue     
Edicts (詔令, Zhàolìng) which the Shogunate processes through an appropriate ritual, each handled by specialized    
Ministers (大臣, Dàchén) who serve as your 君子 (Jūnzǐ)—exemplary persons who cultivate virtue through their     
⇣craft.                                                                                                           
-------------
The Shogunate is Asimi's autonomous agent framework, inspired by the imperial bureaucracy of ancient China. It orchestrates multiple specialized AI ministers to handle complex software engineering tasks from inception to deployment.

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
    +-- Marshal (incidents)
```

## Core Concepts

### Edicts

An **Edict** is a work order issued by the Ruler.
It captures their intent and tracks the entire lifecycle of a task from request to completion.

**Phases:**
- `brewing` - Chancellor determines the scope and approach
- `planning` - Strategist breaks down the work into Lings
- `forging` - Forge implements the code changes
- `judging` - Judge runs tests and validates changes
- `censoring` - Censor reviews for quality and standards
- `deploying` - Changes are deployed (future)
- `sealed` - Edict successfully completed
- `cancelled` - Edict was cancelled

An edict can be **halted** at any phase (boolean flag), pausing work until the Ruler responds to a pending Zhengming.

> **Implementation Note:** Halted is a `bool` field on `Edict`, not a separate phase. Set `Halted = true` when Zhengming is requested, `Halted = false` when answered.

### Ministers

Each minister is a specialized AI agent with a specific role:

| Minister | Role | Core Tools | Specialized Tools |
|----------|------|------------|-------------------|
| **Chancellor** | Coordinates all ministers, manages edict lifecycle, interfaces with the Ruler | — | `create_edict`, `cancel_edict`, `request_zhengming`, `answer_zhengming`, `get_edict_status`, `list_edicts`, `list_rituals`, `enact_ritual`, `get_tian_events`, `asimisql` |
| **Strategist** | Analyzes edicts, creates execution plans, decomposes work into Lings | `read_file`, `list_files`, `grep` | `create_ling`, `update_ling`, `list_lings` |
| **Forge** | Implements code changes according to plans | `read_file`, `write_file`, `edit_file`, `list_files`, `grep`, `run_shell_command` | `create_manifest`, `update_manifest`, `commit_manifest` |
| **Judge** | Writes tests and validates changes through test coverage | `read_file`, `write_file`, `edit_file`, `list_files`, `run_shell_command` | `record_verdict` |
| **Censor** | Reviews code for ethics, quality, and standards compliance | `read_file`, `list_files`, `grep` | `record_precedent` |
| **Marshal** | Handles production incidents and performs root cause analysis | `read_file`, `list_files`, `grep`, `run_shell_command` | `create_incident`, `resolve_incident` |

**Core Tools** are the basic file system and shell tools needed for each minister's work. **Specialized Tools** are unique to each minister's role in the Shogunate.

### Chancellor Tools Reference

The Chancellor has access to these tools for orchestrating the Shogunate:

**Edict Management:**
- `create_edict(intent, scope?)` - Create a new edict from the Ruler's intent
- `cancel_edict(edict_id)` - Cancel an edict, moving it to `cancelled` phase
- `get_edict_status(edict_id)` - Get current status, phase, and Lings for an edict
- `list_edicts(status?)` - List edicts, optionally filtered by status

**Zhengming (Clarification):**
- `request_zhengming(edict_id, question, priority?)` - Pause edict and ask Ruler for clarification
- `answer_zhengming(edict_id, answer)` - Provide Ruler's answer and resume the edict

**Ritual Management:**
- `list_rituals()` - List available rituals from `.agents/rituals/`
- `enact_ritual(ritual_name, edict_id)` - Start a ritual for an edict

**Observability:**
- `get_tian_events(edict_id?, event_type?, limit?)` - Query the Tian event ledger
- `asimisql(query)` - Execute raw SQL for advanced queries

> **Note:** `invoke_minister` was removed. The Chancellor orchestrates ministers through rituals, not direct invocation. For ad-hoc minister work, use `enact_ritual` with a custom ritual.

### Strategist Tools Reference

The Strategist breaks down edicts into actionable work items:

- `create_ling(edict_id, description, depends_on?)` - Create a new Ling (sub-task) for an edict
- `update_ling(ling_id, status?, description?)` - Update a Ling's status or description
- `list_lings(edict_id)` - List all Lings for an edict with their dependencies

### Forge Tools Reference

The Forge tracks all file modifications:

- `create_manifest(edict_id, file_path, change_type)` - Record a new file change (create/modify/delete)
- `update_manifest(manifest_id, status)` - Update manifest status (staged → live → quenched)
- `commit_manifest(manifest_id, commit_sha)` - Mark manifest as committed with git SHA

### Judge Tools Reference

The Judge validates changes through testing:

- `record_verdict(edict_id, ling_id?, passed, details?)` - Record test results for an edict or specific Ling

### Censor Tools Reference

The Censor reviews code for ethics and quality:

- `record_precedent(edict_id, approved, reasoning)` - Record ethics review outcome with reasoning

### Marshal Tools Reference

The Marshal handles production incidents:

- `create_incident(description, severity, edict_id?)` - Create a new incident, optionally linked to an edict
- `resolve_incident(incident_id, resolution, root_cause?)` - Mark incident resolved with details

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

**The Zhengming Loop:**
1. Minister calls `request_zhengming` with a question → Edict moves to `halted` phase
2. Ruler provides their answer
3. Chancellor calls `answer_zhengming` with the response → Answer appended to edict intent, edict resumes previous phase

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
    depends_on: [build]
    on_failure_target: build  # Loop back on failure
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

**[NEW] Step Hooks:**

Each step can define `before` and `after` shell commands that wrap the minister's response:

```yaml
steps:
  - name: build
    minister: forge
    task: Implement feature X
    before: "git stash"           # Run before minister executes
    after: "just fmt && just lint" # Run after minister completes
```

> **Implementation Note:** Replace the current step types (`minister`, `prompt`, `cmd`, `gate`) with a unified model where every step invokes a minister, and shell commands are hooks:
> - `before` - Shell command run before the step
> - `after` - Shell command run after successful completion
> - Remove `cmd` type entirely - use `before`/`after` hooks instead
> - Remove `gate` type - use `before` with exit code checking

**Failure handling:**
- `retry` - Retry the step up to max_retries
- `goto` - Jump to a specific step (use `on_failure_target`)
- `abort` - Stop the ritual entirely

> **Implementation Note:** Remove `zhengming` from failure handling options. Zhengming is not a failure recovery mechanism - it's a clarification protocol that should be invoked explicitly by ministers when they detect ambiguity, not as an automatic response to step failure. Zhengming operates outside the normal failure flow.

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
- **[NEW]** `ritual_step_started` - A ritual step began execution
- **[NEW]** `ritual_step_completed` - A ritual step finished successfully
- **[NEW]** `ritual_step_failed` - A ritual step failed

> **Implementation Note:** Add these event emissions to `RitualRunner.executeStep()` in `shogunate/ritual.go`:
> - Emit `ritual_step_started` at the beginning of `executeStep()`
> - Emit `ritual_step_completed` when step succeeds
> - Emit `ritual_step_failed` when step fails (before retry logic)

## Using the Shogunate

### Starting the Shogunate

The Shogunate starts automatically when Asimi initializes. Ministers begin their processing loops and await tasks.

### Issuing Edicts

Simply describe what you want in natural language. The Chancellor will:
1. Create an edict capturing your intent
2. Classify the scope (S, M, L, XL)
3. Enact an appropriate ritual

When a ritual is enacted, the Ritual Guard takes over and executes the workflow.

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

Chancellor: [Uses cancel_edict tool]
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
```
You: What happened with the logout feature?

Chancellor: [Uses get_tian_events tool for edict issue-123]
  Events show: ritual_step_failed at step "build" - compilation error
```

For advanced queries, use `asimisql` with raw SQL.

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

---

## Implementation Plan Summary

The following changes are marked with **[NEW]** in this document:

### 1. Add `Halted` Bool Flag
**File:** `storage/shogunate_schema.go`
- Add `Halted bool` field to `Edict` struct

**File:** `shogunate/chancellor.go`
- Update `RequestZhengming` to set `Halted = true`
- Update `AnswerZhengming` to set `Halted = false`

### 2. Add Missing Chancellor Tools
**File:** `shogunate/tools/chancellor_tools.go`
- `cancel_edict` - Set edict phase to `cancelled`
- `answer_zhengming` - Complete the Zhengming loop, append answer to intent, resume edict
- `list_rituals` - Scan `.agents/rituals/` and return available ritual names
- `enact_ritual` - Invoke the RitualGuard to execute a ritual for an edict
- `get_tian_events` - Query tian_events table with optional filters

**File:** `shogunate/chancellor.go`
- Remove `invoke_minister` - ministers are orchestrated via rituals, not direct calls

### 3. Add Minister-Specific Tools
**File:** `shogunate/tools/strategist_tools.go`
- `create_ling`, `update_ling`, `list_lings` - Manage Lings within an edict

**File:** `shogunate/tools/forge_tools.go`
- `create_manifest`, `update_manifest`, `commit_manifest` - Track file changes

**File:** `shogunate/tools/judge_tools.go`
- `record_verdict` - Record test pass/fail verdicts

**File:** `shogunate/tools/censor_tools.go`
- `record_precedent` - Record ethics review outcomes

**File:** `shogunate/tools/marshal_tools.go`
- `create_incident`, `resolve_incident` - Manage production incidents

### 4. Replace Step Types with Before/After Hooks
**File:** `shogunate/ritual.go`
- Add `Before` and `After` fields to `RitualStep` struct
- Remove `Type` field (or deprecate `cmd`, `gate` types)
- Update `executeStep()` to run `before` command before minister invocation
- Update `executeStep()` to run `after` command after successful completion
- Handle hook failures (fail the step if hook returns non-zero)

### 5. Remove Zhengming from Failure Handling
**File:** `shogunate/ritual.go`
- Remove `OnFailureZhengming` constant
- Update `handleFailure()` to not support zhengming action
- Document that zhengming is explicit, not a failure recovery

### 6. Add Ritual Step Events
**File:** `shogunate/ritual.go`
- Add event emission at start of `executeStep()`: `ritual_step_started`
- Add event emission on success: `ritual_step_completed`
- Add event emission on failure: `ritual_step_failed`

**File:** `shogunate/shogunate.go`
- Add new event constants:
  - `EventStepStarted ShogunateEvent = "ritual_step_started"`
  - `EventStepFailed ShogunateEvent = "ritual_step_failed"`
  - (Note: `EventStepCompleted` already exists)
