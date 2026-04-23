package main

import (
	"context"
	"fmt"
	"net"

	"github.com/afittestide/asimi/internal/rpc"
)

// installRPCLoopback wires the TUI model to a LoopbackShogunate that
// talks to the real shogunate through an in-process net.Pipe carrying
// the MessagePack RPC. Wire-safe calls and every notification travel
// through the codec; the three still-in-process methods (GetMinister,
// ConfigureModel, SetRulingCtx) delegate to the real shogunate inline.
//
// Opt-in via ASIMI_LOOPBACK=1. Off by default so production paths are
// undisturbed until the loopback has proven itself in the field.
func installRPCLoopback(ctx context.Context, model *TUIModel) error {
	if model == nil || model.shogunate == nil {
		return fmt.Errorf("installRPCLoopback: tui model or shogunate is nil")
	}

	real := model.shogunate

	pa, pb := net.Pipe()
	server := rpc.New(pa, rpc.Options{})
	client := rpc.New(pb, rpc.Options{})

	rpc.RegisterShogunateHandlers(server, real)

	go func() { _ = server.Serve() }()
	go func() { _ = client.Serve() }()

	// Pump server→client notifications for the session's lifetime.
	go rpc.ServeShogunateNotifications(ctx, server, real)

	model.shogunate = rpc.NewLoopbackShogunate(client, real)
	return nil
}
