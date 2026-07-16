# Roadmap

Below is what is left to develop:

## V0.9 

### Bug Fixes & Polish (active, close to done)

- Fix Zhengming answering-mode height calculation e643 *(needs ruler seal)*
- Fix double notification on ritual cancellation e651
- Fix await_ruler_seal comma-separated manifest paths e637
- Pre-create ritual tab on "Implement" e654
- Fix "minister not found" on ritual tabs e647
- PodmanRunner: per-command exec + concurrent scheduler e657

### Court Alignment (low-hanging v1.0 foundations)

- Rename `invoke_minister` → `consult_minister`, available to all e653

## V1.0

### Court Alignment

- Realign the ministers e629
- Sharpen:
-- Roles: Improving definitions & translating to imperial chinesse e660
-- Tools e653
-- DB Schema - sharpen edicts, manifests (e524), verdicts, precedents & seals

### Full parallalism

- Use worktrees to enable concurrent ritual execution e531
- Seal edict command with a UI for the user to review and aprrove or chat about changes
- Multiple minister tabs

### UI improvments

- Sharpen the modes e645
- Replace the external $EDITOR with an internal vim clone
- Thinking has 5 rows of live scrolling reasoning
- "stick" to the top important messages so they don't scroll away. This way the ruler always know what step the ritual is in
- Support folding of steps. When a ritual runs, only the current step should be open, the rest folder
- Replace file should show what was replaced

### MCP Support e661

### Skills Support e662

## V1.1

### Infrastructure
- PodmanRunner: per-command exec + concurrent tool scheduler e657
- Rename `shogunate/` Go package to `court/` (excluded from e629)

### Remote Skills
- Skill marketplace discovery via `https://agentskills.io/llms.txt`
- Fetch and cache remote skills into `.agents/skills/`
- Skill versioning and update detection

## V2.0

V2.0 turns Asimi from a coding agent into a governing agent. A `mandate_url`
config points to a Mandate server that bridges the external world and the
Court. The Mandate server holds an OpenAPI spec defining the three realms and
serves their data as HTTP resources. Earth is always git — local and universal.
Heaven and Intent are a ritual-updated cache maintained by the Mandate server,
fed by webhooks and cron jobs — CI results, GitHub issues, TODO comments, brand
checks, website analytics.

The Court is a process engine (seal chain, ritual
DAGs, permissions) that reads the spec at startup and pulls realm data from the
Mandate server as ritual context.
The second mandate server - for digital marketers - proves the abstraction.
