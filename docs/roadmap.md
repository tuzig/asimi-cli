# Roadmap

Below is what is left to develop:

## V1.0

### Court Alignment

- Rename `invoke_minister` → `consult_minister`, make available to all ministers (https://github.com/afittestide/asimi-cli/issues/155)
- Realign ministers to full 三省 (Three Departments) alignment (https://github.com/afittestide/asimi-cli/issues/154)
- Sharpen:
  - Roles: Improving definitions & translating to 文言文 (https://github.com/afittestide/asimi-cli/issues/156)
  - Tools (https://github.com/afittestide/asimi-cli/issues/155)
  - DB Schema — sharpen edicts, manifests, verdicts, precedents & seals (https://github.com/afittestide/asimi-cli/issues/152)

### Full Parallelism

- Use git worktrees to enable concurrent ritual execution (https://github.com/afittestide/asimi-cli/issues/153)
- Seal edict command with a UI for the user to review and approve or chat about changes
- Multiple minister tabs

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

### Skills Support

- Skill marketplace discovery via `https://agentskills.io/llms.txt`
- Fetch and cache remote skills into `.agents/skills/`
- Skill versioning and update detection

## V1.2

Improved realms alignment. The repo is not only earth, but earth and intent.
Tests and docs are two examples for Intent content in the repo.
Earth is just part of the repo, the part with code that is part of the program (https://github.com/afittestide/asimi-cli/issues/160).

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
The second mandate server — for digital marketers — proves the abstraction.
