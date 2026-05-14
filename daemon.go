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
	"syscall"
	"time"

	"github.com/afittestide/asimi/internal/rpc"
	"github.com/afittestide/asimi/internal/shogunateapi"
	"github.com/afittestide/asimi/shogunate"
	"go.uber.org/fx"
)

// runDaemonMode starts a foreground daemon process: wires the same
// providers the interactive TUI would use (minus the TUI itself),
// listens on the socket returned by rpc.SocketPath, and serves one
// client at a time.
//
// Shutdown: SIGINT or SIGTERM drains in-flight work with a 10s grace
// and tears the socket down.
func runDaemonMode() error {
	logBaseName = "asimi-daemon"
	initLogger()

	var shog *shogunate.Shogunate
	fxOptions := []fx.Option{
		fx.Provide(
			ProvideLogger,
			ProvideConfig,
			ProvideStorage,
			ProvideGormDB,
			ProvideRepoInfo,
			ProvideScheduler,
			ProvideShellRunner,
			ProvidePromptHistory,
			ProvideCommandHistory,
			ProvideSessionHistory,
			ProvideShogunate,
		),
		fx.Populate(&shog),
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

	if shog == nil {
		return fmt.Errorf("daemon: shogunate not initialised")
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

	return serveClients(ctx, listener, shog)
}

// serveClients accepts connections on listener and services each one
// until ctx cancels or Accept fails. Clients are serial for v1 — one
// TUI at a time per scope. Concurrent clients are future work.
func serveClients(ctx context.Context, listener net.Listener, shog shogunateapi.Client) error {
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
		wg.Add(1)
		go func(nc net.Conn) {
			defer wg.Done()
			serveOne(ctx, nc, shog)
		}(c)
	}
}

// serveOne runs the RPC server loop for a single client connection:
// registers handlers, pumps server→client notifications, blocks until
// the client disconnects or ctx cancels.
func serveOne(ctx context.Context, c net.Conn, shog shogunateapi.Client) {
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	conn := rpc.New(c, rpc.Options{})
	rpc.RegisterShogunateHandlers(conn, shog)

	go rpc.PumpShogunateEvents(connCtx, conn, shog.Subscribe(connCtx))

	// Close the underlying net.Conn when ctx cancels so Serve() wakes
	// from its read loop.
	done := make(chan struct{})
	go func() {
		select {
		case <-connCtx.Done():
			_ = c.Close()
		case <-done:
		}
	}()

	slog.Info("daemon: client connected")
	_ = conn.Serve()
	close(done)
	slog.Info("daemon: client disconnected")
}

func writePidFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0o600)
}
