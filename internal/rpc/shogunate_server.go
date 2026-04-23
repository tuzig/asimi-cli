package rpc

import (
	"context"

	"github.com/afittestide/asimi/internal/shogunateapi"
	"github.com/afittestide/asimi/internal/wire"
)

// RegisterShogunateHandlers binds every supported Shogunate RPC method
// on c to the given client. The client is the server-side, in-process
// implementation that actually does the work.
//
// This is the initial subset; remaining methods land incrementally.
func RegisterShogunateHandlers(c *Conn, impl shogunateapi.Client) {
	c.Handle(MethodHasMinister, func(ctx context.Context, params []byte) ([]byte, error) {
		var p HasMinisterParams
		if err := wire.Decode(params, &p); err != nil {
			return nil, wire.NewError(wire.CodeDecodeFailed, err.Error())
		}
		return wire.Encode(HasMinisterResult{Has: impl.HasMinister(p.ID)})
	})

	c.Handle(MethodResetMinisterSession, func(ctx context.Context, params []byte) ([]byte, error) {
		var p ResetMinisterSessionParams
		if err := wire.Decode(params, &p); err != nil {
			return nil, wire.NewError(wire.CodeDecodeFailed, err.Error())
		}
		impl.ResetMinisterSession(p.ID)
		return nil, nil
	})

	c.Handle(MethodEdictKey, func(ctx context.Context, params []byte) ([]byte, error) {
		var p EdictKeyParams
		if err := wire.Decode(params, &p); err != nil {
			return nil, wire.NewError(wire.CodeDecodeFailed, err.Error())
		}
		return wire.Encode(EdictKeyResult{Key: impl.EdictKey(p.EdictID)})
	})

	c.Handle(MethodCourtEdictKey, func(ctx context.Context, _ []byte) ([]byte, error) {
		return wire.Encode(CourtEdictKeyResult{Key: impl.CourtEdictKey()})
	})

	c.Handle(MethodCreateEdict, func(ctx context.Context, params []byte) ([]byte, error) {
		var p CreateEdictParams
		if err := wire.Decode(params, &p); err != nil {
			return nil, wire.NewError(wire.CodeDecodeFailed, err.Error())
		}
		edict, err := impl.CreateEdict(p.IssueRef, p.Intent)
		if err != nil {
			return nil, err
		}
		return wire.Encode(CreateEdictResult{Edict: edict})
	})

	c.Handle(MethodCreateEdictSilent, func(ctx context.Context, params []byte) ([]byte, error) {
		var p CreateEdictParams
		if err := wire.Decode(params, &p); err != nil {
			return nil, wire.NewError(wire.CodeDecodeFailed, err.Error())
		}
		edict, err := impl.CreateEdictSilent(p.IssueRef, p.Intent)
		if err != nil {
			return nil, err
		}
		return wire.Encode(CreateEdictResult{Edict: edict})
	})

	c.Handle(MethodGetEdict, func(ctx context.Context, params []byte) ([]byte, error) {
		var p GetEdictParams
		if err := wire.Decode(params, &p); err != nil {
			return nil, wire.NewError(wire.CodeDecodeFailed, err.Error())
		}
		edict, err := impl.GetEdict(p.EdictID)
		if err != nil {
			return nil, err
		}
		return wire.Encode(GetEdictResult{Edict: edict})
	})

	c.Handle(MethodGrantRulerSeal, func(ctx context.Context, params []byte) ([]byte, error) {
		var p GrantRulerSealParams
		if err := wire.Decode(params, &p); err != nil {
			return nil, wire.NewError(wire.CodeDecodeFailed, err.Error())
		}
		if err := impl.GrantRulerSeal(p.EdictID, p.Notes); err != nil {
			return nil, err
		}
		return nil, nil
	})

	c.Handle(MethodListActiveEdicts, func(ctx context.Context, _ []byte) ([]byte, error) {
		edicts, err := impl.ListActiveEdicts()
		if err != nil {
			return nil, err
		}
		return wire.Encode(ListActiveEdictsResult{Edicts: edicts})
	})

	c.Handle(MethodCancelZhengming, func(ctx context.Context, params []byte) ([]byte, error) {
		var p CancelZhengmingParams
		if err := wire.Decode(params, &p); err != nil {
			return nil, wire.NewError(wire.CodeDecodeFailed, err.Error())
		}
		impl.CancelZhengming(p.RequestID)
		return nil, nil
	})

	c.Handle(MethodHandleZhengming, func(ctx context.Context, params []byte) ([]byte, error) {
		var p HandleZhengmingParams
		if err := wire.Decode(params, &p); err != nil {
			return nil, wire.NewError(wire.CodeDecodeFailed, err.Error())
		}
		if err := impl.HandleZhengmingResponse(ctx, p.RequestID, p.Answer); err != nil {
			return nil, err
		}
		return nil, nil
	})

	c.Handle(MethodAllowRunnerFallback, func(ctx context.Context, params []byte) ([]byte, error) {
		var p AllowRunnerFallbackParams
		if err := wire.Decode(params, &p); err != nil {
			return nil, wire.NewError(wire.CodeDecodeFailed, err.Error())
		}
		impl.AllowRunnerFallback(p.Allow)
		return nil, nil
	})

	c.Handle(MethodClearSessionHistory, func(ctx context.Context, params []byte) ([]byte, error) {
		var p ClearSessionHistoryParams
		if err := wire.Decode(params, &p); err != nil {
			return nil, wire.NewError(wire.CodeDecodeFailed, err.Error())
		}
		if err := impl.ClearSessionHistory(p.TabTarget); err != nil {
			return nil, err
		}
		return nil, nil
	})

	c.Handle(MethodRollbackSession, func(ctx context.Context, params []byte) ([]byte, error) {
		var p RollbackSessionParams
		if err := wire.Decode(params, &p); err != nil {
			return nil, wire.NewError(wire.CodeDecodeFailed, err.Error())
		}
		if err := impl.RollbackSession(p.TabTarget, p.Snapshot); err != nil {
			return nil, err
		}
		return nil, nil
	})

	c.Handle(MethodAddSessionMessage, func(ctx context.Context, params []byte) ([]byte, error) {
		var p AddSessionMessageParams
		if err := wire.Decode(params, &p); err != nil {
			return nil, wire.NewError(wire.CodeDecodeFailed, err.Error())
		}
		if err := impl.AddSessionMessage(p.TabTarget, p.Role, p.Content); err != nil {
			return nil, err
		}
		return nil, nil
	})
}
