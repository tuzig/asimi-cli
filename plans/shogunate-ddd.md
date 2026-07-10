# Shogunate DDD Implementation Plan

## Context

The `docs/shogunate_guide.md` describes the full Shogunate vision. Substantial code already exists (6 ministers, ritual engine, 13 DB tables, session system, TUI). This plan identifies the **gaps** between guide and code, organized as DDD bounded contexts, then sequences implementation by dependency.

### Key Design Decisions

1. **Edict sovereignty:** Only the Ruler creates edicts (via `SubmitEdict`). No minister — not even the Chancellor — can create edicts. The Chancellor can `update_edict` (refine intent), Confucius can *suggest* via zhengming.
2. **ConnectTo as primary interface:** The TUI attaches to edicts, ministers, and rituals via bidirectional `ConnectTo` streams. Every TUI tab is a `ConnectTo` stream. See `plans/daemon-db-migration.md` for the full API.
3. **LLM sessions are infrastructure:** Ministers manage their own LLM sessions internally. The API exposes them via `ConnectTo(minister_name)` for observability, never as primary interface.
4. **Brewing phase:** When the Ruler submits a raw prompt, the Chancellor brews it — distilling an edict name, asking zhengming if ambiguous, refining intent. The Ruler can skip brewing by specifying a ritual directly on `SubmitEdict`.
5. **Halted is a boolean flag** on the edict, orthogonal to phase. Any phase can be halted when zhengming is pending.

---

## Bounded Contexts

### BC1: Edict Lifecycle (The Court)
**Aggregate Root:** `Edict`
**Entities:** Edict, Ling, Zhengming, RulerCouncil
**Value Objects:** EdictPhase, LingStatus, ZhengmingPriority/Status
**Events:** edict_created, phase_changed, zhengming_needed/answered

**Status:** Largely built. Storage types, phase transitions, and Zhengming all work.
**Gaps:**
- Rename `PhaseClassifing` → `PhaseBrewing`
- Add `Halted bool` flag to Edict (not a separate phase)
- Enforce edict sovereignty: remove `create_edict` from all minister tool sets
- Chancellor needs `update_edict` tool (refine intent after brewing)

### BC2: Forge & Artifacts (Earth/Di)
**Aggregate Root:** `ForgeManifest`
**Events:** forge_committed, manifest_committed, manifest_rejected

**Status:** Complete. Forge minister + manifest lifecycle + tools all exist.

### BC3: Quality Assurance (Verdicts & Precedents)
**Aggregates:** `JudgeVerdict`, `CensorPrecedent`
**Events:** verdict_delivered, precedent_recorded

**Status:** Complete. Judge and Censor ministers fully implemented.

### BC4: Ritual Orchestration (The Ceremony Engine)
**Aggregate Root:** `RitualExecution`
**Entities:** RitualDef, RitualStepState
**Events:** ritual_started/completed/failed, ritual_step_started/completed/failed

**Status:** Core engine exists but **diverges significantly from guide**.
**Gaps (largest):**
- Guide describes **AAA steps** (`arrange`/`act`/`assert`) — code has `task`/`command` fields (`ritual.go:144-155`)
- No ritual-level `arrange`, `on_failure`, `max_retries` defaults on `RitualDef` (`ritual.go:121-127`)
- No per-step `scope`, `model`, `temperature`, `env` config
- Template expansion is primitive string replace — guide implies full `text/template`
- Builtin arrange functions missing (`get_edict`, `get_court_status`, `get_manifests`, `get_verdicts`, `get_precedents`)
- Missing embedded rituals: `grand-orchestration`, `wakeup`, `report_failure`
- Missing events: `manifest_committed`, `manifest_rejected`, `ritual_step_started/completed/failed`

### BC5: Observability & Events (Heaven/Tian)
**Aggregate Root:** `TianEvent`
**Entities:** TianEventDLQ, Incident, RitualGuardCheckpoint

**Status:** Tables and RitualGuard exist but event routing is a stub.
**Gaps:**
- No pub/sub Event Registry — guide describes minister subscriptions per event type
- No `Events()` channel on `Minister` interface
- RitualGuard `processEvent()` just logs, doesn't dispatch to ministers or trigger rituals
- Missing event types in constants (`shogunate.go:20-33`)

### BC6: Presentation (TUI Tabs via ConnectTo)
**Status:** TUI exists with vi modes but **no tab system**.
**Gaps:**
- No tab architecture — each tab should be a `ConnectTo` stream
- No `gt`/`gT`/`:tabnew` navigation
- Confucius minister not implemented (needed for Hunting tab)

---

## Implementation Phases

### Phase 0: Foundation Alignment
Small fixes that bring code terminology in line with the guide and enforce sovereignty.

| Deliverable | File |
|---|---|
| Rename `PhaseClassifing` → `PhaseBrewing`, add `Halted bool` to Edict | `storage/shogunate_schema.go` |
| Remove `create_edict` from all minister tool sets | `shogunate/chancellor.go` and others |
| Add `update_edict` tool to Chancellor (refine intent, not create) | `shogunate/chancellor.go` |
| Add missing event constants (`manifest_committed`, `manifest_rejected`, step events) | `shogunate/shogunate.go` |
| Rename `invoke_ritual` → `enact_ritual` in Chancellor tools | `shogunate/chancellor.go` |

### Phase 1: Ritual AAA Refactor (BC4 — highest impact)
The guide's ritual model is the backbone. Everything else depends on rituals working as described.

| Deliverable | File |
|---|---|
| Add `Arrange`, `Act`, `Assert` fields to `RitualStep` (keep `Task` as alias for backward compat) | `shogunate/ritual.go` |
| Add `Scope`, `Model`, `Temperature`, `Env` per-step config | `shogunate/ritual.go` |
| Add ritual-level `Arrange`, `OnFailure`, `MaxRetries` to `RitualDef` | `shogunate/ritual.go` |
| Refactor `executeStep()` to run arrange → act → assert | `shogunate/ritual.go` |
| Replace string substitution with `text/template` for template variables | `shogunate/ritual.go` |
| Implement builtin arrange functions (`get_edict`, `get_court_status`, etc.) | `shogunate/ritual_arrange.go` (new) |
| Rewrite embedded `swift-strike` and `grand-campaign` to AAA format | `shogunate/ritual.go` |
| Add embedded `wakeup`, `grand-orchestration`, `report_failure` rituals | `shogunate/ritual.go` |

### Phase 2: Event Registry (BC5)
Bring the guide's pub/sub event routing to life. Can run in parallel with Phase 3.

| Deliverable | File |
|---|---|
| Define `Event` struct and add `Events() chan<- *Event` to Minister interface | `shogunate/minister.go` |
| Implement `EventRegistry` with subscribe/dispatch | `shogunate/shogunate.go` |
| Wire minister subscriptions per guide's event table | `shogunate/shogunate.go` |
| Enhance RitualGuard to dispatch events to subscribers and trigger event-driven rituals | `shogunate/ritual_guard.go` |

### Phase 3: Confucius Minister (BC1 extension)
Can run in parallel with Phase 2. Required before Phase 4.

| Deliverable | File |
|---|---|
| Implement Confucius: full read-only access, suggests edicts via zhengming (no `create_edict`) | `shogunate/confucius.go` (new) |
| Register in `NewShogunate()` | `shogunate/shogunate.go` |
| Add `Prompts` channel (like Chancellor) for Hunting tab | `shogunate/confucius.go` |

### Phase 4: TUI Tab System (BC6)
Depends on Phase 3 (Confucius for Hunting tab). Each tab is a `ConnectTo` stream.

| Deliverable | File |
|---|---|
| Add `Tab` struct wrapping a `ConnectTo` stream (own content buffer, prompt state) | `tui.go` |
| Render top tab bar, create default Ruling + Hunting tabs | `tui.go` |
| Ruling tab = `ConnectTo(edict_id)`, Hunting tab = `ConnectTo("confucius")` | `tui.go` |
| Implement `gt`/`gT`/`Ngt` navigation keybindings | `tui.go` |
| `:tabnew <minister>` = `ConnectTo(minister_name)` observation tab | `commands.go`, `tui.go` |
| `:tabnew ritual <run_id>` = `ConnectTo(ritual_run_id)` | `commands.go`, `tui.go` |
| Support `since` on reconnect/tab resume (replay missed events) | `tui.go` |

### Phase 5: Lifecycle Polish
Depends on Phases 1 + 2.

| Deliverable | File |
|---|---|
| Invoke `wakeup` ritual at end of `Shogunate.Start()` | `shogunate/shogunate.go` |
| Invoke `report_failure` on retry exhaustion in `handleFailure()` | `shogunate/ritual.go` |
| Implement halt/resume: Zhengming sets `Halted=true`, answer clears it | `shogunate/minister.go`, `storage/shogunate_schema.go` |
| Emit step-level Tian events during ritual execution | `shogunate/ritual.go` |

---

## Dependency Graph

```
Phase 0 ──► Phase 1 (Ritual AAA) ──┬──► Phase 5 (Lifecycle)
                                    │
             Phase 2 (Events) ──────┘
             Phase 3 (Confucius) ──► Phase 4 (TUI Tabs)
```

Phases 1, 2, 3 can proceed in parallel after Phase 0.

---

## Verification

After each phase:
- `just test` — all existing tests pass
- `just lint` — no lint regressions
- For Phase 0: Verify no minister can create edicts; only `SubmitEdict` path works
- For Phase 1: Write ritual YAML matching guide syntax, verify it parses and runs with AAA
- For Phase 2: Emit an event, verify subscribers receive it
- For Phase 3: Send a prompt to Confucius, verify read-only + zhengming suggestion works (no edict creation)
- For Phase 4: Launch TUI, verify `gt`/`gT` switches tabs, `:tabnew forge` opens `ConnectTo("forge")` stream
- For Phase 5: Start Shogunate, verify wakeup ritual fires; exhaust retries, verify report_failure fires
