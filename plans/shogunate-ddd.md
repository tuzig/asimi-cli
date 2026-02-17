# Shogunate DDD Implementation Plan

## Context

The `docs/shogunate_guide.md` describes the full Shogunate vision. Substantial code already exists (6 ministers, ritual engine, 13 DB tables, session system, TUI). This plan identifies the **gaps** between guide and code, organized as DDD bounded contexts, then sequences implementation by dependency.

---

## Bounded Contexts

### BC1: Edict Lifecycle (The Court)
**Aggregate Root:** `Edict`
**Entities:** Edict, Ling, Zhengming, RulerCouncil
**Value Objects:** EdictPhase, LingStatus, ZhengmingPriority/Status
**Events:** edict_created, phase_changed, zhengming_needed/answered

**Status:** Largely built. Storage types, phase transitions, and Zhengming all work.
**Gaps:**
- Typo `classifing` fixed → `brewing` (renamed phase)
- Halted is now a boolean flag on the edict, not a separate phase

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
**Entities:** TianEventDLQ, MarshalIncident, RitualGuardCheckpoint

**Status:** Tables and RitualGuard exist but event routing is a stub.
**Gaps:**
- No pub/sub Event Registry — guide describes minister subscriptions per event type
- No `Events()` channel on `Minister` interface
- RitualGuard `processEvent()` just logs, doesn't dispatch to ministers or trigger rituals
- Missing event types in constants (`shogunate.go:20-33`)

### BC6: Presentation (TUI Tabs) + Confucius
**Status:** TUI exists with vi modes but **no tab system**.
**Gaps:**
- No Ruling/Hunting/Minister tab architecture
- No `gt`/`gT`/`:tabnew` navigation
- Confucius minister not implemented (needed for Hunting tab)

---

## Implementation Phases

### Phase 0: Foundation Alignment
Small fixes that bring code terminology in line with the guide.

| Deliverable | File |
|---|---|
| Rename `PhaseClassifing` → `PhaseBrewing`, add `Halted` bool flag | `storage/shogunate_schema.go` |
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
| Implement Confucius: full read-only access, can create edicts and raise zhengming | `shogunate/confucius.go` (new) |
| Register in `NewShogunate()` | `shogunate/shogunate.go` |
| Add `Prompts` channel (like Chancellor) for Hunting tab | `shogunate/confucius.go` |

### Phase 4: TUI Tab System (BC6)
Depends on Phase 3 (Confucius for Hunting tab).

| Deliverable | File |
|---|---|
| Add `Tab` struct (own content buffer, prompt, chat history, minister connection) | `tui.go` |
| Render top tab bar, create default Ruling + Hunting tabs | `tui.go` |
| Implement `gt`/`gT`/`Ngt` navigation keybindings | `tui.go` |
| Implement `:tabnew <minister>` command for observation tabs | `commands.go`, `tui.go` |
| Wire Ruling → Chancellor, Hunting → Confucius | `tui.go` |

### Phase 5: Lifecycle Polish
Depends on Phases 1 + 2.

| Deliverable | File |
|---|---|
| Invoke `wakeup` ritual at end of `Shogunate.Start()` | `shogunate/shogunate.go` |
| Invoke `report_failure` on retry exhaustion in `handleFailure()` | `shogunate/ritual.go` |
| Implement halt/resume: Zhengming → `halted` phase, answer → restore prior phase | `shogunate/minister.go`, `storage/shogunate_schema.go` |
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
- For Phase 1: Write ritual YAML matching guide syntax, verify it parses and runs
- For Phase 2: Emit an event, verify subscribers receive it
- For Phase 3: Send a prompt to Confucius, verify read-only + edict creation works
- For Phase 4: Launch TUI, verify `gt`/`gT` switches tabs, `:tabnew forge` opens observation
- For Phase 5: Start Shogunate, verify wakeup ritual fires; exhaust retries, verify report_failure fires
