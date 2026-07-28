# Roadmap

Below is what is left to develop:

## V0.10.0 (in progress)

### Court Alignment Cleanup

- Fix suggest_edict zhengming routing to Secretary tab (e693)
- Update stale docs referencing consult_minister (e707)

### UX Fixes

- Fix TUI garbage characters / rendering artifacts (e697)
- CTRL-C on ritual tab should resume (e681)

### Autonomous Execution Modes

- Headless Prompt Mode (`-p` flag) — non-interactive execution with stdout output and exit codes, `--max-turns` for benchmark control (e684)
- Isolated Host Mode (`--isolated-host` flag) — bypass podman sandbox and approval gates for already-isolated environments like terminal-bench and CI (e683)
- Handsoff Mode (`--handsoff` flag) — auto-answer zhengming with the recommended option, enabling fully autonomous runs without human intervention (e682)

### Postponed (from 0.10.0)

- Realm-Aware File Permissions — `RealmGuard` system for earth/intent file access control; proved more complex than expected, work stored on `realm-aware-permissions` branch (e695, e696, e698, e699, e702)

## V1.0

### Court Alignment

- Realign ministers to full 三省 (Three Departments) alignment (https://github.com/afittestide/asimi-cli/issues/154)
- Sharpen:
  - Roles: Improving definitions & translating to 文言文 (https://github.com/afittestide/asimi-cli/issues/156)
  - Tools (https://github.com/afittestide/asimi-cli/issues/155)
  - DB Schema — sharpen edicts, manifests, verdicts, precedents & seals (https://github.com/afittestide/asimi-cli/issues/152)

### UI Improvements

- Sharpen the modes (https://github.com/afittestide/asimi-cli/issues/157)
- Replace the external $EDITOR with an internal vim clone
- Thinking has 5 rows of live scrolling reasoning
- "Stick" important messages to the top so they don't scroll away — the ruler always knows what step the ritual is in
- Support folding of steps — when a ritual runs, only the current step should be open, the rest folded
- Replace file should show what was replaced

### MCP Support (https://github.com/afittestide/asimi-cli/issues/158)

### Skills Support (https://github.com/afittestide/asimi-cli/issues/159)

## V1.1

### Full Parallelism

- Use git worktrees to enable concurrent ritual execution (https://github.com/afittestide/asimi-cli/issues/153)
- Seal edict command with a UI for the user to review and approve or chat about changes

### Skills Support

- Skill marketplace discovery via `https://agentskills.io/llms.txt`
- Fetch and cache remote skills into `.agents/skills/`
- Skill versioning and update detection

## V1.2

Improved ministers permissions through realms alignment.
The repo is not only earth, but earth and intent.
Tests and docs are two examples for Intent content in the repo.
Earth is just part of the repo, the part with code that is part of the program (https://github.com/afittestide/asimi-cli/issues/160).

Update swift-strike to TDD

## V2.0

V2.0 turns Asimi from a coding agent into a governing agent. A `mandate_url`
config points to a Mandate server that bridges the external world and the
Court. The Mandate server holds an OpenAPI spec defining the three realms and
serves their data as HTTP resources. Earth is always git — local and universal.
Heaven and Intent are a ritual-updated cache maintained by the Mandate server,
fed by webhooks and cron jobs — CI results, GitHub issues, TODO comments, brand
checks, website analytics.

The Court is a process engine (seal chain, ritual
DAGs, permissions) that reads the spec at startup and pulls realm data from
the Mandate server as ritual context.
The second mandate server — for digital marketers — proves the abstraction.
