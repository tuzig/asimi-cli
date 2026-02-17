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

### Service Definition

```protobuf
syntax = "proto3";

package asimi;

service AsimiDaemon {
  // === Session Management ===
  
  // Create a new session, returns session ID
  rpc CreateSession(CreateSessionRequest) returns (SessionResponse);
  
  // Get session by ID
  rpc GetSession(GetSessionRequest) returns (SessionResponse);
  
  // List sessions for current branch
  rpc ListSessions(ListSessionsRequest) returns (ListSessionsResponse);
  
  // Update session (messages, context files)
  rpc UpdateSession(UpdateSessionRequest) returns (Empty);
  
  // Delete session
  rpc DeleteSession(DeleteSessionRequest) returns (Empty);
  
  // === Conversation ===
  
  // Send a message and stream the response
  rpc SendMessage(SendMessageRequest) returns (stream StreamChunk);
  
  // Cancel ongoing message generation
  rpc CancelMessage(CancelMessageRequest) returns (Empty);
  
  // Get message history for session
  rpc GetMessageHistory(GetMessageHistoryRequest) returns (MessageHistoryResponse);
  
  // === Prompt/Command History ===
  
  // Get prompt suggestions (autocomplete)
  rpc GetPromptSuggestions(GetPromptSuggestionsRequest) returns (PromptSuggestionsResponse);
  
  // Add prompt to history
  rpc AddPromptHistory(AddPromptHistoryRequest) returns (Empty);
  
  // Get command history
  rpc GetCommandHistory(GetCommandHistoryRequest) returns (CommandHistoryResponse);
  
  // Add command to history
  rpc AddCommandHistory(AddCommandHistoryRequest) returns (Empty);
  
  // === Edict Management ===
  
  // Create a new edict
  rpc CreateEdict(CreateEdictRequest) returns (EdictResponse);
  
  // Get edict status
  rpc GetEdict(GetEdictRequest) returns (EdictResponse);
  
  // List edicts with optional filter
  rpc ListEdicts(ListEdictsRequest) returns (ListEdictsResponse);
  
  // Cancel edict
  rpc CancelEdict(CancelEdictRequest) returns (Empty);
  
  // === Shogunate Events ===
  
  // Subscribe to shogunate events (edict phase changes, ritual progress, etc.)
  rpc SubscribeEvents(SubscribeEventsRequest) returns (stream ShogunateEvent);
  
  // Emit an event (for tools)
  rpc EmitEvent(EmitEventRequest) returns (Empty);
  
  // === Zhengming (Clarification Requests) ===
  
  // Get pending clarification requests
  rpc GetPendingZhengming(GetPendingZhengmingRequest) returns (ZhengmingListResponse);
  
  // Answer a clarification request
  rpc AnswerZhengming(AnswerZhengmingRequest) returns (Empty);
  
  // === Rituals ===
  
  // List available rituals
  rpc ListRituals(Empty) returns (ListRitualsResponse);
  
  // Get ritual execution status
  rpc GetRitualExecution(GetRitualExecutionRequest) returns (RitualExecutionResponse);
  
  // === Repository/Branch ===
  
  // Get or create current repository/branch context
  rpc GetContext(Empty) returns (ContextResponse);
  
  // Set current branch
  rpc SetBranch(SetBranchRequest) returns (Empty);
  
  // === Health & Lifecycle ===
  
  // Check daemon health
  rpc Health(Empty) returns (HealthResponse);
  
  // Graceful shutdown
  rpc Shutdown(Empty) returns (Empty);
}
```

### Message Definitions

```protobuf
// === Common Types ===

message Empty {}

message Session {
  string id = 1;
  string created_at = 2;      // RFC3339 timestamp
  string last_updated = 3;
  string first_prompt = 4;
  string provider = 5;
  string model = 6;
  string working_dir = 7;
  string project_slug = 8;
  int32 message_count = 9;
}

message Message {
  string role = 1;            // "human", "ai", "system", "tool"
  string content = 2;         // JSON-encoded content parts
  int32 sequence = 3;
}

message Edict {
  string edict_id = 1;
  string session_id = 2;
  string issue_ref = 3;
  string intent = 4;
  string current_phase = 5;   // classifing, planning, forging, judging, censoring, deploying, sealed, cancelled
  string created_at = 6;
  string updated_at = 7;
}

message Ling {
  string ling_id = 1;
  string edict_id = 2;
  string description = 3;
  repeated string dependencies = 4;
  string status = 5;          // pending, in_progress, done, blocked
}

message Zhengming {
  string request_id = 1;
  string edict_id = 2;
  string minister_id = 3;
  string question = 4;
  string answer = 5;
  string priority = 6;        // normal, urgent
  string status = 7;          // pending, answered, expired
  string timeout_at = 8;
}

// === Session Messages ===

message CreateSessionRequest {
  string provider = 1;
  string model = 2;
  string working_dir = 3;
  string system_prompt = 4;
  map<string, string> context_files = 5;  // path -> content
}

message SessionResponse {
  Session session = 1;
}

message GetSessionRequest {
  string session_id = 1;
}

message ListSessionsRequest {
  int32 limit = 1;
  string branch_name = 2;     // optional, defaults to current
}

message ListSessionsResponse {
  repeated Session sessions = 1;
}

message UpdateSessionRequest {
  string session_id = 1;
  repeated Message messages = 2;
  map<string, string> context_files = 3;
}

message DeleteSessionRequest {
  string session_id = 1;
}

// === Conversation Messages ===

message SendMessageRequest {
  string session_id = 1;
  string prompt = 2;
  map<string, string> context_files = 3;  // additional context for this message
}

message StreamChunk {
  oneof chunk {
    TextChunk text = 1;
    ReasoningChunk reasoning = 2;
    ToolCallChunk tool_call = 3;
    ToolResultChunk tool_result = 4;
    ErrorChunk error = 5;
    DoneChunk done = 6;
  }
}

message TextChunk {
  string text = 1;
}

message ReasoningChunk {
  string text = 1;
}

message ToolCallChunk {
  string tool_name = 1;
  string arguments = 2;       // JSON
}

message ToolResultChunk {
  string tool_name = 1;
  string result = 2;
  bool is_error = 3;
}

message ErrorChunk {
  string message = 1;
}

message DoneChunk {
  string final_text = 1;      // complete response text
}

message CancelMessageRequest {
  string session_id = 1;
}

message GetMessageHistoryRequest {
  string session_id = 1;
}

message MessageHistoryResponse {
  repeated Message messages = 1;
}

// === History Messages ===

message GetPromptSuggestionsRequest {
  string prefix = 1;
  int32 limit = 2;
}

message PromptSuggestionsResponse {
  repeated string prompts = 1;
}

message AddPromptHistoryRequest {
  string prompt = 1;
}

message GetCommandHistoryRequest {
  int32 limit = 1;
}

message CommandHistoryResponse {
  repeated string commands = 1;
}

message AddCommandHistoryRequest {
  string command = 1;
}

// === Edict Messages ===

message CreateEdictRequest {
  string edict_id = 1;
  string session_id = 2;
  string issue_ref = 3;
  string intent = 4;
}

message EdictResponse {
  Edict edict = 1;
}

message GetEdictRequest {
  string edict_id = 1;
}

message ListEdictsRequest {
  string phase = 1;           // optional filter
  int32 limit = 2;
}

message ListEdictsResponse {
  repeated Edict edicts = 1;
}

message CancelEdictRequest {
  string edict_id = 1;
  string reason = 2;
}

// === Event Messages ===

message SubscribeEventsRequest {
  string edict_id = 1;        // optional, subscribe to specific edict
  repeated string event_types = 2;  // optional filter
}

message ShogunateEvent {
  string event_id = 1;
  string edict_id = 2;
  string event_type = 3;
  string timestamp = 4;
  map<string, string> payload = 5;
}

message EmitEventRequest {
  string edict_id = 1;
  string event_type = 2;
  map<string, string> payload = 3;
}

// === Zhengming Messages ===

message GetPendingZhengmingRequest {
  string edict_id = 1;        // optional
}

message ZhengmingListResponse {
  repeated Zhengming requests = 1;
}

message AnswerZhengmingRequest {
  string request_id = 1;
  string answer = 2;
}

// === Ritual Messages ===

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
  string state = 5;           // pending, running, completed, failed
  repeated StepState steps = 6;
}

message StepState {
  int32 index = 1;
  string name = 2;
  string status = 3;          // pending, running, done, failed
  int32 retry_count = 4;
  string message = 5;
}

// === Repository/Branch Messages ===

message ContextResponse {
  Repository repository = 1;
  Branch branch = 2;
}

message Repository {
  string host = 1;
  string org = 2;
  string project = 3;
}

message Branch {
  string name = 1;
}

message SetBranchRequest {
  string name = 1;
}

// === Health Messages ===

message HealthResponse {
  string status = 1;          // "healthy", "degraded"
  string version = 2;
  int64 uptime_seconds = 3;
  map<string, int64> db_stats = 4;  // table -> count
}
```

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

### Phase 1: API Layer Extraction (No Process Split Yet)

**Goal:** Create clean internal API that abstracts DB access, while keeping everything in one process.

1. **Define service interface** matching the gRPC service above
   - `internal/daemon/service.go` - `DaemonService` interface
   - `internal/daemon/session_service.go` - session operations
   - `internal/daemon/edict_service.go` - edict operations

2. **Create repository layer** (unify DB access)
   - Consolidate `storage.DB` and `gorm.DB` into single abstraction
   - Use GORM for all tables (migrate away from raw SQL)
   - `internal/repository/session_repo.go`
   - `internal/repository/edict_repo.go`
   - `internal/repository/history_repo.go`

3. **Refactor TUI to use service interface**
   - Replace direct `storage.DB` calls with service calls
   - Replace direct `gorm.DB` calls with service calls
   - Keep in-process for now

4. **Verify with tests**
   - All existing tests pass
   - New service layer has unit tests

### Phase 2: gRPC Service Implementation

**Goal:** Implement daemon as standalone gRPC server with WebRTC transport.

1. **Generate protobuf code**
   - `api/proto/asimi.proto`
   - `api/gen/grpc/` - generated Go code

2. **Implement gRPC server with WebRTC transport**
   - Use `go.viam.com/utils/rpc` for gRPC-over-WebRTC
   - `daemon/server.go` - server setup with WebRTC listener
   - Wire service layer to gRPC handlers
   - Support both local (Unix socket) and remote (WebRTC) clients

3. **Add daemon subcommand**
   - `asimi daemon` - start daemon process
   - `asimi daemon --webrtc` - enable WebRTC listener
   - `asimi daemon --socket /path/to/socket` - local Unix socket
   - Health check endpoint

4. **Implement daemon client**
   - `client.go` - implements `DaemonService` interface
   - WebRTC connection for remote clients
   - Unix socket for local TUI (lower latency)

### Phase 3: Process Split

**Goal:** Run TUI and daemon as separate processes.

1. **TUI connects to daemon on startup**
   - Socket path from config or env
   - Fallback to embedded mode if daemon unavailable

2. **Handle daemon lifecycle**
   - TUI can start daemon if not running (optional)
   - Signal handling for graceful shutdown
   - Reconnection logic

3. **Remove direct DB imports from TUI package**
   - `main.go` should not import `storage` directly
   - All DB access through client

### Phase 4: Cleanup & Optimization

1. **Remove `ShogunateSchema` DDL** - GORM AutoMigrate is the source of truth
2. **Remove `storage.DB` abstraction** - GORM only
3. **Add connection pooling** - gRPC client pool for TUI
4. **Add request timeout/retry** - resilience patterns
5. **Update documentation**

## File Structure After Migration

```
.
├── api/
│   ├── proto/
│   │   └── asimi.proto
│   └── gen/
│       └── grpc/
├── cmd/
│   ├── asimi/              # TUI entrypoint
│   │   └── main.go
│   └── asimi-daemon/       # Daemon entrypoint
│       └── main.go
├── daemon/
│   ├── server.go           # gRPC server (WebRTC + Unix socket)
│   ├── webrtc.go           # WebRTC configuration & auth
│   ├── service.go          # Service implementation
│   ├── session.go          # Session operations
│   ├── edict.go            # Edict operations
│   ├── history.go          # Prompt/command history
│   ├── db.go               # GORM setup
│   ├── session_repo.go
│   ├── edict_repo.go
│   ├── history_repo.go
│   └── shogunate/          # Moved from root (daemon-only)
│       ├── shogunate.go
│       ├── chancellor.go
│       ├── minister.go
│       ├── ritual.go
│       └── ...
├── chat.go                 # TUI - chat handling
├── client.go               # TUI - daemon client (gRPC)
├── commandline.go          # TUI - command line
├── completion.go           # TUI - tab completion
├── prompt.go               # TUI - prompt handling
├── status.go               # TUI - status bar
├── tui.go                  # TUI - main UI
├── ...                     # TUI - other flat files
├── web/                    # Browser client (optional)
│   ├── index.html
│   ├── main.ts
│   └── package.json
├── internal/
│   ├── config/             # Shared config
│   └── repo/               # Shared repo utilities
└── storage/
    ├── models.go           # Shared GORM structs
    └── migrations.go       # GORM AutoMigrate
```

**Principles:**
- `daemon/` = all daemon-only code (server, service, repos, shogunate)
- Root level = flat TUI code (no subdirectories)
- `internal/` = truly shared code only
- `storage/` = shared types/models
- `web/` = optional browser client for remote access

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
