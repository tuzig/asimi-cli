# Daemon Database Migration Plan

## Overview

Migrate from monolithic TUI with embedded DB to a client-server architecture where the daemon owns all database access.

**Goal:** TUI becomes a thin client with zero direct database dependencies.

## Current State

```
┌─────────────────────────────────────┐
│              TUI Process            │
│  ┌──────────┐    ┌───────────────┐  │
│  │ Session  │    │  Shogunate    │  │
│  │ Manager  │    │   Ministers   │  │
│  └────┬─────┘    └───────┬───────┘  │
│       │                  │          │
│       ▼                  ▼          │
│  ┌─────────────────────────────┐    │
│  │      storage.DB (sql.DB)    │    │
│  │      gorm.DB (GORM)         │    │
│  └─────────────────────────────┘    │
│            │                        │
│            ▼                        │
│      asimi.db (SQLite)              │
└─────────────────────────────────────┘
```

**Problems:**
- Two DB abstractions (sql.DB + gorm.DB) to same file
- Shogunate tightly coupled to TUI process
- Cannot run daemon independently
- No clean API boundary

## Target State

```
┌──────────────────┐                    ┌──────────────────────────┐
│    TUI Process   │                    │     Daemon Process       │
│                  │    WebRTC Data     │                          │
│  ┌────────────┐  │     Channel        │  ┌────────────────────┐  │
│  │   Client   │  │◄═══════════════════│══│     Service        │  │
│  │  (no DB)   │  │    (gRPC-over-     │  │                    │  │
│  └────────────┘  │     WebRTC)        │  │  ┌──────────────┐  │  │
│                  │                    │  │  │  Shogunate   │  │  │
│  - Render UI     │   ┌────────────┐   │  │  │  Ministers   │  │  │
│  - Handle input  │   │  Browser   │   │  │  └──────────────┘  │  │
│  - Stream output │   │   Client   │═══│══│                    │  │
│                  │   └────────────┘   │  │  ┌──────────────┐  │  │
│                  │                    │  │  │   DB Layer   │  │  │
│                  │                    │  │  │  (gorm only) │  │  │
│                  │                    │  │  └──────────────┘  │  │
│                  │                    │  └────────────────────┘  │
│                  │                    │            │             │
│                  │                    │            ▼             │
│                  │                    │      asimi.db (SQLite)   │
└──────────────────┘                    └──────────────────────────┘
```

**Transport:** gRPC over WebRTC data channels using `go.viam.com/utils/rpc`
- Enables local TUI + remote browser clients
- Full streaming support (bidirectional)
- P2P connection via WebRTC ICE

## Daemon API (gRPC Service)

### Design Principles

1. **Edict sovereignty:** Only the Ruler (human) creates edicts via `SubmitEdict`. Ministers can refine/update edicts but never create them. Confucius can *suggest* edicts via zhengming, but creation is the Ruler's prerogative.
2. **ConnectTo as primary interface:** The Ruler attaches to an edict's lifecycle via a bidirectional stream. Everything flows through it — court output, zhengming questions, ruler prompts. Like `tmux attach` for an edict.
3. **LLM sessions are infrastructure:** Each minister manages its own LLM sessions internally. Observe them via `ConnectTo(minister_name)`, never manage them directly.

### Edict Lifecycle Flow

```
User types prompt
       │
       ▼
  SubmitEdict(intent)  →  edict_id
       │
       ▼
  ConnectTo(edict_id)  →  bidirectional stream
       │
       ▼
  ┌─────────────────────────────────────────────┐
  │  BREWING                                    │
  │  Chancellor distils edict name from prompt, │
  │  asks zhengming if ambiguous, refines       │
  │  intent until ready for assembly            │
  ├─────────────────────────────────────────────┤
  │  Chancellor classifies size (S/L) and       │
  │  enacts ritual: swift-strike or             │
  │  grand-campaign                             │
  └─────────────────────────────────────────────┘
       │
       ▼
  planning → forging → judging → censoring → sealed
```

Any phase can be **halted** (boolean flag) when zhengming is pending. Resumes when Ruler answers via the `ConnectTo` stream.

### Service Definition

```protobuf
syntax = "proto3";

package asimi;

service Shogunate {
  // === Primary Interface ===

  // Ruler issues a new edict — the ONLY way edicts are created
  rpc SubmitEdict(SubmitEdictRequest) returns (EdictResponse);

  // Attach to any target — bidirectional stream, one primitive for all tabs
  //   ConnectTo(edict_id)      → Ruling tab: full bidirectional (prompt, zhengming, cancel)
  //   ConnectTo(minister_name) → Minister tab: observe + interrupt/join conversation
  //   ConnectTo(ritual_run_id) → Ritual tab: observe progress + can abort
  rpc ConnectTo(stream RulerMessage) returns (stream CourtMessage);

  // === Browse & Inspect (read-only, no stream) ===

  rpc GetEdict(GetEdictRequest) returns (EdictDetailResponse);
  rpc ListEdicts(ListEdictsRequest) returns (ListEdictsResponse);
  rpc GetTianEvents(GetTianEventsRequest) returns (TianEventsResponse);
  rpc ListRituals(Empty) returns (ListRitualsResponse);

  // === Lifecycle ===

  rpc Health(Empty) returns (HealthResponse);
  rpc Shutdown(Empty) returns (Empty);
}
```

### Message Definitions

```protobuf
// === Common ===

message Empty {}

// === Edict ===

message SubmitEdictRequest {
  string intent = 1;                       // The Ruler's words
  string issue_ref = 2;                    // Optional: linked issue/ticket
  map<string, string> context_files = 3;   // Files loaded via @ references
}

message EdictResponse {
  string edict_id = 1;
  string intent = 2;
  string current_phase = 3;   // brewing, planning, forging, judging, censoring, sealed, cancelled
  bool halted = 4;             // paused waiting on Zhengming (orthogonal to phase)
  string created_at = 5;
}

message GetEdictRequest {
  string edict_id = 1;
}

message EdictDetailResponse {
  string edict_id = 1;
  string intent = 2;
  string current_phase = 3;   // brewing, planning, forging, judging, censoring, sealed, cancelled
  bool halted = 4;             // paused waiting on Zhengming
  string created_at = 5;
  string updated_at = 6;
  repeated Ling lings = 7;
  RitualExecutionResponse active_ritual = 8;  // nil if none running
  repeated Zhengming pending_zhengming = 9;
}

message ListEdictsRequest {
  string phase = 1;           // optional filter
  int32 limit = 2;
}

message ListEdictsResponse {
  repeated EdictResponse edicts = 1;
}

message Ling {
  string ling_id = 1;
  string edict_id = 2;
  string description = 3;
  repeated string dependencies = 4;
  string status = 5;          // pending, in_progress, done, blocked
}

// === ConnectTo bidirectional stream ===

// Ruler → Court
message RulerMessage {
  oneof message {
    ConnectRequest connect = 1;         // Initial: what to attach to
    RulerPrompt prompt = 2;             // Ruler speaks (new instruction, clarification)
    ZhengmingAnswer zhengming_answer = 3; // Answer a minister's question
    CancelSignal cancel = 4;            // Interrupt/abort current work
  }
}

// One connect primitive — every TUI tab is a ConnectTo stream
message ConnectRequest {
  oneof target {
    string edict_id = 1;               // Ruling tab: full bidirectional
    string minister_name = 2;           // Minister tab: observe + can interrupt/join
    string ritual_run_id = 3;           // Ritual tab: observe progress + can abort
  }
}

message RulerPrompt {
  string text = 1;
  map<string, string> context_files = 2;
}

message ZhengmingAnswer {
  string request_id = 1;
  string answer = 2;
}

message CancelSignal {}

// Court → Ruler
message CourtMessage {
  oneof message {
    TextOutput text = 1;                // Minister's text output
    ThoughtOutput thought = 2;          // Minister's reasoning/thinking
    PhaseChange phase_change = 3;       // Edict phase transition
    ZhengmingQuestion zhengming = 4;    // Minister needs clarification
    ToolActivity tool_activity = 5;     // Tool call or result
    MinisterEvent minister_event = 6;   // Minister started/completed work
    RitualProgress ritual_progress = 7; // Ritual step progress
    ErrorOutput error = 8;
    EdictSealed sealed = 9;             // Edict completed
  }
}

message TextOutput {
  string minister_id = 1;
  string text = 2;
}

message ThoughtOutput {
  string minister_id = 1;
  string text = 2;
}

message PhaseChange {
  string from_phase = 1;
  string to_phase = 2;
}

message ZhengmingQuestion {
  string request_id = 1;
  string minister_id = 2;
  string question = 3;
  string priority = 4;       // normal, urgent
}

message ToolActivity {
  string minister_id = 1;
  string tool_name = 2;
  string arguments = 3;      // JSON (for call)
  string result = 4;         // For result
  bool is_error = 5;
  bool is_call = 6;          // true = call, false = result
}

message MinisterEvent {
  string minister_id = 1;
  string event = 2;          // "started", "completed", "failed"
  string detail = 3;
}

message RitualProgress {
  string ritual_name = 1;
  string step_name = 2;
  string status = 3;         // running, completed, failed, retrying
  int32 step_index = 4;
  int32 total_steps = 5;
}

message ErrorOutput {
  string message = 1;
}

message EdictSealed {
  string summary = 1;
}

// === Zhengming ===

message Zhengming {
  string request_id = 1;
  string edict_id = 2;
  string minister_id = 3;
  string question = 4;
  string answer = 5;
  string priority = 6;
  string status = 7;         // pending, answered, expired
}

// === Tian Events ===

message GetTianEventsRequest {
  string edict_id = 1;
  int32 limit = 2;
  int32 offset = 3;
}

message TianEventsResponse {
  repeated TianEvent events = 1;
}

message TianEvent {
  uint64 id = 1;
  string edict_id = 2;
  string event_type = 3;
  string timestamp = 4;
  map<string, string> payload = 5;
}

// === Rituals ===

message ListRitualsResponse {
  repeated RitualInfo rituals = 1;
}

message RitualInfo {
  string name = 1;
  string description = 2;
  int32 step_count = 3;
}

message GetRitualExecutionRequest {
  string execution_id = 1;
}

message RitualExecutionResponse {
  string execution_id = 1;
  string ritual_name = 2;
  string edict_id = 3;
  int32 current_step = 4;
  string state = 5;
  repeated StepState steps = 6;
}

message StepState {
  int32 index = 1;
  string name = 2;
  string status = 3;
  int32 retry_count = 4;
  string message = 5;
}

// === Health ===

message HealthResponse {
  string status = 1;          // "healthy", "degraded"
  string version = 2;
  int64 uptime_seconds = 3;
  map<string, int64> db_stats = 4;
}
```

### Minister Tool Boundaries (Sovereignty Rule)

| Minister | Can create edicts? | Key tools |
|----------|-------------------|-----------|
| Chancellor | **No** — can only `update_edict` (refine intent) | update_edict, enact_ritual, cancel_edict |
| Strategist | No | insert_ling, list_ling, update_ling_status + read-only files |
| Forge | No | create_manifest, update_manifest, commit_manifest + file tools + shell |
| Judge | No | insert_verdict, update_manifest_status + shell + files |
| Censor | No | record_precedent, query_precedents + read-only files |
| Marshal | No | log_incident, approve_hotfix + read-only shell |
| Confucius | **No** — can suggest via zhengming | request_zhengming + read-only files + asimisql (SELECT only) |

Only `SubmitEdict` (Ruler → API) creates edicts. This is enforced by not giving any minister a `create_edict` tool.

## WebRTC Transport

Using `go.viam.com/utils/rpc` for gRPC-over-WebRTC:

### Connection Flow

```
┌─────────────┐    1. Signal    ┌───────────────┐
│   Client    │ ───────────────►│  Signal Server│
│  (TUI/Web)  │                 │   (embedded)  │
│             │◄─────────────── │               │
└─────────────┘   2. ICE Info   └───────────────┘
      │                                ▲
      │ 3. WebRTC Data Channel         │
      │         (P2P)                  │
      ▼                                │
┌─────────────────────────────────────┴┐
│            Daemon Process            │
│  ┌──────────────────────────────┐    │
│  │    gRPC-over-WebRTC Server   │    │
│  └──────────────────────────────┘    │
└──────────────────────────────────────┘
```

### Server Setup (daemon)

```go
import (
    "go.viam.com/utils/rpc"
)

func main() {
    // Create gRPC service
    svc := asimi.NewAsimiDaemonService(daemonImpl)
    
    // WebRTC server with authentication
    webrtcOpts := rpc.NewWebRTCServerOptions(
        rpc.WithAuthTokenProvider(authProvider),
    )
    
    server := rpc.NewServer(
        rpc.WithWebRTCServerOptions(webrtcOpts),
    )
    
    // Register gRPC service
    asimi.RegisterAsimiDaemonServer(server.GRPCServer(), svc)
    
    // Listen on WebRTC + Unix socket
    server.Run(context.Background())
}
```

### Client Setup (TUI)

```go
func connect(addr string) (asimi.AsimiDaemonClient, error) {
    conn, err := rpc.Dial(
        context.Background(),
        addr,
        rpc.WithCredentials(rpc.Credentials{
            Type: "token",
            Data: authToken,
        }),
    )
    if err != nil {
        return nil, err
    }
    return asimi.NewAsimiDaemonClient(conn), nil
}
```

### Local vs Remote

| Connection | Transport | Use Case |
|------------|-----------|----------|
| `unix:///path/to/socket` | Unix socket | Local TUI (lowest latency) |
| `webrtc://<peer-id>` | WebRTC | Remote browser/TUI |
```

## Migration Phases

### Phase 1: Edict-First Interface Extraction (No Process Split Yet)

**Goal:** Create a Go `ShogunateService` interface matching the gRPC service, wire TUI to use it in-process. LLM sessions become internal — TUI never touches them directly.

1. **Define service interface**
   - `internal/daemon/service.go` — `ShogunateService` interface with `SubmitEdict`, `ConnectTo`, `GetEdict`, `ListEdicts`, etc.
   - No session CRUD in the interface — sessions are minister-internal

2. **Implement `ConnectTo` as an in-process stream**
   - `internal/daemon/connect.go` — bidirectional channel bridge
   - `RulerMessage` channel (TUI → Shogunate): prompts, zhengming answers, cancel
   - `CourtMessage` channel (Shogunate → TUI): text, thoughts, phase changes, zhengming questions, tool activity
   - Wire existing Chancellor prompt/result flow through this bridge

3. **Refactor TUI to use `ShogunateService`**
   - Replace direct `shogunate.Shogunate` calls with service interface
   - Replace direct `storage.DB` / `gorm.DB` calls with service queries
   - Each TUI tab becomes a `ConnectTo` stream to an edict

4. **Enforce edict sovereignty**
   - Remove `create_edict` from all minister tool sets
   - Chancellor keeps `update_edict` (refine intent based on classification)
   - Confucius suggests edicts via zhengming, not creation

5. **Verify with tests**
   - All existing tests pass
   - New service layer has unit tests
   - Test: only `SubmitEdict` creates edicts, minister tools cannot

### Phase 2: gRPC + WebRTC Implementation

**Goal:** Expose `ShogunateService` over gRPC with WebRTC transport.

1. **Generate protobuf code**
   - `api/proto/shogunate.proto` (from service definition above)
   - `api/gen/grpc/` — generated Go code

2. **Implement gRPC server with WebRTC transport**
   - `daemon/server.go` — `go.viam.com/utils/rpc` server
   - Map `ConnectTo` bidirectional stream to gRPC stream
   - Support Unix socket (local) + WebRTC (remote)

3. **Add daemon subcommand**
   - `asimi daemon` — start daemon process
   - `asimi daemon --webrtc` — enable WebRTC listener
   - `asimi daemon --socket /path/to/socket` — local Unix socket

4. **Implement gRPC client**
   - `client.go` — implements `ShogunateService` via gRPC
   - Transparent to TUI: same interface, remote transport

### Phase 3: Process Split

**Goal:** Run TUI and daemon as separate processes.

1. **TUI connects to daemon on startup**
   - Socket path from config or env
   - Fallback to embedded mode if daemon unavailable

2. **Handle daemon lifecycle**
   - TUI can start daemon if not running (optional)
   - Signal handling for graceful shutdown
   - Reconnection logic for `ConnectTo` streams

3. **Remove direct DB imports from TUI package**
   - `main.go` should not import `storage` or `shogunate` directly
   - All interaction through `ShogunateService` client

### Phase 4: Cleanup & Optimization

1. **Remove `ShogunateSchema` DDL** — GORM AutoMigrate is the source of truth
2. **Remove `storage.DB` abstraction** — GORM only
3. **Add request timeout/retry** — resilience patterns
4. **Update documentation**

## File Structure After Migration

```
.
├── api/
│   ├── proto/
│   │   └── shogunate.proto   # Edict-first service definition
│   └── gen/
│       └── grpc/
├── cmd/
│   ├── asimi/                # TUI entrypoint
│   │   └── main.go
│   └── asimi-daemon/         # Daemon entrypoint
│       └── main.go
├── daemon/
│   ├── server.go             # gRPC server (WebRTC + Unix socket)
│   ├── webrtc.go             # WebRTC configuration & auth
│   ├── service.go            # ShogunateService implementation
│   ├── connect.go            # ConnectTo stream bridge
│   ├── edict.go              # Edict operations
│   ├── db.go                 # GORM setup
│   └── shogunate/            # Moved from root (daemon-only)
│       ├── shogunate.go
│       ├── chancellor.go
│       ├── minister.go       # LLM sessions managed here (internal)
│       ├── ritual.go
│       └── ...
├── client.go                 # TUI - ShogunateService client (gRPC)
├── tui.go                    # TUI - main UI + tab system
├── status.go                 # TUI - status bar
├── commandline.go            # TUI - command line
├── ...                       # TUI - other flat files
├── web/                      # Browser client (optional)
│   ├── index.html
│   ├── main.ts
│   └── package.json
├── internal/
│   ├── config/               # Shared config
│   ├── daemon/               # ShogunateService interface (shared between client & server)
│   │   └── service.go
│   └── repo/                 # Shared repo utilities
└── storage/
    ├── models.go             # Shared GORM structs (Edict, Ling, etc.)
    └── migrations.go         # GORM AutoMigrate
```

**Principles:**
- `daemon/` = all daemon-only code (server, service, shogunate, ministers, LLM sessions)
- Root level = flat TUI code (thin client, no DB imports)
- `internal/daemon/` = `ShogunateService` interface shared by client and server
- `storage/` = shared GORM model types
- LLM sessions live entirely inside `daemon/shogunate/` — never exposed as primary API
- TUI interacts only through edicts via `ConnectTo` streams

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Latency from IPC | Local TUI uses Unix socket; WebRTC for remote only |
| Daemon crash loses in-flight requests | Persist state early; recovery on restart |
| Schema migration across versions | Version the API; daemon handles migrations |
| Socket permissions | Use user-local socket path (XDG_RUNTIME_DIR) |
| Backward compatibility | Support embedded mode for single-process usage |
| WebRTC connection establishment | ICE candidates, TURN server fallback |
| WebRTC auth/authorization | Token-based auth in WebRTC handshake |
| Browser compatibility | Use go.viam.com/utils/rpc browser client library |

## Dependencies

| Package | Purpose |
|---------|---------|
| `go.viam.com/utils/rpc` | gRPC over WebRTC data channels |
| `google.golang.org/grpc` | gRPC framework (transitive) |
| `gorm.io/gorm` | ORM for SQLite |
| `modernc.org/sqlite` | Pure Go SQLite driver |

## Testing Strategy

1. **Service layer tests** - mock repository
2. **Repository tests** - in-memory SQLite
3. **gRPC integration tests** - full daemon with test client
4. **E2E tests** - TUI + daemon process pair

## Timeline Estimate

- **Phase 1:** 2-3 days (refactoring, no behavior change)
- **Phase 2:** 2-3 days (gRPC implementation)
- **Phase 3:** 1-2 days (process split)
- **Phase 4:** 1 day (cleanup)

**Total:** ~1 week
