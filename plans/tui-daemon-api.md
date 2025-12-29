# TUI ↔ Daemon API (HTTP over Unix Socket)

This document defines the local HTTP API between the Asimi TUI (client) and a new long‑running daemon (server) communicating over a Unix domain socket (UDS).

## Goals

- Move **LLM + tools + storage + sandbox orchestration** into a daemon process.
- Keep the **Bubble Tea UI, keybindings, and rendering** in the TUI process.
- Preserve current behavior (streaming, tool call UX, session persistence, approvals) while enabling future clients.

## Non‑Goals (for v1)

- Remote access (TCP listeners), multi-user auth, or exposing the daemon to the network.
- Stable public API guarantees beyond the local TUI/daemon boundary.

## Responsibilities Split

### Daemon (server)

- Owns configuration (user + project), model client creation, and session lifecycle.
- Runs the agent loop (streaming completion + tool scheduling/execution).
- Enforces sandbox/host boundaries and requests **user approval** for host commands.
- Persists sessions/history (SQLite) and serves session lists/resume data.

### TUI (client)

- Owns terminal UI state, modes, rendering, local help viewer.
- Initiates/controls sessions and turns via the daemon API.
- Presents host-command approval prompts and sends the user decision to the daemon.
- Handles purely local UX actions (open `$EDITOR`, open browser for OAuth, etc.).

## Transport

- **HTTP/1.1 over Unix domain socket**.
- All requests are **local**; the daemon must not bind to TCP in v1.
- Default socket path (Unix):
  - Prefer: `${XDG_RUNTIME_DIR}/asimi/asimi.sock`
  - Fallback: `~/.local/share/asimi/asimi.sock`
- Socket file permissions: `0600` (owner read/write), directory `0700`.

## Conventions

- Base path: `/v1`
- Request/response bodies are JSON unless noted.
- Timestamps are RFC3339 (`time.RFC3339Nano`).
- IDs are opaque strings.
- All non-stream responses include `request_id` (also echoed in `X-Request-Id`).

## Error Model

Non-stream endpoints return:

```json
{
  "request_id": "req_...",
  "error": {
    "code": "invalid_request",
    "message": "human readable",
    "details": { "any": "json" }
  }
}
```

SSE endpoints:
- If the request is invalid (JSON/schema), the daemon should respond with a normal JSON error and a non-2xx HTTP status.
- Once `stream.start` has been emitted, the daemon must emit a terminal `event: stream.end` (with `status=error` and an `error` object) and then close the connection.

### Error codes (v1)

- `invalid_request` (400)
- `not_found` (404)
- `conflict` (409)
- `timeout` (504)
- `cancelled` (409)
- `approval_required` (409)
- `internal` (500)

## Versioning

- Path versioning: `/v1/...`
- Daemon must expose `api_version` and `min_client_version` in `GET /v1/status`.
- Additive changes are allowed inside v1; breaking changes require `/v2`.

## Streaming (Server‑Sent Events)

Agent turns stream via **SSE** (`Content-Type: text/event-stream`).

- The server should emit keepalives: `event: ping` every ~15s.
- Closing the HTTP connection **cancels** the in-flight turn (daemon should respect `r.Context()`).

### SSE event envelope

Each event uses:

```
event: <type>
data: <json>

```

Example:

```
event: stream.start
data: {"request_id":"req_...","session_id":"sess_...","turn_id":"turn_...","ts":"2025-12-13T13:35:11.000Z"}

event: stream.cont
data: {"request_id":"req_...","session_id":"sess_...","turn_id":"turn_...","ts":"2025-12-13T13:35:11.123Z","delta":"partial text"}

event: stream.end
data: {"request_id":"req_...","session_id":"sess_...","turn_id":"turn_...","ts":"2025-12-13T13:35:12.000Z","status":"success"}

```

All event payloads include:

- `request_id` (string)
- `session_id` (string)
- `turn_id` (string)
- `ts` (RFC3339)

### Streaming event types

- `stream.start` – turn started
- `stream.cont` – incremental assistant text (0..N times)
- `stream.end` – terminal event (exactly once)

`stream.end` must include:
- `status`: `success|error|cancelled|max_tokens|max_turns`
- `error`: present only when `status=error`

Tool events (mirrors current TUI tool-call UX):

- `tool.scheduled`
- `tool.executing`
- `tool.waiting_for_approval`
- `tool.success`
- `tool.error`

### Streaming payload examples

`stream.start`:

```json
{
  "request_id": "req_...",
  "session_id": "sess_...",
  "turn_id": "turn_...",
  "ts": "2025-12-13T13:35:11.000Z",
  "provider": "anthropic",
  "model": "claude-3-5-sonnet-latest"
}
```

`stream.cont`:

```json
{
  "request_id": "req_...",
  "session_id": "sess_...",
  "turn_id": "turn_...",
  "ts": "2025-12-13T13:35:11.123Z",
  "delta": "partial text"
}
```

`stream.end` (success):

```json
{
  "request_id": "req_...",
  "session_id": "sess_...",
  "turn_id": "turn_...",
  "ts": "2025-12-13T13:35:12.000Z",
  "status": "success"
}
```

`stream.end` (error):

```json
{
  "request_id": "req_...",
  "session_id": "sess_...",
  "turn_id": "turn_...",
  "ts": "2025-12-13T13:35:12.000Z",
  "status": "error",
  "error": { "code": "internal", "message": "model request failed" }
}
```

## Data Types

### `SessionSummary`

```json
{
  "id": "2025-12-13-150405-acde1234",
  "created_at": "2025-12-13T13:25:00Z",
  "last_updated": "2025-12-13T13:35:10Z",
  "first_prompt": "Help me refactor…",
  "provider": "anthropic",
  "model": "claude-3-5-sonnet-latest",
  "working_dir": "/path/to/repo",
  "project_slug": "owner-repo",
  "message_count": 42
}
```

### `Message` (for resume/export)

```json
{
  "role": "user|assistant|system|tool",
  "parts": [
    { "type": "text", "text": "..." },
    {
      "type": "tool_call",
      "tool_call_id": "uuid",
      "name": "read_file",
      "arguments_json": "{\"path\":\"README.md\"}"
    },
    {
      "type": "tool_result",
      "tool_call_id": "uuid",
      "name": "read_file",
      "output": "file content…",
      "is_error": false
    }
  ]
}
```

### `ContextFile`

```json
{ "path": "relative/or/abs", "content": "..." }
```

### `ToolEvent` (streaming payload)

```json
{
  "request_id": "req_...",
  "session_id": "sess_...",
  "turn_id": "turn_...",
  "ts": "2025-12-13T13:35:11.123Z",
  "tool_call_id": "uuid",
  "tool_name": "run_in_shell",
  "status": "scheduled|executing|awaiting_approval|success|error",
  "input": "{\"command\":\"just test\"}",
  "output": "{\"stdout\":\"...\",\"exitCode\":\"0\"}",
  "error": ""
}
```

## Endpoints

### Status - Health & Version


- `GET /v1/status`
  - Used for ping & health
  - 200:
    ```json
    {
      "api_version": "v1",
      "daemon_version": "0.3.0",
      "min_client_version": "0.3.0",
      "daemon_pid": 12345,
      "started_at": "2025-12-13T13:20:00Z",
      "socket_path": "/run/user/1000/asimi/asimi.sock",
      "shell_runner": { "type": "podman|host", "container_id": "..." }
    }
    ```

### Config

- `GET /v1/config`
  - Returns the effective (merged) config the daemon is using, redacting secrets.

- `POST /v1/config:reload`
  - Reloads project config (`.agents/asimi.conf`) and user config.

### Models

- `GET /v1/models`
  - Query params:
    - `refresh=0|1` (optional; when `1` the daemon may call provider APIs)
  - 200: `{ "models": [ ... ] }`
  - Model object (matches current unified model view):
    ```json
    {
      "id": "claude-3-5-sonnet-latest",
      "display_name": "Claude 3.5 Sonnet",
      "provider": "anthropic",
      "description": "",
      "status": "active|ready|login_required|error"
    }
    ```

- `PUT /v1/llm/selection`
  - Body:
    ```json
    { "provider": "anthropic", "model": "claude-3-5-sonnet-latest" }
    ```
  - Behavior:
    - Persist selection to user config (equivalent to current `SaveConfig` behavior).
    - This sets defaults for newly created sessions; existing sessions are unchanged unless explicitly updated.

### Sessions

- `POST /v1/sessions`
  - Body (all optional; defaults come from config):
    ```json
    { "working_dir": "/path", "provider": "anthropic", "model": "claude-3-5-sonnet-latest" }
    ```
  - 201: `{ "session": <SessionSummary> }`

- `GET /v1/sessions`
  - Query params:
    - `limit` (int, optional)
    - `scope=branch|repo|all` (optional; default `branch`)
  - 200: `{ "sessions": [<SessionSummary>...] }`

- `GET /v1/sessions/{session_id}`
  - Query params:
    - `include=summary|messages|all` (default `summary`)
  - 200:
    ```json
    { "session": <SessionSummary>, "messages": [<Message>...] }
    ```

- `PATCH /v1/sessions/{session_id}`
  - Updates session-scoped settings (v1: LLM selection only).
  - Body:
    ```json
    {
      "provider": "anthropic",
      "model": "claude-3-5-sonnet-latest",
      "force_reset": true
    }
    ```
  - Behavior:
    - If `provider` or `model` changes and the session has non-system messages:
      - return `409 conflict` unless `force_reset=true`
      - when `force_reset=true`, the daemon resets history (equivalent to `:reset`) before continuing.

- `POST /v1/sessions/{session_id}:reset`
  - Clears conversation history but keeps the session container/config context (equivalent to current `ClearHistory`).

- `POST /v1/sessions/{session_id}:compact`
  - Compacts conversation history using the daemon’s compaction prompt.
  - 200: `{ "summary": "..." }`

- `GET /v1/sessions/{session_id}/context`
  - 200: context breakdown (matches current `/context` feature):
    ```json
    {
      "model": "claude-3-5-sonnet-latest",
      "total_tokens": 200000,
      "used_tokens": 12345,
      "system_prompt_tokens": 1000,
      "system_tools_tokens": 8000,
      "memory_files_tokens": 200,
      "messages_tokens": 3145,
      "free_tokens": 150000,
      "autocompact_buffer": 45000
    }
    ```

### Context Files (manual `@file` loading)

- `POST /v1/sessions/{session_id}/context-files`
  - Body:
    ```json
    { "path": "README.md", "content": "..." }
    ```
  - Adds/overwrites a context file for the next turn only (daemon clears after a turn finishes).

- `DELETE /v1/sessions/{session_id}/context-files`
  - Clears all context files.

### Turns (chat + tool execution)

- `POST /v1/sessions/{session_id}/turns:stream`
  - Request headers:
    - `Accept: text/event-stream`
    - `Content-Type: application/json`
  - Body:
    ```json
    {
      "prompt": "user text",
      "max_turns": 999
    }
    ```
  - 200: SSE stream of events (see **Streaming**).


### Host Command Approvals

When a tool requires host execution approval, the daemon emits:

- `event: tool.waiting_for_approval`
  - `data`: includes `approval_id`, `command`, and `tool_call_id`.

The TUI responds:

- `POST /v1/approvals/{approval_id}`
  - Body:
    ```json
    { "approved": true }
    ```
  - 200: `{ "status": "ok" }`

If denied, the daemon should:
- mark the tool call as failed (`tool.error`)
- continue the turn (the LLM can recover) or terminate the turn (implementation choice; must be consistent).

### Export

- `POST /v1/sessions/{session_id}/export`
  - Body:
    ```json
    { "type": "conversation|full" }
    ```
  - 200:
    ```json
    { "filename": "asimi-export-....md", "markdown": "# ..." }
    ```
  - TUI writes the file to a temp dir and opens `$EDITOR` (keeps editor execution out of the daemon).
