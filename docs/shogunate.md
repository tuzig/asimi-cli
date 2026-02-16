# The Shogunate — Architecture Specification

> **Status**: Part A documents the implemented system. Part B sketches future design goals.

---

## Part A — What Exists Today

### I. Court Architecture

The Shogunate is a **coordinator-actor system**. A single `Shogunate` struct
owns the lifecycle of six ministers, a ritual registry, a ritual runner, and an event registry.
Ministers are persistent goroutines that receive work over channels and reply
with results.

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

**Core types** (`minister.go`):

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

Every minister embeds `MinisterBase`, which provides database access, session creation, event emission, and zhengming (clarification) requests.

#### Bootstrap Sequence

1. **`NewShogunate(db, cfg, runner, logger)`** — creates all six ministers with a shared `MinisterBase` (db, runner, logger). No LLM client yet.
2. **`ConfigureModel(model, sessionConfig, repoInfo)`** — injects the LLM client and config into every minister via `SetMinisterConfig()`.
3. **`Start(ctx)`** — loads rituals from embedded YAML + `.agents/rituals/`, launches each minister's `Run()` goroutine, starts the ritual guard polling loop, and invokes the `wakeup` ritual.
4. **TUI sends a `Prompt`** to `chancellor.Prompts` channel.
5. **Chancellor** creates an edict (`PhaseClassifying`), creates a `Session`, and streams the LLM response back to the TUI via notify callbacks.
6. **LLM tool calls** (`invoke_minister`, `invoke_ritual`) dispatch work to other ministers or start ritual workflows.

---

### II. Edict Lifecycle

An edict is a persistent record in SQLite (the `edicts` table). It tracks intent, phase, and seals.

**Creation**: When the Chancellor receives a `Prompt` with no `SessionID`, it calls `CreateEdict(edictID, intent)` which writes a row with `current_phase = PhaseClassifying`.

**Phase progression**:

```
Classifying → Planning → Forging → Judging → Censoring → Done
                                                    ↑
                                              (or Cancelled)
```

Ministers advance the phase by calling `UpdatePhase()` on their `MinisterBase`. The Chancellor can also regress to an earlier phase — for example, `RegressToForging()` resets rejected ling and moves back to `PhaseForging`.

**Seals**: The edict carries two seal columns — `chancellor_seal` and `censor_seal` — set via `SetChancellorSeal()` and `SetCensorSeal()`. Both must be set for an edict to be considered fully approved.

**Zhengming** (正名, rectification of names): Any minister can halt progress by calling `RequestZhengming(edictID, question, priority)`, which inserts a row into the `zhengming` table. The Chancellor presents the question to the Ruler and calls `AnswerZhengming()` + `AppendToIntent()` when answered.

---

### III. Ritual Protocol

A ritual is a YAML-defined workflow that orchestrates ministers through a series of steps. Two rituals are embedded in the binary:

**swift-strike** (S-size edicts) — a tight forge/judge loop:

```yaml
name: swift-strike
steps:
  - name: forge
    minister: forge
    task: |
      Implement the changes for edict {{ .edict_id }}.
      Focus on minimal, targeted changes.
    on_failure: retry
    max_retries: 3

  - name: judge
    minister: judge
    task: |
      Run tests and validate the changes for edict {{ .edict_id }}.
    depends_on: [forge]
    on_failure: goto
    on_failure_target: forge
```

**grand-campaign** (L-size edicts) — architecture-first with strict gatekeeping:

```yaml
name: grand-campaign
steps:
  - name: strategist
    minister: strategist
    task: Analyze and produce a Battle Plan...
    on_failure: zhengming

  - name: forge
    minister: forge
    depends_on: [strategist]
    on_failure: retry
    max_retries: 3

  - name: judge
    minister: judge
    depends_on: [forge]
    on_failure: goto
    on_failure_target: forge

  - name: censor
    minister: censor
    depends_on: [judge]
    on_failure: goto
    on_failure_target: strategist
```

**wakeup** — invoked as the last step of `Start()`, orients the court before the Ruler speaks:

```yaml
name: wakeup
steps:
  - name: orient
    minister: strategist
    before: |
      git_branch=$(git rev-parse --abbrev-ref HEAD)
      git_status=$(git status --short)
      git_log=$(git log --oneline -5)
      pending_edicts=$(asimi-sql "SELECT edict_id, current_phase FROM edicts WHERE current_phase NOT IN ('done','cancelled')")
      pending_zhengming=$(asimi-sql "SELECT edict_id, question FROM zhengming WHERE answer IS NULL")
    task: |
      Court is now in session. Here is the state of affairs:
      Branch: {{ .git_branch }}
      Working tree: {{ .git_status }}
      Recent commits: {{ .git_log }}
      Pending edicts: {{ .pending_edicts }}
      Unanswered zhengming: {{ .pending_zhengming }}

      Help the Ruler start his day by raising zhengming.
      Suggest 2-3 actionable options for the Ruler to pursue.
      Return them as a numbered list, each with a one-line description.

```

The Strategist raises zhengming directly — the Ruler sees the options and picks one to start the session.

**Event Registry**: The Shogunate maintains a central event registry. Ministers subscribe to event types they care about and receive them as tasks. Events flow through the registry, not directly between ministers.

| Event | Emitted by | Subscribers |
|-------|-----------|-------------|
| `edict_created` | Chancellor | Strategist |
| `phase_changed` | any minister | Chancellor |
| `zhengming_raised` | any minister | Chancellor |
| `zhengming_answered` | Chancellor | originating minister |
| `manifest_committed` | Forge | Judge |
| `verdict_delivered` | Judge | Chancellor, Censor |
| `precedent_recorded` | Censor | Chancellor |
| `incident_created` | logger, Marshal | Chancellor |

A warning or error level log message is an incident. The logger emits `incident_created` directly — the Shogunate handles it from there.

A ritual step can also subscribe to events via an `on_event` field — the step blocks until the named event fires, then proceeds with the event payload available as template variables.

Each step can have `before` and `after` scripts. A `before` script runs before the step executes — its variables become template inputs for the step's `task`. An `after` script runs after the step completes and receives the step's output as `$STEP_OUTPUT`.

**Step types**: `minister` (invoke a minister), `cmd` (shell command), `prompt` (LLM call), `gate` (wait for condition), `confirm` (require user approval).

**Failure modes**: `retry` (up to `max_retries`), `goto` (jump to a named step), `zhengming` (halt and ask the Ruler), `abort` (give up).

The `RitualRunner` executes steps sequentially, respecting `depends_on` ordering. Project-specific rituals can be added under `.agents/rituals/` and will be loaded at startup (overriding embedded rituals of the same name).

The Chancellor's `invoke_ritual` tool starts a ritual asynchronously — `RitualRunner.Start()` creates a `RitualExecution`, then `RitualRunner.Run()` proceeds in a background goroutine.

---

### IV. Threading Strategy

The Shogunate uses a **channel-based actor pattern** built on Go's CSP primitives.

**Minister goroutines** (persistent): Each minister's `Run()` method blocks on a `select` over its task channel and the context's Done channel:

```go
// Every minister follows this pattern:
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

**Task dispatch** (synchronous blocking): The Chancellor's `invoke_minister` tool sends a `Task` on the target minister's channel, then blocks on the per-call `Done` channel waiting for a `Result`. A **5-minute timeout** prevents indefinite blocking.

```
Chancellor goroutine          Minister goroutine
       │                              │
       ├── Task ──────────────────►   │
       │   (blocks on Done chan)       ├── processTask()
       │                              │
       │   ◄───────── Result ─────────┤
       │                              │
```

**Ritual runner**: Runs in a separate goroutine spawned by `StartRitual()`. It dispatches `Task` messages to ministers sequentially according to the ritual's step order.

**Ritual guard**: A polling loop (`runRitualGuard()`) that ticks at a configurable interval and runs the `RitualGuardRunner.Run()` method with a per-cycle timeout (default 30s). The guard processes events from the event registry.

**Cancellation**: `Shogunate.Stop()` cancels the root context, which propagates to all minister goroutines and the ritual guard.

---

### V. Tool-Level Isolation

Ministers are isolated not by filesystem namespaces but by **tool catalogs** — each minister receives a different set of tools when its `Session` is created.

| Minister | File Tools | Shell | DB Tables | Orchestration Tools |
|----------|-----------|-------|-----------|-------------------|
| **Chancellor** | read-only (list, read, read_many, grep) | no | edicts, zhengming, forge_manifests, ling | invoke_minister, invoke_ritual, create_edict, asimi_sql |
| **Strategist** | read-only | no | ling | insert_ling, list_ling, update_ling_status |
| **Forge** | read-write (read, write, replace, list, read_many, grep) | yes | forge_manifests | create_manifest, update_manifest, commit_manifest |
| **Judge** | edit (read, write, replace, list, read_many, grep) | yes | verdicts, forge_manifests | record_verdict, list_pending_manifests, update_manifest_status |
| **Censor** | read-only | no | censor_precedents | record_precedent, list_quenched_manifests, query_precedents |
| **Marshal** | read-only | yes | incidents | create_incident, resolve_incident, get_incident, get_manifest_by_commit |
| **Confucius** | read-only (all tables) | no | edicts, ling, forge_manifests, verdicts, censor_precedents | create_edict |

Key constraints enforced by this design:
- **Strategist cannot write code** — it only plans (ling) and reads.
- **Censor cannot modify files** — it reviews and records precedents.
- **Chancellor cannot write files** — it orchestrates, never implements.
- **Only Forge and Judge have shell access** alongside the Marshal (for incident investigation).
- **Confucius sees all but changes nothing** — full read-only across every table; can only create edicts.

The `Session` also enforces **write protection** — a file must be read via `read_file` before it can be written via `write_file`. This is tracked per-session in `filesRead`.

---

### VI. TUI — Tabs and Two Tempos

The TUI uses **vi-style tabs** with a top tab-bar. Asimi starts with two tabs: **Ruling** and **Hunting**. Each tab has its own chat history and input buffer — switching tabs preserves your place in both.

**Navigation**: `gt` / `gT` (next/prev tab), `1gt` / `2gt` (jump by number).

```
┌──────────────────────────────────────────────┐
│ [Ruling] [Hunting]                           │
│──────────────────────────────────────────────│
│ [CHANCELLOR] Session: edict-1739...          │
│ ─────────────────────────────────────        │
│ Chancellor: I'll analyze this request and    │
│ invoke the appropriate ritual...             │
│                                              │
│ ┌ InvokeMinister forge                       │
│ │ [implement auth changes...]                │
│ └                                            │
│                                              │
│ ┌ Ritual swift-strike                        │
│ │ Started [a3f8b2c1]                         │
│ └                                            │
│                                              │
│ > _                                          │
└──────────────────────────────────────────────┘
```

#### Ruling Tab — Campaign Tempo

The Ruling tab is the court. You talk to the **Chancellor**, edicts flow through phases (classify → plan → forge → judge → censor), rituals orchestrate ministers. This is deliberate, structured work.

#### Hunting Tab — Skirmish Tempo

The Hunting tab is where agility lives — knight moves, L-shaped lateral thinking. You talk to **Confucius** (孔子), a new role that sits outside the court hierarchy.

**Confucius** sees everything: edicts, ling, manifests, precedents, code — full read-only access across the entire system. His job is to distill the Ruler's *ren* (仁) — to help fuzzy intent crystallize into sharp edicts.

The flow between tabs:

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

When Confucius creates an edict, the Hunting tab shows a clickable link. Clicking it switches to the Ruling tab, scrolled to where that edict's conversation begins. The Hunting tab stays where you left it.

| Aspect | Ruling | Hunting |
|--------|--------|---------|
| **Counterpart** | Chancellor | Confucius |
| **Tempo** | Campaign (grand-campaign) | Skirmish (swift-strike) |
| **Purpose** | Execute edicts | Spot openings, distill intent |
| **Edict creation** | Ruler dictates directly | Confucius distills from conversation |
| **Awareness** | Court ministers only | Full read-only across all state |

#### Stream Messages

Stream message types flow from the Session to the TUI:

- `StreamStartMsg{SessionID}` — signals a new response is beginning
- `StreamChunkMsg{Text}` — incremental LLM output
- `StreamReasoningChunkMsg{Text}` — reasoning/thinking output
- `StreamCompleteMsg{}` — response finished normally
- `StreamInterruptedMsg{PartialContent}` — cancelled by user
- `StreamErrorMsg{Err}` — error occurred
- `StreamDoneMsg{}` — all processing complete
- `MinisterInvokingMsg` / `MinisterCompletedMsg` — minister dispatch tracking
- `RitualStepMsg` — ritual step progress
- `ZhengmingPendingMsg` — clarification needed from the Ruler
- `EventMsg` — event fired from the event registry
- `TabSwitchMsg{TabIndex}` — tab navigation
- `EdictLinkMsg{EdictID, TabIndex}` — clickable edict cross-reference from Hunting

---

### VII. Constitutional Summary

| Concern | Prevention Mechanism |
|---------|---------------------|
| **Minister writes code it shouldn't** | Tool catalogs: only Forge and Judge get write/edit tools |
| **Minister runs shell commands it shouldn't** | Shell tool only granted to Forge, Judge, Marshal |
| **Intent drift** | Chancellor classifies edicts; Strategist plans before Forge implements |
| **Ambiguity propagation** | Zhengming: any minister can halt and request clarification |
| **Runaway minister** | 5-minute timeout on `invoke_minister`; context cancellation propagates |
| **Tool call loops** | Session detects repeated identical tool calls after 3 attempts |
| **Unauthorized file writes** | Write protection: file must be `read_file`'d before `write_file` |
| **Ritual failure** | Step-level retry, goto, zhengming, abort; Censor can regress to Strategist |
| **Precedent violation** | Censor logs all rulings to `censor_precedents` table as searchable case law |

---

## Part B — Future: Deep Realm Isolation

The current system isolates ministers through tool catalogs at the application level. A future evolution would enforce isolation at the runtime level through the *San Cai* (三才) realm model:

**Context severance**: Ceremony goroutines would start from `context.Background()` rather than inheriting parent context values, preventing covert data leakage between realms (Tian/Di/Ren).

**Filesystem namespaces**: Each ceremony would operate in a chroot-isolated view — Forge sees the git repo but not test artifacts; Judge sees source as read-only plus ephemeral log space that is destroyed after a verdict.

**Middleware sanitization**: Data flowing between ceremonies would pass through realm translators. Direct Tian-to-Di (test results to code) transfers would be forbidden; instead, test failures would flow through Ren (intent) as sanitized summaries stripped of stack traces and line numbers, preventing the Forge from "guessing" at fixes based on leaked test output.

**Session continuity modes**: Each ceremony would declare whether its LLM session is `fresh` (new instance), `fork` (inherited with sanitization), or `continuation` (same session). The Royal Guard would reject `continuation` for cross-realm transitions.

These mechanisms would make the constitutional metaphor enforceable at the OS and runtime level rather than relying solely on application-level tool selection.
