package rpc

import (
	"context"
	"log/slog"
	"reflect"
	"sync"

	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/internal/wire"
	"github.com/afittestide/asimi/shogunate"
)

// Notification method names (server → client). Keep in sync with the
// typeToMethod registry below.
const (
	NotifyStreamStart         = "stream.start"
	NotifyStreamChunk         = "stream.chunk"
	NotifyStreamReasoning     = "stream.reasoning"
	NotifyStreamComplete      = "stream.complete"
	NotifyStreamDone          = "stream.done"
	NotifyStreamInterrupted   = "stream.interrupted"
	NotifyStreamMaxTokens     = "stream.max_tokens"
	NotifyStreamError         = "stream.error"
	NotifyEventsDrained       = "events.drained"
	NotifyMinisterInvoking    = "minister.invoking"
	NotifyMinisterCompleted   = "minister.completed"
	NotifyEvent               = "event"
	NotifyZhengmingPending    = "zhengming.pending"
	NotifyZhengmingAnswered   = "zhengming.answered"
	NotifyRitualStep          = "ritual.step"
	NotifyContainerLaunched   = "runner.container_launched"
)

// typeToMethod maps a Go notification type to its wire method name.
// Used by the server-side dispatcher to route messages coming out of
// shogunate.Subscribe. Extendable: call RegisterNotificationType.
var (
	typeToMethodMu sync.RWMutex
	typeToMethod   = map[reflect.Type]string{
		reflect.TypeOf(shogunate.StreamStartMsg{}):            NotifyStreamStart,
		reflect.TypeOf(shogunate.StreamChunkMsg{}):            NotifyStreamChunk,
		reflect.TypeOf(shogunate.StreamReasoningChunkMsg{}):   NotifyStreamReasoning,
		reflect.TypeOf(shogunate.StreamCompleteMsg{}):         NotifyStreamComplete,
		reflect.TypeOf(shogunate.StreamDoneMsg{}):             NotifyStreamDone,
		reflect.TypeOf(shogunate.StreamInterruptedMsg{}):      NotifyStreamInterrupted,
		reflect.TypeOf(shogunate.StreamMaxTokensReachedMsg{}): NotifyStreamMaxTokens,
		reflect.TypeOf(shogunate.StreamErrorMsg{}):            NotifyStreamError,
		reflect.TypeOf(shogunate.EventsDrainedMsg{}):          NotifyEventsDrained,
		reflect.TypeOf(shogunate.MinisterInvokingMsg{}):       NotifyMinisterInvoking,
		reflect.TypeOf(shogunate.MinisterCompletedMsg{}):      NotifyMinisterCompleted,
		reflect.TypeOf(shogunate.EventNotificationMsg{}):      NotifyEvent,
		reflect.TypeOf(shogunate.ZhengmingPendingMsg{}):       NotifyZhengmingPending,
		reflect.TypeOf(shogunate.ZhengmingAnsweredMsg{}):      NotifyZhengmingAnswered,
		reflect.TypeOf(shogunate.RitualStepMsg{}):             NotifyRitualStep,
		reflect.TypeOf(runners.ContainerLaunchedMsg{}):        NotifyContainerLaunched,
	}
)

// RegisterNotificationType associates a Go type with a wire method name.
// Idempotent; later registrations win.
func RegisterNotificationType(msg any, method string) {
	typeToMethodMu.Lock()
	defer typeToMethodMu.Unlock()
	typeToMethod[reflect.TypeOf(msg)] = method
}

// methodForType looks up the wire method for a runtime value.
func methodForType(v any) (string, bool) {
	typeToMethodMu.RLock()
	defer typeToMethodMu.RUnlock()
	m, ok := typeToMethod[reflect.TypeOf(v)]
	return m, ok
}

// PumpNotifications subscribes to the given Client and forwards every
// message to conn as a wire notification keyed by method name. Unknown
// message types are dropped with a debug log. Runs until ctx is done.
func PumpNotifications(ctx context.Context, conn *Conn, events <-chan any) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-events:
			if !ok {
				return
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

// NotificationDecoder decodes raw notification params into a concrete
// Go value. The client-side registry below wires one per known method.
type NotificationDecoder func([]byte) (any, error)

var (
	methodToDecoderMu sync.RWMutex
	methodToDecoder   = map[string]NotificationDecoder{
		NotifyStreamStart:       decode[shogunate.StreamStartMsg],
		NotifyStreamChunk:       decode[shogunate.StreamChunkMsg],
		NotifyStreamReasoning:   decode[shogunate.StreamReasoningChunkMsg],
		NotifyStreamComplete:    decode[shogunate.StreamCompleteMsg],
		NotifyStreamDone:        decode[shogunate.StreamDoneMsg],
		NotifyStreamInterrupted: decode[shogunate.StreamInterruptedMsg],
		NotifyStreamMaxTokens:   decode[shogunate.StreamMaxTokensReachedMsg],
		NotifyStreamError:       decode[shogunate.StreamErrorMsg],
		NotifyEventsDrained:     decode[shogunate.EventsDrainedMsg],
		NotifyMinisterInvoking:  decode[shogunate.MinisterInvokingMsg],
		NotifyMinisterCompleted: decode[shogunate.MinisterCompletedMsg],
		NotifyEvent:             decode[shogunate.EventNotificationMsg],
		NotifyZhengmingPending:  decode[shogunate.ZhengmingPendingMsg],
		NotifyZhengmingAnswered: decode[shogunate.ZhengmingAnsweredMsg],
		NotifyRitualStep:        decode[shogunate.RitualStepMsg],
		NotifyContainerLaunched: decode[runners.ContainerLaunchedMsg],
	}
)

// RegisterNotificationDecoder associates a wire method with a decoder.
// Mirrors RegisterNotificationType for client-side extensibility.
func RegisterNotificationDecoder(method string, d NotificationDecoder) {
	methodToDecoderMu.Lock()
	defer methodToDecoderMu.Unlock()
	methodToDecoder[method] = d
}

// decode is a generic helper that builds a NotificationDecoder for type T.
func decode[T any](params []byte) (any, error) {
	var v T
	if err := wire.Decode(params, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// SubscribeAll registers a NotifyHandler on conn for every known
// notification method. Decoded values flow into out. The returned
// function unregisters handlers (best-effort: the Conn's handler map is
// not currently concurrent-safe for deletion, so unregister is a no-op
// today — included for API symmetry).
//
// Callers drain out until ctx is done.
func SubscribeAll(conn *Conn, out chan<- any) {
	methodToDecoderMu.RLock()
	defer methodToDecoderMu.RUnlock()
	for method, dec := range methodToDecoder {
		method, dec := method, dec // capture
		conn.HandleNotify(method, func(ctx context.Context, params []byte) {
			v, err := dec(params)
			if err != nil {
				slog.Debug("rpc: decode notification", "method", method, "err", err)
				return
			}
			select {
			case out <- v:
			case <-ctx.Done():
			}
		})
	}
}
