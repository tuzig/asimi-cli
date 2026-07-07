package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/rpc"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/internal/types"
	"github.com/afittestide/asimi/internal/wire"
	"github.com/afittestide/asimi/shogunate"
	"go.uber.org/fx"
)

// runDaemonMode starts a foreground daemon process: wires shared
// infrastructure (logger, config, DB), listens on the socket returned
// by rpc.SocketPath, and serves clients. Each connection gets its own
// Shogunate and Runner created from the shared resources.
//
// Shutdown: SIGINT or SIGTERM drains in-flight work with a 10s grace
// and tears the socket down.
func runDaemonMode() error {
	logBaseName = "asimi-daemon"
	initLogger()

	var shared *DaemonShared
	fxOptions := []fx.Option{
		// Always silence the fx logger — its PROVIDE/RUNNING output is noise.
		fx.NopLogger,
		fx.Provide(
			ProvideLogger,
			ProvideRepoInfo,
			ProvideConfig,
			ProvideStorage,
			ProvideGormDB,
			ProvideDaemonShared,
		),
		fx.Populate(&shared),
	}
	app := fx.New(fxOptions...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := app.Start(ctx); err != nil {
		return fmt.Errorf("daemon: fx start: %w", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = app.Stop(stopCtx)
	}()

	if shared == nil {
		return fmt.Errorf("daemon: shared resources not initialised")
	}

	path, err := rpc.SocketPath()
	if err != nil {
		return fmt.Errorf("daemon: resolve socket path: %w", err)
	}
	listener, err := rpc.Listen(path)
	if err != nil {
		return fmt.Errorf("daemon: listen %s: %w", path, err)
	}
	defer listener.Close()

	slog.Info("daemon listening", "socket", path)
	fmt.Fprintf(os.Stderr, "asimi daemon ready at %s\n", path)

	// Readiness signal to a parent process (TUI autostart). The parent
	// hands us a pipe fd and blocks on it until we signal that the
	// listener is bound.
	if fdStr := os.Getenv("ASIMI_READY_FD"); fdStr != "" {
		if fd, err := strconv.Atoi(fdStr); err == nil && fd > 2 {
			f := os.NewFile(uintptr(fd), "ready")
			if f != nil {
				_, _ = f.Write([]byte{1})
				_ = f.Close()
			}
		}
	}

	pidPath := path + ".pid"
	if err := writePidFile(pidPath); err != nil {
		slog.Warn("daemon: write pid file", "path", pidPath, "err", err)
	}
	defer os.Remove(pidPath)

	// Signal handling.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		slog.Info("daemon: shutdown signal received")
		cancel()
		_ = listener.Close()
	}()

	return serveClients(ctx, listener, shared)
}

// serveClients accepts connections on listener and services each one
// until ctx cancels or Accept fails. Each connection gets its own
// Shogunate created from the shared resources. connID is assigned
// atomically so every connection has a unique identifier.
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
		go func(nc net.Conn, cid uint64) {
			defer wg.Done()
			serveOne(ctx, nc, shared, cid)
		}(c, id)
	}
}

// createShogunate creates a PodmanRunner and Shogunate for a new
// daemon connection. It loads the project config, builds the runner,
// creates the Shogunate, wires the session persister (using
// config.DefaultSessionConfig for defaults), and starts it.
func createShogunate(
	ctx context.Context,
	shared *DaemonShared,
	connID uint64,
	hp types.SetContextParams,
	projectCfg *config.Config,
	repoInfo repo.RepoInfo,
) (*shogunate.Shogunate, *runners.PodmanRunner, error) {
	runner := runners.NewPodmanRunner(&projectCfg.Sandbox, repoInfo, connID, nil)

	shogCfg := config.DefaultShogunateConfig()
	if hp.Username != "" {
		shogCfg.Username = hp.Username
	}
	if hp.Project != "" {
		shogCfg.Project = hp.Project
	} else if repoInfo.Slug != "" {
		shogCfg.Project = repoInfo.Slug
	}

	shog := shogunate.NewShogunate(shared.DB, shogCfg, runner, shared.Logger)
	shog.SetRepoInfo(repoInfo)

	// Wire session persister so daemon sessions are persisted to DB,
	// same as the TUI path does via ProvideShogunate.
	if shared.Storage != nil && projectCfg.Session.Enabled {
		defaults := config.DefaultSessionConfig()
		maxSessions := defaults.MaxSessions
		maxAgeDays := defaults.MaxAgeDays
		if projectCfg.Session.MaxSessions > 0 {
			maxSessions = projectCfg.Session.MaxSessions
		}
		if projectCfg.Session.MaxAgeDays > 0 {
			maxAgeDays = projectCfg.Session.MaxAgeDays
		}
		sessionStore, err := NewSessionStore(shared.Storage, repoInfo, maxSessions, maxAgeDays)
		if err != nil {
			shared.Logger.Warn("daemon: failed to create session store", "conn_id", connID, "err", err)
		} else {
			shog.SetSessionPersister(sessionStore)
		}
	}

	if err := shog.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("shogunate start: %w", err)
	}

	return shog, runner, nil
}

// reconfigureModel reloads the project config, reinitialises Bifrost,
// and reconfigures the Shogunate model. It is called on every
// SetContext — both the first handshake and subsequent re-calls — so
// that the model always reflects the latest handshake params and
// on-disk config.
func reconfigureModel(ctx context.Context, shog *shogunate.Shogunate, hp types.SetContextParams) error {
	projectCfg, err := config.LoadProjectConfig(hp.ProjectRoot, false)
	if err != nil {
		return fmt.Errorf("load project config: %w", err)
	}

	repoInfo := repo.RepoInfo{
		ProjectRoot:  hp.ProjectRoot,
		WorktreePath: hp.WorktreePath,
		Branch:       hp.Branch,
		Slug:         hp.Project,
	}

	bifrostClient, err := shogunate.InitBifrost(
		ctx,
		projectCfg.LLM.RequestTimeoutSeconds,
		projectCfg.LLM.StreamIdleTimeoutSeconds,
		projectCfg.LLM.MaxRetries,
		projectCfg.LLM.BaseURL,
		hp.APIKeys,
	)
	if err != nil {
		return fmt.Errorf("init bifrost: %w", err)
	}

	sessionCfg := &shogunate.SessionConfig{
		LLM:        projectCfg.LLM,
		Sandbox:    projectCfg.Sandbox,
		AgentsFile: projectCfg.Session.AgentsFile,
		WorkingDir: hp.ProjectRoot,
	}

	shog.ConfigureModel(bifrostClient, sessionCfg, repoInfo)

	return nil
}

// serveOne runs the RPC server loop for a single client connection.
// It performs a handshake (SetContext), creates per-connection
// Runner and Shogunate, registers handlers, pumps server→client
// notifications, and blocks until the client disconnects or ctx
// cancels. connID uniquely identifies this connection within the
// daemon process.
//
// SetContext is idempotent: the first call creates the Shogunate and
// Runner; every call (re)configures the model from the latest params.
func serveOne(ctx context.Context, c net.Conn, shared *DaemonShared, connID uint64) {
	defer c.Close()

	conn := rpc.New(c, rpc.Options{})
	conn.Handle(rpc.MethodPing, func(ctx context.Context, _ []byte) ([]byte, error) {
		return wire.Encode(rpc.PingResult{Ok: true})
	})

	var shog *shogunate.Shogunate
	var runner *runners.PodmanRunner
	firstHandshakeCh := make(chan struct{})
	var firstHandshakeOnce sync.Once

	conn.Handle(rpc.MethodSetContext, func(_ context.Context, params []byte) ([]byte, error) {
		var p types.SetContextParams
		if err := wire.Decode(params, &p); err != nil {
			return nil, wire.NewError(wire.CodeDecodeFailed, err.Error())
		}
		if p.ProjectRoot == "" {
			return nil, wire.NewError(0, "empty project_root")
		}
		if _, err := os.Stat(p.ProjectRoot); err != nil {
			return nil, wire.NewError(0, "invalid project_root")
		}
		if shog == nil {
			projectCfg, err := config.LoadProjectConfig(p.ProjectRoot, false)
			if err != nil {
				return nil, wire.NewError(0, err.Error())
			}
			repoInfo := repo.RepoInfo{ProjectRoot: p.ProjectRoot, WorktreePath: p.WorktreePath, Branch: p.Branch, Slug: p.Project}
			shog, runner, err = createShogunate(ctx, shared, connID, p, projectCfg, repoInfo)
			if err != nil {
				return nil, wire.NewError(0, err.Error())
			}
		}
		if err := reconfigureModel(ctx, shog, p); err != nil {
			return nil, wire.NewError(0, err.Error())
		}
		firstHandshakeOnce.Do(func() { close(firstHandshakeCh) })
		return wire.Encode(struct{}{})
	})

	go func() {
		if err := conn.Serve(); err != nil {
			shared.Logger.Debug("daemon: conn.Serve exited", "conn_id", connID, "err", err)
		}
	}()

	handshakeCtx, handshakeCancel := context.WithTimeout(ctx, 30*time.Second)
	defer handshakeCancel()
	select {
	case <-firstHandshakeCh:
	case <-handshakeCtx.Done():
		shared.Logger.Warn("daemon: handshake timeout", "conn_id", connID)
		conn.Close()
		return
	}

	rpc.RegisterShogunateHandlers(conn, shog)

	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()
	go rpc.PumpShogunateEvents(connCtx, conn, shog.Subscribe(connCtx))

	defer func() {
		if runner != nil {
			closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer closeCancel()
			_ = runner.Close(closeCtx)
		}
	}()

	shared.Logger.Info("daemon: client connected", "conn_id", connID)
	<-conn.Done()
	shared.Logger.Info("daemon: client disconnected", "conn_id", connID)
}

// sendErrorAndClose sends a "daemon.error" notification carrying errMsg
// over conn, then waits 100 ms before closing the underlying connection.
// The 100 ms pause is a heuristic: Notify only enqueues the frame for
// async writing, so without a small delay the subsequent conn.Close can
// race the kernel's TCP send buffer and the client never sees the
// notification. 100 ms is long enough for the write loop to flush the
// frame in practice, but short enough that a caller waiting on the
// connection still sees a timely disconnect.
func writePidFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600)
}
