# Multi-Client Daemon Support

## Context

The daemon currently shares a single `Shogunate` and `Runner` across all client connections. When multiple clients connect:

1. **Notification bug**: `SetNotify()` overwrites callback - only last client receives notifications
2. **State races**: `ministers` and `tabCancels` maps shared without locks
3. **Project mismatch**: `ShogunateConfig.Project` set once at daemon startup, can't serve different projects

Use case: User wants to work on two projects simultaneously with two TUI clients.

## Approach

**Per-connection isolation**: Each client gets its own `Runner` (container) and `Shogunate` (state). Shared infrastructure: database, logger, runner config.

**Handshake protocol**: Client must call `SetContext` before any other RPC method. Daemon validates project path, creates isolated resources, then serves requests.

## Architecture

```
Daemon (shared infrastructure)
  ├── db (*gorm.DB)
  ├── logger (*slog.Logger)
  └── runnerConfig (*runners.Config)

Connection 1                    Connection 2
  ├── Runner (isolated)           ├── Runner (isolated)
  └── Shogunate (isolated)        └── Shogunate (isolated)
```

## Changes

### 1. `internal/rpc/shogunate_methods.go` — Add method constant

```go
const MethodSetContext = "SetContext"
```

### 2. `internal/rpc/shogunate_types.go` — Add wire types

```go
type SetContextParams struct {
    Project      string `msgpack:"project"`
    Username     string `msgpack:"username"`
    ProjectRoot  string `msgpack:"project_root"`
    WorktreePath string `msgpack:"worktree_path"`
    Branch       string `msgpack:"branch"`
}

type SetContextResult struct{}
```

### 3. `internal/rpc/shogunate_client.go` — Add client method

```go
func (c *ShogunateClient) SetContext(ctx context.Context, params SetContextParams) error {
    _, err := c.conn.Call(ctx, MethodSetContext, params)
    return err
}
```

### 4. `internal/runners/podman.go` — Container naming per connection

Change `NewPodmanRunner` signature to accept `connID`:

```go
func NewPodmanRunner(cfg *Config, repoInfo repo.RepoInfo, connID uint64, fallback Runner) *PodmanRunner {
    imageName := cfg.ImageName
    if imageName == "" {
        imageName = fmt.Sprintf("localhost/asimi-sandbox-%s:latest", repoInfo.Slug)
    }

    containerName := fmt.Sprintf("asimi-shell-%s-%d", repoInfo.Slug, connID)

    return &PodmanRunner{
        imageName:     imageName,
        containerName: containerName,
        allowFallback: cfg.AllowHostFallback,
        noCleanup:     cfg.NoCleanup,
        config:        cfg,
        repoInfo:      repoInfo,
        fallback:      fallback,
        outputs:       make(map[int]*commandOutput),
        nextCommandID: 1,
    }
}
```

Also update `host.go` signature to match (even if unused).

### 5. `daemon.go` — Main daemon changes

Add shared resources struct:

```go
type DaemonShared struct {
    DB           *gorm.DB
    Logger       *slog.Logger
    RunnerConfig *runners.Config
}

func ProvideDaemonShared(db *gorm.DB, cfg *Config, logger *slog.Logger) *DaemonShared {
    return &DaemonShared{
        DB:           db,
        Logger:       logger,
        RunnerConfig: &cfg.Sandbox,
    }
}
```

Modify `runDaemonMode` fx options - remove per-connection providers, add shared:

```go
var shared *DaemonShared
fxOptions := []fx.Option{
    fx.Provide(
        ProvideLogger,
        ProvideConfig,
        ProvideStorage,
        ProvideGormDB,
        ProvideDaemonShared,
    ),
    fx.Populate(&shared),
}
```

Modify `serveClients` to track connection IDs:

```go
func serveClients(ctx context.Context, listener net.Listener, shared *DaemonShared) error {
    var connID atomic.Uint64
    var wg sync.WaitGroup
    defer wg.Wait()

    for {
        if err := ctx.Err(); err != nil {
            return nil
        }
        c, err := listener.Accept()
        if err != nil {
            if ctx.Err() != nil {
                return nil
            }
            return fmt.Errorf("daemon: accept: %w", err)
        }

        id := connID.Add(1)
        wg.Add(1)
        go func(nc net.Conn, connID uint64) {
            defer wg.Done()
            serveOne(ctx, nc, shared, connID)
        }(c, id)
    }
}
```

Add `serveOne` with handshake and per-connection resources:

```go
func serveOne(ctx context.Context, c net.Conn, shared *DaemonShared, connID uint64) {
    defer c.Close()

    conn := rpc.New(c, rpc.Options{})

    // Phase 1: Handshake - wait for SetContext with timeout
    handshakeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
    defer cancel()

    projectCtx, err := waitForHandshake(handshakeCtx, conn)
    if err != nil {
        sendErrorAndClose(conn, err)
        return
    }

    // Validate project_root exists
    if _, err := os.Stat(projectCtx.ProjectRoot); os.IsNotExist(err) {
        sendErrorAndClose(conn, fmt.Errorf("project_root does not exist: %s", projectCtx.ProjectRoot))
        return
    }

    // Phase 2: Create per-connection resources
    repoInfo := repo.RepoInfo{
        ProjectRoot:  projectCtx.ProjectRoot,
        WorktreePath: projectCtx.WorktreePath,
        Branch:       projectCtx.Branch,
        Slug:         projectCtx.Project,
    }

    runner := runners.NewPodmanRunner(shared.RunnerConfig, repoInfo, connID, nil)
    defer func() {
        closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        _ = runner.Close(closeCtx)
    }()

    cfg := config.DefaultShogunateConfig()
    cfg.Project = projectCtx.Project
    cfg.Username = projectCtx.Username

    shog := shogunate.NewShogunate(shared.DB, cfg, runner, shared.Logger)

    // Phase 3: Register handlers and serve
    rpc.RegisterShogunateHandlers(conn, shog)
    go rpc.PumpShogunateEvents(ctx, conn, shog.Subscribe(ctx))

    shared.Logger.Info("daemon: client connected", "project", projectCtx.Project, "conn_id", connID)
    _ = conn.Serve()
    shared.Logger.Info("daemon: client disconnected", "project", projectCtx.Project, "conn_id", connID)
}

func waitForHandshake(ctx context.Context, conn *rpc.Conn) (*SetContextParams, error) {
    resultChan := make(chan *SetContextParams, 1)
    errChan := make(chan error, 1)

    conn.Handle(rpc.MethodSetContext, func(ctx context.Context, params []byte) ([]byte, error) {
        var p rpc.SetContextParams
        if err := wire.Decode(params, &p); err != nil {
            errChan <- err
            return nil, wire.NewError(wire.CodeDecodeFailed, err.Error())
        }
        resultChan <- &p
        return wire.Encode(rpc.SetContextResult{})
    })

    go conn.Serve()

    select {
    case <-ctx.Done():
        return nil, fmt.Errorf("handshake timeout: %w", ctx.Err())
    case err := <-errChan:
        return nil, fmt.Errorf("handshake decode failed: %w", err)
    case p := <-resultChan:
        return p, nil
    }
}

func sendErrorAndClose(conn *rpc.Conn, err error) {
    _ = conn.Notify("daemon.error", map[string]string{"error": err.Error()})
    time.Sleep(100 * time.Millisecond)
    conn.Close()
}
```

### 6. `providers.go` — Update daemon providers

Remove from daemon fx chain (per-connection now):
- `ProvideRepoInfo`
- `ProvideShellRunner`
- `ProvideShogunate`
- `ProvidePromptHistory`
- `ProvideCommandHistory`
- `ProvideSessionHistory`
- `ProvideScheduler`

Keep:
- `ProvideLogger`
- `ProvideConfig`
- `ProvideStorage`
- `ProvideGormDB`

Add:
- `ProvideDaemonShared`

### 7. `loopback.go` — Client handshake on connect

```go
func connectToDaemon(ctx context.Context) (*rpc.ShogunateClient, error) {
    path, err := rpc.SocketPath()
    if err != nil {
        return nil, err
    }

    c, err := net.Dial("unix", path)
    if err != nil {
        return nil, err
    }

    conn := rpc.New(c, rpc.Options{})
    client := rpc.NewShogunateClient(conn)

    repoInfo := repo.GetRepoInfo()
    username := "guest"
    if u, err := user.Current(); err == nil {
        username = u.Username
    }

    params := rpc.SetContextParams{
        Project:      repoInfo.Slug,
        Username:     username,
        ProjectRoot:  repoInfo.ProjectRoot,
        WorktreePath: repoInfo.WorktreePath,
        Branch:       repoInfo.Branch,
    }

    if err := client.SetContext(ctx, params); err != nil {
        c.Close()
        return nil, fmt.Errorf("handshake failed: %w", err)
    }

    return client, nil
}
```

## Connection Flow

```
Client                          Daemon
------                          ------
  |                               |
  |------ connect -------------->|
  |                               |
  |------ SetContext ------------>|  (required first, 30s timeout)
  |                               |-- validate project_root
  |                               |-- create Runner (connID)
  |                               |-- create Shogunate
  |                               |-- register handlers
  |<----- ACK --------------------|
  |                               |
  |------ SubmitPrompt --------->|  (uses connection's Shogunate)
  |                               |
```

## Verification

```bash
just test          # All tests pass
just lint          # No lint errors
just build         # Builds cleanly
```

## Testing Scenarios

1. **Single client**: Existing behavior preserved
2. **Two clients, different projects**: Each gets own container + Shogunate
3. **Two clients, same project**: Each gets own container + Shogunate (isolated)
4. **Handshake timeout**: Client that doesn't send SetContext within 30s gets disconnected with error
5. **Invalid project_root**: Client gets error notification, connection closed
6. **Container cleanup**: Containers cleaned up on disconnect via `runner.Close()`
