# Zhengming Tool: Lock-and-Wait + Multiple Questions

## Context

The zhengming tool (`shogunate/tools/zhengming.go`) lets LLM ministers request clarification from the user. Two TODOs need implementing:

1. **Line 34** — `Call()` should block until the user answers (currently returns immediately with "pending")
2. **Lines 94-96** — Schema should support multiple questions, each with 2-4 answer options (recommended first)

## Approach

**Channel-based wait** (not DB polling) — matches existing `InvokeMinisterTool` pattern in the codebase. A waiter registry on `MinisterBase` maps `requestID → chan string`. The tool blocks on the channel; `HandleZhengmingResponse` unblocks it.

**No DB schema changes** — JSON-encode the structured questions array into the existing `Question` text field. Same for answers in `Answer` field.

## Changes

### 1. `shogunate/tools/zhengming.go` — Main tool changes

- Add `ZhengmingQuestion` and `ZhengmingAnswer` types
- Add `ZhengmingWaiter` interface with `WaitForAnswer(ctx, requestID) (string, error)`
- Add `Waiter ZhengmingWaiter` field to `RequestZhengmingTool`
- Rewrite `Call()`: parse `questions` array, validate 2-4 options each, JSON-encode into question string, call `RequestZhengming`, notify, then **block** on `Waiter.WaitForAnswer()`. Return answered result with the user's response
- Update `ParameterSchema()`: replace single `question` string with `questions` array of `{question, options}` objects
- Update `Format()`: iterate over questions array, show truncated preview of each

### 2. `shogunate/minister.go` — Waiter registry

- Add `zhengmingWaiters map[string]chan string` and `zhengmingMu sync.Mutex` to `MinisterBase`
- Add `RegisterZhengmingWaiter(requestID) <-chan string`
- Add `ResolveZhengmingWaiter(requestID, answer) bool`
- Add `WaitForAnswer(ctx, requestID) (string, error)` — blocks on channel, respects context cancellation and DB timeout, cleans up on cancel, marks expired in DB
- Rename `ZhengmingPendingMsg.Question` → `Questions` (carries JSON array)

### 3. `shogunate/chancellor.go` — Wiring

- Wire `Waiter: c` in `RequestZhengmingTool` construction (line 463)
- In `HandleZhengmingResponse`: call `c.ResolveZhengmingWaiter(requestID, answer)` to unblock the waiting tool
- Update notify wrapper: `question` → `questions` parameter name

### 4. `shogunate/strategist.go` — Compat (line 147)

- The strategist calls `RequestZhengming` directly with a plain question string. Since `RequestZhengming` signature stays the same (accepts a string), we JSON-encode a single question with options: `[{"question":"...","options":["Provide details","Cancel edict"]}]`

### 5. `shogunate/confucius.go` — Display compat (line 315)

- Parse `z.Question` as JSON `[]ZhengmingQuestion`, show first question text. Fall back to raw string for old records.

### 6. `shogunate/minister_test.go` — Tests

- `TestZhengmingWaitForAnswer` — register waiter, resolve from goroutine, verify answer returned
- `TestZhengmingWaitForAnswer_ContextCancelled` — cancel context, verify error
- `TestZhengmingMultipleQuestions` — verify JSON round-trip of questions array

## Verification

```bash
just test          # All tests pass
just lint          # No lint errors
just build         # Builds cleanly
```
