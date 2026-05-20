package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/afittestide/asimi/internal/rpc"
	tea "github.com/charmbracelet/bubbletea"
)

// installRPCLoopback wires the TUI model to a LoopbackShogunate that
// talks to the real shogunate through an in-process net.Pipe carrying
// the MessagePack RPC. Wire-safe calls and every notification travel
// through the codec; the three still-in-process methods (GetMinister,
// ConfigureModel) delegate to the real shogunate inline.
//
// Approval requests become a daemon→TUI Call — the returned hook is
// called once the *tea.Program exists so the approval handler can
// forward inbound requests as tea.Msg into the program event loop.
//
// Opt-in via ASIMI_LOOPBACK=1. Off by default so production paths are
// undisturbed until the loopback has proven itself in the field.
func installRPCLoopback(ctx context.Context, model *TUIModel) (func(*tea.Program), error) {
	if model == nil || model.shogunate == nil {
		return nil, fmt.Errorf("installRPCLoopback: tui model or shogunate is nil")
	}

	real := model.shogunate

	pa, pb := net.Pipe()
	server := rpc.New(pa, rpc.Options{})
	client := rpc.New(pb, rpc.Options{})

	rpc.RegisterShogunateHandlers(server, real)

	go func() {
		if err := server.Serve(); err != nil {
			slog.Debug("loopback: server.Serve error", "err", err)
		}
	}()
	go func() {
		if err := client.Serve(); err != nil {
			slog.Debug("loopback: client.Serve error", "err", err)
		}
	}()

	// Pump server-side Subscribe events: intercept approval requests,
	// forward everything else as wire notifications.
	go rpc.PumpShogunateEvents(ctx, server, real.Subscribe(ctx))

	model.shogunate = rpc.NewLoopbackShogunate(client, real)

	// Defer approval-handler registration until we have a *tea.Program
	// to send tea.Msgs into.
	return func(p *tea.Program) {
		rpc.RegisterApprovalHandler(client, teaSender{p})
		rpc.RegisterEditorHandler(client, teaSender{p})
	}, nil
}

// teaSender adapts *tea.Program to rpc.ProgramSender. Keeps the rpc
// package free of bubbletea imports.
type teaSender struct{ *tea.Program }

func (s teaSender) Send(msg any) { s.Program.Send(msg) }

// installDaemonSocket dials a running asimi daemon at socketPath and
// swaps the TUI's shogunate for a ReconnectingClient wrapping the RPC
// connection. Wire-safe methods and all notifications flow over the real
// socket; GetMinister, ConfigureModel still delegate to
// the TUI's local shogunate (a known limitation — any feature that
// relies on those methods will see local-only state instead of the
// daemon's).
//
// Opt-in via ASIMI_DAEMON_SOCKET=/path/to/asimi.sock.
func installDaemonSocket(ctx context.Context, model *TUIModel, socketPath string) (func(*tea.Program), error) {
	if model == nil || model.shogunate == nil {
		return nil, fmt.Errorf("installDaemonSocket: tui model or shogunate is nil")
	}

	factory := func() (*rpc.Conn, error) {
		c, err := rpc.Dial(socketPath)
		if err != nil {
			return nil, err
		}
		conn := rpc.New(c, rpc.Options{})
		go func() {
			if err := conn.Serve(); err != nil {
				slog.Debug("daemon rpc client terminated", "err", err)
			}
		}()
		return conn, nil
	}

	rc := rpc.NewReconnectingClient(factory, model.shogunate)
	if err := rc.Start(); err != nil {
		return nil, fmt.Errorf("installDaemonSocket: start: %w", err)
	}

	model.shogunate = rc

	return func(p *tea.Program) {
		rc.RegisterHandler(func(conn *rpc.Conn) {
			rpc.RegisterApprovalHandler(conn, teaSender{p})
			rpc.RegisterEditorHandler(conn, teaSender{p})
		})
	}, nil
}
