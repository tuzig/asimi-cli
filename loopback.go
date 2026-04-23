package main

import (
	"context"
	"fmt"
	"net"

	"github.com/afittestide/asimi/internal/rpc"
	tea "github.com/charmbracelet/bubbletea"
)

// installRPCLoopback wires the TUI model to a LoopbackShogunate that
// talks to the real shogunate through an in-process net.Pipe carrying
// the MessagePack RPC. Wire-safe calls and every notification travel
// through the codec; the three still-in-process methods (GetMinister,
// ConfigureModel, SetRulingCtx) delegate to the real shogunate inline.
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

	go func() { _ = server.Serve() }()
	go func() { _ = client.Serve() }()

	// Pump server-side Subscribe events: intercept approval requests,
	// forward everything else as wire notifications.
	go rpc.PumpShogunateEvents(ctx, server, real.Subscribe(ctx))

	model.shogunate = rpc.NewLoopbackShogunate(client, real)

	// Defer approval-handler registration until we have a *tea.Program
	// to send tea.Msgs into.
	return func(p *tea.Program) {
		rpc.RegisterApprovalHandler(client, teaSender{p})
	}, nil
}

// teaSender adapts *tea.Program to rpc.ProgramSender. Keeps the rpc
// package free of bubbletea imports.
type teaSender struct{ *tea.Program }

func (s teaSender) Send(msg any) { s.Program.Send(msg) }
