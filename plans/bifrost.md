# Replace langchaingo with Bifrost

## Context

langchaingo (`github.com/tmc/langchaingo`) is the current LLM abstraction layer. It's becoming stale and provides more abstraction than needed. We're replacing it with **Bifrost** (`github.com/maximhq/bifrost/core` v1.4.2) — a unified AI gateway library with 2.4k stars, 15+ providers, channel-based streaming, tool call support, and active maintenance (Feb 2026).

Bifrost provides a single `*Bifrost` client that routes to any provider (Anthropic, OpenAI, Ollama, llama.cpp, etc.) via an OpenAI-compatible unified interface. Streaming uses Go channels instead of callbacks.

## Files to Modify (~20 files)

| File | Change Scope |
|------|-------------|
| `go.mod` | Add bifrost, remove langchaingo |
| `main.go` | Replace `getModelClient()` → bifrost `Init()`, remove provider imports |
| `keyring.go` | Add bifrost `Account` interface implementation bridging config/keyring |
| `shogunate/session.go` | Core: replace all `llms.*` types, streaming, tool calls, token counting |
| `shogunate/minister.go` | Replace `llms.Model` field and signatures |
| `shogunate/shogunate.go` | Replace `ConfigureModel()` signature |
| `shogunate/strategist.go` | Replace direct `GenerateContent()` calls |
| `storage/schema.go` | `SessionData.Messages` type change |
| `storage/session_store.go` | Message serialization + migration from old format |
| `storage_adapter.go` | Replace message type constants |
| `tui.go` | `llmInitSuccessMsg` type, `switchModel()` |
| `resume.go` | Replace all `llms.` type references |
| `export.go` | Replace message type references |
| Test files | `session_test.go`, `export_test.go`, `tui_test.go`, `shutdown_test.go`, `resume_test.go` |

## Type Mapping

| langchaingo | Bifrost (`schemas.*`) |
|---|---|
| `llms.Model` | `*bifrost.Bifrost` |
| `llms.MessageContent` | `schemas.ChatMessage` |
| `llms.ChatMessageTypeSystem/Human/AI/Tool` | `schemas.ChatMessageRoleSystem/User/Assistant/Tool` |
| `llms.TextContent{Text: s}` | `schemas.ChatMessage{Content: &ChatMessageContent{ContentStr: &s}}` |
| `llms.Tool` | `schemas.ChatTool` |
| `llms.FunctionDefinition` | `schemas.ChatToolFunction` |
| `llms.ToolCall` | `schemas.ChatAssistantMessageToolCall` |
| `llms.ToolCallResponse` | `schemas.ChatMessage` with role `"tool"` + `ChatToolMessage` embed |
| `llms.ContentChoice` | `schemas.ChatNonStreamResponseChoice` |
| `llms.WithStreamingFunc(fn)` | `ChatCompletionStreamRequest()` → `chan *BifrostStreamChunk` |
| `llms.WithStreamingReasoningFunc(fn)` | Stream delta's reasoning field |
| `llms.CountTokens()` | Use tiktoken-go directly |
| `llms.GetModelContextSize()` | Maintain local map (already exists as `extendedModelContextSizes`) |

## Implementation Phases

### Phase 1: Add Bifrost, Create Account Bridge

1. `go get github.com/maximhq/bifrost/core@v1.4.2`
2. Add bifrost `Account` interface implementation to `keyring.go`:
   - `GetConfiguredProviders()` — returns providers from config + available credentials
   - `GetKeysForProvider()` — loads from keyring/env using existing `GetOauthToken()`, `GetAPIKeyFromKeyring()`
   - `GetConfigForProvider()` — returns base URL overrides, custom headers for OAuth
3. Replace `getModelClient()` in `main.go` with `bifrost.Init(ctx, config)`
4. Update `llmInitSuccessMsg` to carry `*bifrost.Bifrost`
5. Update `tui.go` to pass bifrost client through `ConfigureModel()`

### Phase 2: Core Session Migration

1. **`shogunate/session.go`** — the biggest change:
   - Replace `model llms.Model` → store `*bifrost.Bifrost` + provider/model strings
   - Replace `messages []llms.MessageContent` → `messages []schemas.ChatMessage`
   - Replace `toolDefs []llms.Tool` → `toolDefs []schemas.ChatTool`
   - Rewrite `generateLLMResponse()`: build `BifrostChatRequest`, call `ChatCompletionStreamRequest()`
   - Rewrite streaming: `for chunk := range ch` loop replacing callback closures
   - Rewrite `buildLLMTools()` to produce `[]schemas.ChatTool`
   - Rewrite `processToolCalls()` with bifrost tool call types
   - Rewrite `appendMessage()` to build `schemas.ChatMessage` from response
   - Update `SanitizeMessages()` to check bifrost role/content types
   - Update `CompactHistory()` message construction
   - Replace `countTokens()` to use tiktoken-go directly
   - Expand `extendedModelContextSizes` to be the primary context size source

2. **`shogunate/minister.go`** — replace `llms.Model` field → bifrost client
3. **`shogunate/shogunate.go`** — update `ConfigureModel()` signature
4. **`shogunate/strategist.go`** — replace `model.GenerateContent()` → `ChatCompletionRequest()`

### Phase 3: Storage Migration

1. **`storage/schema.go`** — change `SessionData.Messages` to `[]schemas.ChatMessage`
2. **`storage/session_store.go`** — add format detection in `LoadSession()`:
   - Try bifrost JSON format first
   - Fall back to langchaingo format with conversion (`"human"`→`"user"`, `"ai"`→`"assistant"`, extract text from `parts[]`)
   - Save in new format going forward
3. **`storage_adapter.go`** — update role constants

### Phase 4: Peripheral Files

1. `resume.go` — replace message type references
2. `export.go` — replace message inspection logic
3. `models.go` — optionally use `client.ListAllModels()` (existing HTTP calls work fine too)

### Phase 5: Tests + Cleanup

1. Update all test files: mock implementations, message construction, type assertions
2. Remove langchaingo from `go.mod` and its `replace` directive
3. Run `go mod tidy`
4. Run `just test`
5. Run `just lint`

## Key Architecture Changes

### Streaming: Callbacks → Channels
```
BEFORE: generateLLMResponse(ctx, streamingFunc, reasoningFunc)
        → model.GenerateContent(ctx, msgs, WithStreamingFunc(fn))
        → callback invoked per chunk

AFTER:  ch := client.ChatCompletionStreamRequest(ctx, req)
        → for chunk := range ch { notify(StreamChunkMsg{...}) }
```

### Provider Init: Per-Provider Constructors → Single Client
```
BEFORE: switch provider {
          case "anthropic": anthropic.New(opts...)
          case "openai": openai.New(opts...)
          ...
        }

AFTER:  bifrost.Init(ctx, BifrostConfig{Account: &keyring Account{config}})
        // Single client routes to any provider via req.Provider field
```

### Credentials: Per-Call Config → Account Interface
```
BEFORE: getModelClient() loads credentials and passes to provider constructor

AFTER:  keyring Account.GetKeysForProvider() called by bifrost on demand
        // Same keyring/OAuth logic, different entry point
```

## Verification

1. `just build` — compiles cleanly
2. `just test` — all existing tests pass
3. `just lint` — no lint errors
4. Manual test: start asimi, `/login` with Anthropic OAuth, send a prompt, verify streaming works
5. Manual test: switch to OpenAI provider, verify tool calls work
6. Manual test: configure Ollama, verify local model works
7. Verify `/resume` loads old sessions (storage migration)
8. Verify `/export` produces correct output
9. Verify `/compact` works with new message format
