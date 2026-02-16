# The Shogunate — User Guide

## Background — Confucian Foundations 儒學基礎

The Shogunate framework is built upon the bedrock of Confucian philosophy,
which emphasizes moral cultivation, social harmony, and the rectification of
names. This is not merely aesthetic metaphor—it is a deliberate encoding of
timeless principles into the craft of software development.

The Five Constant Virtues (五常, Wǔcháng):

- 仁 (Rén) — Benevolence/Humaneness: The Censor embodies 仁, ensuring code serves users with compassion
- 义 (Yì) — Righteousness: The Judge upholds 义, validating that implementations meet their proper purpose
- 礼 (Lǐ) — Ritual Propriety: Rituals themselves are 礼, the formal patterns that maintain order
- 智 (Zhì) — Wisdom: The Strategist exercises 智, discerning the proper path through complexity
- 信 (Xìn) — Trustworthiness: The Tian ledger maintains 信, providing immutable accountability

As Confucius taught in the Analects (論語): 

> If names be not correct, language is not in accordance with the truth of things.
> If language be not in accordance with the truth of things,
> affairs cannot be carried on to success."
> 名不正，則言不順；言不順，則事不成。

This is the essence of Zhengming (正名).

### Main Function

The Shogunate's purpose is to free the Ruler to hunt.
The Ruler together with Confucius the sage will hunt for new rituals,
improvments and look behind corners to come up with knight moves.
The court handles the bureaucracy so the Ruler
can stay in skirmish tempo, spotting openings and shaping direction.

To free the ruler, the Shogunate harmonizes the three realms:

- **Humanity (人, Rén)** — The Ruler. The source of intent and will, the "why" of all action. Ren flows downward through edicts and clarifications. The Ruler's humaneness (仁) is the wellspring from which all purpose emerges.

- **Heaven (天, Tiān)** — The Ledger. Events, logs, test results, and the immutable record of all that transpires. Heaven witnesses and remembers — the Tian (天) database provides accountability and the mandate that validates the Ruler's decrees.

- **Earth (地, Dì)** — Implementation. The codebase, commits, branches and worktrees. Earth receives the Ruler's intent and grounds it in material reality.

The ministers and rituals mediate between these realms — translating Ren into action, recording outcomes in Tian, shaping changes in Di. When the three realms are in harmony, intent flows naturally into code without friction. The Ruler speaks, ministers act, Heaven records, Earth changes. When harmony breaks — ambiguity in Ren, gaps in Tian, or errors in Di — Zhengming restores alignment.

> **Note on Ren:** Chinese has two homophones: 人 (humanity, the realm) and 仁 (benevolence, the virtue). The Ruler's 人 (humanity) is the source of intent;
> the ministers 仁 (benevolence) gives that intent moral weight. Both are pronounced *rén*, but they carry distinct meanings — one ontological (who acts), one ethical (how one acts).

### Guiding Principles

The Shogunate embodies these principles:

1. **Zhengming** (正名) — Rectification of Names.
   Never guess at requirements. When ambiguity threatens, stop and ask.
   **Guessing is TREASON** — a minister that proceeds without clarity betrays the Ruler's trust.

2. **Dao** (道) — The Way.
   Follow Wu-Wei. Use rituals for repeatable frictionless workflows,
   ensuring the earth stays free of friction and bureaucracy.

3. **De** (德) — Virtue.
   The Censor ensures ethical behavior. Code must be beautiful.

The metaphor isn't just aesthetic — it encodes a philosophy of careful, principled software development where clarity precedes action and quality is non-negotiable.

---

## Overview

When you interact with Asimi's Shogunate, you assume the role of the **Ruler**
(君主, Jūnzhǔ). You issue Edicts (詔令, Zhàolìng) which the Shogunate
processes through an appropriate ritual, each handled by specialized Ministers
(大臣, Dàchén) who serve as your 君子 (Jūnzǐ)—exemplary persons who cultivate
virtue through their craft.

The Shogunate is Asimi's autonomous agent framework, inspired by the imperial
bureaucracy of ancient China. It orchestrates multiple specialized AI ministers
to handle complex software engineering tasks from inception to deployment.

```
                        ┌─────────────────────────────┐
                        │         TUI (tui)           │
                        │  [Ruling]       [Hunting]   │
                        └─────┬───────────────┬───────┘
                              │ Prompt        │ Prompt
                        ┌─────▼─────┐   ┌────▼──────┐
                        │Chancellor │   │ Confucius │
                        │  (宰相)    │   │  (孔子)    │
                        └─────┬─────┘   └───────────┘
                              │              sees all (read-only)
                              │              creates edicts ──►─┐
                              │ Task / Result                   │
          ┌────────────┬──────┼───────┬────────────┐            │
          ▼            ▼      ▼       ▼            ▼            │
     Strategist     Forge   Judge   Censor     Marshal          │
      (兵部)        (工部)  (刑部)  (都察院)    (锦衣卫)          │
                                                                │
                        ┌─────────────────┐                     │
                        │  Ritual Runner  │◄────────────────────┘
                        │  (embedded YAML)│
                        └────────┬────────┘
                                 │ dispatches Tasks
                                 ▼
                            ministers...
```

---

## Core Concepts

### Edicts

An **Edict** is a work order issued by the Ruler. It captures intent and tracks
the entire lifecycle of a change from request to completion.

**Phases:**

```
Classifying → Planning → Forging → Judging → Censoring → Sealed
                                                    ↑
                                              (or Cancelled)
                                              (or Halted)
```

- `classifying` — Chancellor determines the scope and approach
- `planning` — Strategist breaks down the work into Lings
- `forging` — Forge implements the code changes
- `judging` — Judge runs tests and validates changes
- `censoring` — Censor reviews for quality and standards
- `sealed` — Edict successfully completed (minister marks it sealed after successful completion)
- `cancelled` — Edict was cancelled
- `halted` — Edict paused waiting on user feedback (Zhengming pending)

**Phase Transitions:** Ministers transition the edict through phases as they complete their work. The final minister in the ritual marks the edict as `sealed` upon successful completion.

### Ministers

Each minister is a specialized AI agent with a specific role:

| Minister | Role | Core Tools | Specialized Tools |
|----------|------|------------|-------------------|
| **Chancellor** | Coordinates all ministers, manages edict lifecycle, interfaces with the Ruler | — | `create_edict`, `cancel_edict`, `request_zhengming`, `answer_zhengming`, `get_edict_status`, `list_edicts`, `list_rituals`, `enact_ritual`, `get_tian_events`, `asimisql` |
| **Strategist** | Analyzes edicts, creates execution plans, decomposes work into Lings | `read_file`, `list_files`, `grep` | `create_ling`, `update_ling`, `list_lings`, `request_zhengming` |
| **Forge** | Implements code changes according to plans | `read_file`, `write_file`, `edit_file`, `list_files`, `grep`, `run_shell_command` | `create_manifest`, `update_manifest`, `commit_manifest`, `request_zhengming` |
| **Judge** | Writes tests and validates changes through test coverage | `read_file`, `write_file`, `edit_file`, `list_files`, `run_shell_command` | `record_verdict`, `reject_manifest`, `request_zhengming` |
| **Censor** | Reviews code for ethics, quality, and standards compliance | `read_file`, `list_files`, `grep` | `record_precedent`, `reject_manifest`, `request_zhengming` |
| **Marshal** | Handles production incidents and performs root cause analysis | `read_file`, `list_files`, `grep`, `run_shell_command` | `create_incident`, `resolve_incident`, `request_zhengming` |
| **Confucius** | Sees all state read-only, helps distill intent into edicts | `read_file`, `list_files`, `grep` (all tables) | `create_edict`, `request_zhengming` |

**Core Tools** are the basic file system and shell tools needed for each minister's work. **Specialized Tools** are unique to each minister's role in the Shogunate.

### Lings

A **Ling** is a sub-task within an edict's execution plan. The Strategist
creates Lings during the planning phase, breaking down the edict intent into
actionable work items with dependencies.

```
Edict: "Add user authentication"
  |
  +-- Ling 1: "Create user model"
  +-- Ling 2: "Add login endpoint" (depends on Ling 1)
  +-- Ling 3: "Add logout endpoint" (depends on Ling 1)
  +-- Ling 4: "Write authentication tests" (depends on Lings 2, 3)
```

### Zhengming (Clarification)

**Zhengming** (正名, "rectification of names") is the protocol for requesting
clarification when requirements are ambiguous. When a minister encounters
uncertainty that could impact the work, they invoke Zhengming to ask the Ruler
for guidance.

The edict moves to the `halted` phase until you respond. This ensures the
Shogunate never guesses at requirements—it always seeks clarity before
proceeding.

**The Zhengming Loop:**

1. Minister calls `request_zhengming` with a question — edict moves to `halted`
2. Chancellor receives the question and attempts to answer from its wider context
3. If the Chancellor cannot answer, it escalates to the Ruler
4. Answer (from Chancellor or Ruler) is appended to edict intent, edict resumes previous phase

Zhengming is a clarification protocol, not a failure recovery mechanism. Ministers invoke it explicitly when they detect ambiguity—it operates outside the normal failure flow. **To guess is treason**—a minister that proceeds without clarity betrays the Ruler's trust.

### Rituals

**Rituals** are YAML-defined workflows that orchestrate ministers and code snippets
through a series of steps. They provide reusable patterns for common tasks.

**Built-in rituals:**

#### Swift Strike (S-size edicts)

For small, focused changes. A tight loop between Forge and Judge:

```yaml
swift-strike:
    on_failure: retry
    max_retries: 3
    arrange:
        # calling a predefined bash function that outputs the edict in markdown
        get_edict {{ .edict_id}}
    steps:
      - name: forge
        minister: forge
        act: |
          Implement the changes for the edict.
          Focus on minimal, targeted changes.

      - name: judge
        minister: judge
        act: |
          - Ensure new code is covered by tests
          - Ensure the test assert the code will always work
        assert: just test
        depends_on: [forge]
        on_failure: "forge"
```

#### Grand Campaign (M-size edicts)

For medium-complexity work with planning and review:

```yaml
grand-campaign:
    on_failure: retry
    max_retries: 3
    arrange:
        get_edict {{ .edict_id }}
    steps:
      - name: strategist
        minister: strategist
        act: Analyze the edict and produce a Battle Plan.

      - name: forge
        minister: forge
        act: Implement the Battle Plan.
        depends_on: [strategist]

      - name: judge
        minister: judge
        act: |
          - Ensure new code is covered by tests
          - Ensure the tests assert the code will always work
        assert: just test
        depends_on: [forge]
        on_failure: "forge"

      - name: censor
        minister: censor
        act: |
          Review the changes for quality and standards compliance.
        depends_on: [judge]
        on_failure: "strategist"
```

#### Grand Orchestration (L-size edicts)

For complicated architectural work requiring custom workflow design. The Strategist first designs an ad-hoc ritual tailored to the edict's complexity, then the court executes it:

```yaml
grand-orchestration:
    on_failure: retry
    max_retries: 3
    arrange:
        get_edict {{ .edict_id }}
    steps:
      - name: architect
        minister: strategist
        act: |
          Analyze the edict and design a custom ritual workflow.
          Create the ritual definition as a YAML document.
          This ritual will be enacted for this specific edict.

      - name: ratify
        minister: chancellor
        act: |
          Review the proposed ritual for feasibility and safety.
          If approved, create the ad-hoc ritual and enact it.
        depends_on: [architect]
```

#### Wakeup

Invoked as the last step of `Start()`, orients the court before the Ruler speaks:

```yaml
wakeup:
    arrange:
        get_court_status
    steps:
      - name: orient
        minister: strategist
        act: |
          Court is now in session.
          Help the Ruler start his day by raising zhengming.
          Suggest 2-3 actionable options for the Ruler to pursue.
          Return them as a numbered list, each with a one-line description.
```

**Step AAA pattern:**

Each step follows Arrange-Act-Assert. `arrange` and `assert` are optional shell commands that wrap the minister's `act`:

```yaml
steps:
  - name: build
    minister: forge
    scope: edict              # Context inheritance
    env:                      # Environment variables
      GOOS: linux
      GOARCH: amd64
    arrange: "git stash"           # Shell: setup before minister executes
    act: Implement feature X       # LLM: minister instruction
    assert: "just fmt && just lint" # Shell: must pass or step fails
```

- `scope` — controls context inheritance:
  - `edict` — retain the edict's context (default)
  - `private` — start with a fresh context
  - `<step_name>` — fork from a specific step's context
- `env` — environment variables available to `arrange`, `act`, and `assert`
- `arrange` — runs before the minister, gathers context. Variables it exports become template inputs for `act`.
- `act` — the minister's instruction (LLM call).
- `assert` — runs after the minister completes. Non-zero exit fails the step.

At the ritual level, `arrange` sets up shared context for all steps (e.g., `get_edict`, `get_court_status`).

**Creating custom rituals:**

Place YAML file in `.agents/rituals.yaml to define project-specific rituals:

```yaml
# .agents/rituals/my-ritual.yaml
my-ritual:
    description: "Custom workflow for my project"
    arrange:
        get_edict {{ .edict_id }}
    on_failure: retry
    max_retries: 2
    steps:
      - name: step-1
        minister: forge
        act: |
          Do something for the edict.
```

**Template Variables:**

Ritual `arrange` and `act` fields support template expansion using `{{ .variable }}` syntax:

| Variable | Description | Available In |
|----------|-------------|--------------|
| `.edict_id` | Unique identifier for the current edict | All rituals |
| `.session_id` | Current session identifier | All rituals |
| `.intent` | The edict's intent text | All rituals |
| `.scope` | Classified scope (S, M, L) | All rituals |
| `.phase` | Current edict phase | All rituals |
| `.ritual_name` | Name of the running ritual | All rituals |
| `.step_name` | Name of the current step | Step `act` only |
| `.minister_id` | ID of the executing minister | Step `act` only |

**Builtin Arrange Functions:**

The `arrange` field can reference builtin shell functions that output structured data:

| Function | Output | Description |
|----------|--------|-------------|
| `get_edict {{ .edict_id }}` | Markdown | Full edict details including intent, scope, phase, lings |
| `get_court_status` | Markdown | Current state of all ministers and active edicts |
| `get_manifests {{ .edict_id }}` | Markdown | List of forge manifests for the edict |
| `get_verdicts {{ .edict_id }}` | Markdown | Judge verdicts for the edict |
| `get_precedents` | Markdown | Recent censor precedents |

**Failure handling:**
- `retry` — Retry the step up to `max_retries` (default when set at ritual level)
- `"step_name"` — Jump to a named step (e.g., `on_failure: "forge"`)
- `abort` — Stop the ritual entirely

`on_failure` and `max_retries` can be set at the ritual level as defaults, and overridden per step.

**When all retries are exhausted**, the Shogunate invokes the `report_failure` ritual:

```yaml
report_failure:
    arrange:
        get_edict {{ .edict_id }}
        get_manifests {{ .edict_id }}
        get_verdicts {{ .edict_id }}
    steps:
      - name: summarize
        minister: strategist
        act: |
          Summarize the work completed and the failure that occurred.
          Identify what was attempted and where it went wrong.
          Distill a zhengming question for the Ruler to guide next steps.
```

**Loop exhaustion recovery:**

When a step exhausts all `max_retries` attempts, the Shogunate invokes the `report_failure` ritual. This ritual summarizes the work done, identifies the failure point, and distills a Zhengming question for the Ruler to decide the path forward.

---

## Tools Reference

### Chancellor Tools

**Edict Management:**
- `create_edict(intent, scope?)` — Create a new edict from the Ruler's intent
- `cancel_edict(edict_id)` — Cancel an edict, moving it to `cancelled` phase
- `get_edict_status(edict_id)` — Get current status, phase, and Lings for an edict
- `list_edicts(status?)` — List edicts, optionally filtered by status

**Zhengming (Clarification):**
- `request_zhengming(edict_id, question, priority?)` — Pause edict and ask Ruler for clarification
- `answer_zhengming(edict_id, answer)` — Provide Ruler's answer and resume the edict

**Ritual Management:**
- `list_rituals()` — List available rituals from `.agents/rituals/`
- `enact_ritual(ritual_name, edict_id)` — Start a ritual for an edict

**Observability:**
- `get_tian_events(edict_id?, event_type?, limit?)` — Query the Tian event ledger
- `asimisql(query)` — Execute raw SQL for advanced queries

> The Chancellor orchestrates ministers through rituals, not direct invocation. For ad-hoc minister work, use `enact_ritual` with a custom ritual.

### Strategist Tools

- `create_ling(edict_id, description, depends_on?)` — Create a new Ling (sub-task) for an edict
- `update_ling(ling_id, status?, description?)` — Update a Ling's status or description
- `list_lings(edict_id)` — List all Lings for an edict with their dependencies
- `request_zhengming(edict_id, question, priority?)` — Request clarification from the Ruler

### Forge Tools

- `create_manifest(edict_id, file_path, change_type)` — Record a new file change (create/modify/delete)
- `update_manifest(manifest_id, status)` — Update manifest status (staged → live → quenched)
- `commit_manifest(manifest_id, commit_sha)` — Mark manifest as committed with git SHA
- `request_zhengming(edict_id, question, priority?)` — Request clarification from the Ruler

### Judge Tools

- `record_verdict(edict_id, ling_id?, passed, details?)` — Record test results for an edict or specific Ling
- `reject_manifest(manifest_id, reason)` — Mark a manifest as rejected with reasoning
- `request_zhengming(edict_id, question, priority?)` — Request clarification from the Ruler

### Censor Tools

- `record_precedent(edict_id, approved, reasoning)` — Record ethics review outcome with reasoning
- `reject_manifest(manifest_id, reason)` — Mark a manifest as rejected with reasoning
- `request_zhengming(edict_id, question, priority?)` — Request clarification from the Ruler

### Marshal Tools

- `create_incident(description, severity, edict_id?)` — Create a new incident, optionally linked to an edict
- `resolve_incident(incident_id, resolution, root_cause?)` — Mark incident resolved with details
- `request_zhengming(edict_id, question, priority?)` — Request clarification from the Ruler

---

## Data Model

### Forge Manifests

When the Forge implements changes, it creates **Manifests** tracking each file modification:

- `staged` — Change created but not committed
- `live` — Committed to the repository
- `quenched` — Validated by the Judge
- `rejected` — Failed review (set by Judge or Censor via `reject_manifest`)

### Ling Status

Lings track their progress through the execution plan:

- `pending` — Not yet started
- `in_progress` — Currently being worked on
- `completed` — Successfully finished
- `blocked` — Waiting on dependency or Zhengming

### Judge Verdicts

The Judge creates **Verdicts** after running tests:
- `passed` — Tests succeeded
- `failed` — Tests failed

### Incident Severity

The Marshal tracks incidents using standard logging severity levels:

- `debug` — Tracing information, no impact
- `info` — Normal operational events
- `warn` — Warning conditions, potential issues
- `error` — Error conditions, requires attention

### Censor Precedents

The Censor records **Precedents** from ethics reviews:
- `approved` — Code meets standards
- `rejected` — Code violates principles

### Tian Events

The **Tian** (天, Heaven) ledger records all events in the edict lifecycle for auditing and debugging. **All events are logged to the session logs** for complete traceability:

- `edict_created`
- `edict_assigned`
- `phase_changed`
- `forge_committed`
- `manifest_committed`
- `manifest_rejected`
- `ritual_started`
- `ritual_completed`
- `ritual_failed`
- `ritual_step_started`
- `ritual_step_completed`
- `ritual_step_failed`
- `zhengming_needed`
- `zhengming_answered`
- `edict_cancelled`

---

## Using the Shogunate

### Starting the Shogunate

The Shogunate starts automatically when Asimi initializes. Ministers begin their processing loops and await tasks.

### Issuing Edicts

Simply describe what you want in natural language. The Chancellor will:

1. Create an edict capturing your intent
2. Classify the scope (S, M, L, XL)
3. Enact an appropriate ritual

When a ritual is enacted, the Ritual Runner takes over and executes the workflow.

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

```
You: Cancel the logout feature edict

Chancellor: [Uses cancel_edict tool]
  Edict issue-123 cancelled
```

### TUI — Tabs and Two Tempos

The TUI uses **vi-style tabs** with a top tab-bar. Asimi starts with two tabs: **Ruling** and **Hunting**. Each tab has its own chat history and input buffer — switching tabs preserves your place.

**Navigation**: `gt` / `gT` (next/prev tab), `1gt` / `2gt` (jump by number).

**Opening tabs**: `:tabnew <minister>` opens a read-only view of a minister's work log.

```
┌──────────────────────────────────────────────┐
│ [Ruling] [Hunting] [Forge]                   │
│──────────────────────────────────────────────│
│ [FORGE] Work Log                             │
│ ─────────────────────────────────────        │
│ [edict-a3f8] Implementing auth changes...    │
│  wrote: src/auth/handler.go                  │
│  wrote: src/auth/middleware.go               │
│  manifest: staged (2 files)                  │
│                                              │
│ [edict-b7c2] Adding logout endpoint...       │
│  wrote: src/auth/logout.go                   │
│  manifest: live (1 file, sha:e4f1a2)         │
│                                              │
└──────────────────────────────────────────────┘
```

#### Ruling Tab — Campaign Tempo

The Ruling tab is the court. You talk to the **Chancellor**, edicts flow through phases (classify → plan → forge → judge → censor), rituals orchestrate ministers. This is deliberate, structured work.

#### Hunting Tab — Skirmish Tempo

The Hunting tab is where agility lives — knight moves, L-shaped lateral thinking. You talk to **Confucius** (孔子), who sits outside the court hierarchy.

**Confucius** sees everything: edicts, ling, manifests, precedents, code — full read-only access across the entire system. His job is to distill the Ruler's *ren* (仁) — to help fuzzy intent crystallize into sharp edicts.

```
Hunting tab                          Ruling tab
───────────                          ──────────
You + Confucius                      Chancellor + Court
    │                                     │
    ├─ "this auth is brittle"             │
    ├─ Confucius probes, questions        │
    ├─ intent crystallizes                │
    ├─ creates edict ────────────────────►│
    │   [edict-a3f8b2c1](ruling://...)    ├─ Chancellor classifies
    │                                     ├─ ritual kicks off
    │                                     ▼
```

When Confucius creates an edict, the Hunting tab shows a clickable link. Clicking it switches to the Ruling tab, scrolled to where that edict's conversation begins.

#### Minister Tabs — Observation

`:tabnew <minister>` opens a read-only tab showing that minister's work log.
Useful for monitoring what a minister is doing without interrupting the court.

Examples:
- `:tabnew forge` — file changes, manifests, commit history
- `:tabnew judge` — test runs, verdicts
- `:tabnew censor` — review outcomes, precedents
- `:tabnew strategist` — battle plans, lings

| Aspect | Ruling | Hunting | Minister |
|--------|--------|---------|----------|
| **Counterpart** | Chancellor | Confucius | — (scroll-only log) |
| **Tempo** | Campaign (grand-campaign) | Skirmish (swift-strike) | Observation |
| **Purpose** | Execute edicts | Spot openings, distill intent | Monitor minister activity |
| **Edict creation** | Ruler dictates directly | Confucius distills from conversation | — |
| **Awareness** | Court ministers only | Full read-only across all state | Single minister's history |

---

## Architecture

### Core Types

```go
// Minister is the shared interface for all Shogunate ministers
type Minister interface {
    ID() string
    Role() string
    Scratchpad() string
    Tools() []Tool
    Tasks() chan<- *Task
    Events() chan<- *Event
    Run(ctx context.Context)
}

// Prompt carries the Ruler's message to the Chancellor
type Prompt struct {
    Message      string
    SessionID    string            // empty = new session
    ContextFiles map[string]string // files loaded via @ references
}

// Task carries work from Chancellor to a Minister
type Task struct {
    EdictID string
    Work    string
    Done    chan<- Result
}

// Result signals a Minister has completed a Task
type Result struct {
    MinisterID string
    Sealed     bool
    Output     string
    Err        error
}
```

Every minister embeds `MinisterBase`, which provides database access, session creation, event emission, and zhengming requests.

### Bootstrap Sequence

1. **`NewShogunate(db, cfg, runner, logger)`** — creates all six ministers with a shared `MinisterBase` (db, runner, logger). No LLM client yet.
2. **`ConfigureModel(model, sessionConfig, repoInfo)`** — injects the LLM client and config into every minister via `SetMinisterConfig()`.
3. **`Start(ctx)`** — loads rituals from embedded YAML + `.agents/rituals/`, launches each minister's `Run()` goroutine, starts the ritual guard polling loop, and invokes the `wakeup` ritual.
4. **TUI sends a `Prompt`** to `chancellor.Prompts` channel.
5. **Chancellor** creates an edict (`PhaseClassifying`), creates a `Session`, and streams the LLM response back to the TUI via notify callbacks.
6. **LLM tool calls** (`enact_ritual`) dispatch work to ministers through ritual workflows.

### Threading Strategy

The Shogunate uses a **channel-based actor pattern** built on Go's CSP primitives.

**Minister goroutines** (persistent): Each minister's `Run()` method blocks on a `select` over its task channel and the context's Done channel:

```go
func (m *SomeMinister) Run(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case task := <-m.tasks:
            m.processTask(ctx, task)
        case event := <-m.events:
            m.handleEvent(ctx, event)
        }
    }
}
```

**Task dispatch** (synchronous blocking): A ritual step sends a `Task` on the target minister's channel, then blocks on the per-call `Done` channel waiting for a `Result`. A **5-minute timeout** prevents indefinite blocking.

```
Ritual Runner / Chancellor       Minister goroutine
       │                              │
       ├── Task ──────────────────►   │
       │   (blocks on Done chan)       ├── processTask()
       │                              │
       │   ◄───────── Result ─────────┤
       │                              │
```

**Ritual runner**: Runs in a separate goroutine spawned by `StartRitual()`. Dispatches `Task` messages to ministers sequentially according to the ritual's step order.

**Ritual guard**: A polling loop (`runRitualGuard()`) that ticks at a configurable interval and runs the `RitualGuardRunner.Run()` method with a per-cycle timeout (default 30s). The guard processes events from the event registry.

**Cancellation**: `Shogunate.Stop()` cancels the root context, which propagates to all minister goroutines and the ritual guard.

### Event Registry

The Shogunate maintains a central event registry. Ministers subscribe to event types they care about and receive them as tasks. Events flow through the registry, not directly between ministers.

| Event | Emitted by | Subscribers |
|-------|-----------|-------------|
| `edict_created` | Chancellor | Strategist |
| `phase_changed` | any minister | Chancellor |
| `zhengming_raised` | any minister | Chancellor |
| `zhengming_answered` | Chancellor | originating minister |
| `manifest_committed` | Forge | Judge |
| `manifest_rejected` | Judge, Censor | Forge |
| `verdict_delivered` | Judge | Chancellor, Censor |
| `precedent_recorded` | Censor | Chancellor |
| `incident_created` | logger, Marshal | Chancellor |
| `ritual_step_started` | Ritual Runner | — |
| `ritual_step_completed` | Ritual Runner | — |
| `ritual_step_failed` | Ritual Runner | — |

A warning or error level log message is an incident. The logger emits `incident_created` directly — the Shogunate handles it from there.

### Tool-Level Isolation

Ministers are isolated by **tool catalogs** — each minister receives a different set of tools when its `Session` is created.

| Minister | File Tools | Shell | DB Tables | Orchestration Tools |
|----------|-----------|-------|-----------|-------------------|
| **Chancellor** | read-only (list, read, read_many, grep) | no | edicts, zhengming, forge_manifests, ling | create_edict, enact_ritual, asimi_sql |
| **Strategist** | read-only | no | ling | create_ling, list_ling, update_ling, request_zhengming |
| **Forge** | read-write (read, write, replace, list, read_many, grep) | yes | forge_manifests | create_manifest, update_manifest, commit_manifest, request_zhengming |
| **Judge** | edit (read, write, replace, list, read_many, grep) | yes | verdicts, forge_manifests | record_verdict, reject_manifest, request_zhengming |
| **Censor** | read-only | no | censor_precedents | record_precedent, reject_manifest, request_zhengming |
| **Marshal** | read-only | yes | incidents | create_incident, resolve_incident, request_zhengming |
| **Confucius** | read-only (all tables) | no | edicts, ling, forge_manifests, verdicts, censor_precedents | create_edict, request_zhengming |

Key constraints:
- **Strategist cannot write code** — it only plans (ling) and reads.
- **Censor cannot modify files** — it reviews and records precedents.
- **Chancellor cannot write files** — it orchestrates, never implements.
- **Only Forge and Judge have shell access** alongside the Marshal (for incident investigation).
- **Confucius sees all but changes nothing** — full read-only across every table; can only create edicts.

The `Session` also enforces **write protection** — a file must be read via `read_file` before it can be written via `write_file`. This is tracked per-session in `filesRead`.

### Constitutional Summary

| Concern | Prevention Mechanism |
|---------|---------------------|
| **Minister writes code it shouldn't** | Tool catalogs: only Forge and Judge get write/edit tools |
| **Minister runs shell commands it shouldn't** | Shell tool only granted to Forge, Judge, Marshal |
| **Intent drift** | Chancellor classifies edicts; Strategist plans before Forge implements |
| **Ambiguity propagation** | Zhengming: any minister can halt and request clarification |
| **Runaway minister** | 5-minute timeout on task dispatch; context cancellation propagates |
| **Tool call loops** | Session detects repeated identical tool calls after 3 attempts; invokes `report_failure` ritual |
| **Unauthorized file writes** | Write protection: file must be `read_file`'d before `write_file` or `replace_text` |
| **Ritual failure** | Step-level retry, goto, abort; Censor can regress to Strategist |
| **Precedent violation** | Censor logs all rulings to `censor_precedents` table as searchable case law |

---

## Configuration

### Shogunate Settings

Configured in `internal/config/types.go` under `ShogunateConfig`, set via `.agents/asimi.toml`:

```toml
[shogunate]
poll_interval = "5s"     # Ritual Guard polling interval (default: 5s)
ritual_timeout = "30s"   # Max time per ritual step (default: 30s)
```

### Project Rituals

Create custom rituals in `.agents/rituals/*.yaml` to define project-specific workflows.

---

## Troubleshooting

### "LLM not configured"

The model hasn't connected yet. Wait for authentication to complete.

### Edict stuck in a phase

Check for pending Zhengming requests — the Shogunate may be waiting for your input.

### Ritual failures

Check the Tian events to see what happened:

```
You: What happened with the logout feature?

Chancellor: [Uses get_tian_events tool for edict issue-123]
  Events show: ritual_step_failed at step "build" - compilation error
```

For advanced queries, use `asimisql` with raw SQL.

