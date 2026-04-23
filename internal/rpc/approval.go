package rpc

import (
	"context"
	"log/slog"
	"reflect"

	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/internal/wire"
)

// MethodRequestApproval is the daemon→TUI method name used to prompt
// the user for approval of a host-side shell command.
const MethodRequestApproval = "tui.request_approval"

// ApprovalRequestParams is the wire payload the daemon sends when it
// needs the TUI to ask the user about a command.
type ApprovalRequestParams struct {
	Command string `msgpack:"command"`
}

// ApprovalRequestResult carries the user's answer back.
type ApprovalRequestResult struct {
	Approved bool `msgpack:"approved"`
}

// ProgramSender is the minimal surface of *tea.Program the TUI-side
// approval handler needs. Declared here to avoid a bubbletea import
// from the rpc package (keeps the transport layer UI-framework-free).
type ProgramSender interface {
	Send(msg any)
}

// RegisterApprovalHandler wires a handler on the TUI's *Conn that
// converts an inbound Call("tui.request_approval") into the local
// runners.ApprovalRequestMsg tea.Msg the TUI already knows how to
// render, then blocks until the TUI writes the user's answer back on
// the message's ResponseChan.
//
// Dead-TUI safety: if ctx cancels before an answer arrives, the Call
// fails with ctx.Err() on the daemon side, so a stuck TUI never hangs
// a tool forever — combine with a timeout ctx at the call site.
func RegisterApprovalHandler(conn *Conn, program ProgramSender) {
	conn.Handle(MethodRequestApproval, func(ctx context.Context, params []byte) ([]byte, error) {
		var p ApprovalRequestParams
		if err := wire.Decode(params, &p); err != nil {
			return nil, wire.NewError(wire.CodeDecodeFailed, err.Error())
		}
		respCh := make(chan bool, 1)
		program.Send(runners.ApprovalRequestMsg{Command: p.Command, ResponseChan: respCh})
		select {
		case approved := <-respCh:
			return wire.Encode(ApprovalRequestResult{Approved: approved})
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
}

// RequestApproval is the daemon-side call: issues the RPC and returns
// the TUI's answer, honouring ctx for cancellation.
func RequestApproval(ctx context.Context, conn *Conn, command string) (bool, error) {
	raw, err := conn.Call(ctx, MethodRequestApproval, ApprovalRequestParams{Command: command})
	if err != nil {
		return false, err
	}
	var r ApprovalRequestResult
	if err := wire.Decode(raw, &r); err != nil {
		return false, err
	}
	return r.Approved, nil
}

// interceptApproval is the server-side pump helper: when a message
// flowing out of Subscribe is an ApprovalRequestMsg, it bridges the
// in-process ResponseChan to an RPC Call on conn. Returns true if the
// message was handled (and should not be forwarded as a notification).
func interceptApproval(ctx context.Context, conn *Conn, msg any) bool {
	req, ok := msg.(runners.ApprovalRequestMsg)
	if !ok {
		return false
	}
	go func() {
		approved, err := RequestApproval(ctx, conn, req.Command)
		if err != nil {
			slog.Warn("rpc: approval request failed", "command", req.Command, "err", err)
			approved = false
		}
		if req.ResponseChan != nil {
			select {
			case req.ResponseChan <- approved:
			default:
			}
		}
	}()
	return true
}

// PumpShogunateEvents consumes server-side Subscribe events, forwarding
// wire-safe notifications via conn.Notify and intercepting in-process
// side-channels like ApprovalRequestMsg.
//
// Replaces the pair of (PumpNotifications + ServeShogunateNotifications)
// for callers that need approval interception.
func PumpShogunateEvents(ctx context.Context, conn *Conn, events <-chan any) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-events:
			if !ok {
				return
			}
			if interceptApproval(ctx, conn, msg) {
				continue
			}
			method, ok := methodForType(msg)
			if !ok {
				slog.Debug("rpc: no wire method for notification", "type", reflect.TypeOf(msg))
				continue
			}
			if err := conn.Notify(method, msg); err != nil {
				slog.Debug("rpc: notify failed", "method", method, "err", err)
			}
		}
	}
}
