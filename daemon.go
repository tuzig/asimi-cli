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
		fx.Provide(
			ProvideLogger,
			ProvideConfig,
			ProvideStorage,
			ProvideGormDB,
			ProvideDaemonShared,
		),
		fx.Populate(&shared),
	}
	if !cli.Debug {
		fxOptions = append(fxOptions, fx.NopLogger)
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

// serveOne runs the RPC server loop for a single client connection.
// It performs a handshake (SetContext), then creates per-connection
// Runner and Shogunate, registers handlers, pumps server→client
// notifications, and blocks until the client disconnects or ctx
// cancels. connID uniquely identifies this connection within the
// daemon process.
func serveOne(ctx context.Context, c net.Conn, shared *DaemonShared, connID uint64) {
	defer c.Close()

	conn := rpc.New(c, rpc.Options{})

	// Register liveness probe before Serve starts so autostart can
	// verify the daemon is responsive before handshake completes.
	conn.Handle(rpc.MethodPing, func(ctx context.Context, _ []byte) ([]byte, error) {
		return wire.Encode(rpc.PingResult{Ok: true})
	})

	handshakeCh := make(chan rpc.SetContextParams, 1)
	handshakeRespCh := make(chan error, 1)
	conn.Handle(rpc.MethodSetContext, func(_ context.Context, params []byte) ([]byte, error) {
		var p rpc.SetContextParams
		if err := wire.Decode(params, &p); err != nil {
			return nil, wire.NewError(wire.CodeDecodeFailed, err.Error())
		}
		select {
		case handshakeCh <- p:
		default:
		}
		if err := <-handshakeRespCh; err != nil {
			return nil, err
		}
		return wire.Encode(rpc.SetContextResult{})
	})

	// (3) Start Serve() in a goroutine EXACTLY ONCE.
	go func() {
		if err := conn.Serve(); err != nil {
			shared.Logger.Debug("daemon: conn.Serve exited", "conn_id", connID, "err", err)
		}
	}()

	// (4) Wait for handshake with a 30s timeout.
	handshakeCtx, handshakeCancel := context.WithTimeout(ctx, 30*time.Second)
	defer handshakeCancel()

	var hp rpc.SetContextParams
	select {
	case hp = <-handshakeCh:
		// Handshake received.
	case <-handshakeCtx.Done():
		shared.Logger.Warn("daemon: handshake timeout", "conn_id", connID)
		conn.Close()
		return
	}

	// (5) Validate project_root exists via os.Stat.
	if hp.ProjectRoot == "" {
		shared.Logger.Warn("daemon: empty project_root", "conn_id", connID)
		handshakeRespCh <- wire.NewError(0, "empty project_root")
		conn.Close()
		return
	}
	if _, err := os.Stat(hp.ProjectRoot); err != nil {
		shared.Logger.Warn("daemon: invalid project_root", "conn_id", connID, "path", hp.ProjectRoot, "err", err)
		handshakeRespCh <- wire.NewError(0, "invalid project_root")
		conn.Close()
		return
	}

	// (6) Create per-connection resources from handshake params.
	repoInfo := repo.RepoInfo{
		ProjectRoot:  hp.ProjectRoot,
		WorktreePath: hp.WorktreePath,
		Branch:       hp.Branch,
		Slug:         hp.Project,
	}

	runner := runners.NewPodmanRunner(&shared.Config.Sandbox, repoInfo, connID, nil)
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = runner.Close(closeCtx)
	}()

	cfg := config.DefaultShogunateConfig()
	// TODO: Remove Username as it should come from the unix socket so
	// users can't fake it
	if hp.Username != "" {
		cfg.Username = hp.Username
	}
	if hp.Project != "" {
		cfg.Project = hp.Project
	} else if repoInfo.Slug != "" {
		cfg.Project = repoInfo.Slug
	}

	shog := shogunate.NewShogunate(shared.DB, cfg, runner, shared.Logger)
	if err := shog.Start(ctx); err != nil {
		shared.Logger.Error("daemon: shogunate start failed", "conn_id", connID, "err", err)
		handshakeRespCh <- wire.NewError(0, "shogunate start failed")
		conn.Close()
		return
	}

	rpc.RegisterShogunateHandlers(conn, shog)

	select {
	case handshakeRespCh <- nil:
	default:
	}

	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()

	go rpc.PumpShogunateEvents(connCtx, conn, shog.Subscribe(connCtx))

	shared.Logger.Info("daemon: client connected",
		"conn_id", connID, "project", hp.Project, "project_root", hp.ProjectRoot)

	// (7) Block until the connection is done.
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
func sendErrorAndClose(conn *rpc.Conn, errMsg string) {
	_ = conn.Notify("daemon.error", map[string]string{"error": errMsg})
	time.Sleep(100 * time.Millisecond)
	conn.Close()
}

func writePidFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600)
}
