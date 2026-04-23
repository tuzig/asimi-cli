package rpc

import (
	"context"
	"fmt"

	"github.com/afittestide/asimi/internal/wire"
	"github.com/afittestide/asimi/storage"
)

// ShogunateClient exposes the RPC surface as a typed API. It partially
// satisfies shogunateapi.Client — methods with wire-safe signatures are
// implemented here; the rest (GetMinister, ConfigureModel, Subscribe,
// SetRulingCtx, TakeSnapshot) need either in-process passthrough or a
// dedicated notification path and land in a follow-up.
type ShogunateClient struct {
	conn *Conn
}

// NewShogunateClient wraps a live Conn as a typed client.
func NewShogunateClient(conn *Conn) *ShogunateClient {
	return &ShogunateClient{conn: conn}
}

func (c *ShogunateClient) callVoid(ctx context.Context, method string, params any) error {
	_, err := c.conn.Call(ctx, method, params)
	return err
}

func (c *ShogunateClient) HasMinister(id string) bool {
	raw, err := c.conn.Call(context.Background(), MethodHasMinister, HasMinisterParams{ID: id})
	if err != nil {
		return false
	}
	var r HasMinisterResult
	_ = wire.Decode(raw, &r)
	return r.Has
}

func (c *ShogunateClient) ResetMinisterSession(id string) {
	_ = c.callVoid(context.Background(), MethodResetMinisterSession, ResetMinisterSessionParams{ID: id})
}

func (c *ShogunateClient) EdictKey(edictID uint) storage.EdictKey {
	raw, err := c.conn.Call(context.Background(), MethodEdictKey, EdictKeyParams{EdictID: edictID})
	if err != nil {
		return storage.EdictKey{}
	}
	var r EdictKeyResult
	_ = wire.Decode(raw, &r)
	return r.Key
}

func (c *ShogunateClient) CourtEdictKey() storage.EdictKey {
	raw, err := c.conn.Call(context.Background(), MethodCourtEdictKey, nil)
	if err != nil {
		return storage.EdictKey{}
	}
	var r CourtEdictKeyResult
	_ = wire.Decode(raw, &r)
	return r.Key
}

func (c *ShogunateClient) CreateEdict(issueRef, intent string) (*storage.Edict, error) {
	return c.createEdict(MethodCreateEdict, issueRef, intent)
}

func (c *ShogunateClient) CreateEdictSilent(issueRef, intent string) (*storage.Edict, error) {
	return c.createEdict(MethodCreateEdictSilent, issueRef, intent)
}

func (c *ShogunateClient) createEdict(method, issueRef, intent string) (*storage.Edict, error) {
	raw, err := c.conn.Call(context.Background(), method, CreateEdictParams{IssueRef: issueRef, Intent: intent})
	if err != nil {
		return nil, err
	}
	var r CreateEdictResult
	if err := wire.Decode(raw, &r); err != nil {
		return nil, fmt.Errorf("rpc: decode %s result: %w", method, err)
	}
	return r.Edict, nil
}

func (c *ShogunateClient) GetEdict(edictID uint) (*storage.Edict, error) {
	raw, err := c.conn.Call(context.Background(), MethodGetEdict, GetEdictParams{EdictID: edictID})
	if err != nil {
		return nil, err
	}
	var r GetEdictResult
	if err := wire.Decode(raw, &r); err != nil {
		return nil, fmt.Errorf("rpc: decode GetEdict result: %w", err)
	}
	return r.Edict, nil
}

func (c *ShogunateClient) GrantRulerSeal(edictID uint, notes string) error {
	return c.callVoid(context.Background(), MethodGrantRulerSeal, GrantRulerSealParams{EdictID: edictID, Notes: notes})
}

func (c *ShogunateClient) ListActiveEdicts() ([]storage.ActiveEdict, error) {
	raw, err := c.conn.Call(context.Background(), MethodListActiveEdicts, nil)
	if err != nil {
		return nil, err
	}
	var r ListActiveEdictsResult
	if err := wire.Decode(raw, &r); err != nil {
		return nil, err
	}
	return r.Edicts, nil
}

func (c *ShogunateClient) CancelZhengming(requestID string) {
	_ = c.callVoid(context.Background(), MethodCancelZhengming, CancelZhengmingParams{RequestID: requestID})
}

func (c *ShogunateClient) HandleZhengmingResponse(ctx context.Context, requestID, answer string) error {
	return c.callVoid(ctx, MethodHandleZhengming, HandleZhengmingParams{RequestID: requestID, Answer: answer})
}

func (c *ShogunateClient) AllowRunnerFallback(allow bool) {
	_ = c.callVoid(context.Background(), MethodAllowRunnerFallback, AllowRunnerFallbackParams{Allow: allow})
}

func (c *ShogunateClient) ClearSessionHistory(tabTarget string) error {
	return c.callVoid(context.Background(), MethodClearSessionHistory, ClearSessionHistoryParams{TabTarget: tabTarget})
}

func (c *ShogunateClient) RollbackSession(tabTarget string, snapshot int) error {
	return c.callVoid(context.Background(), MethodRollbackSession, RollbackSessionParams{TabTarget: tabTarget, Snapshot: snapshot})
}

func (c *ShogunateClient) AddSessionMessage(tabTarget, role, content string) error {
	return c.callVoid(context.Background(), MethodAddSessionMessage, AddSessionMessageParams{TabTarget: tabTarget, Role: role, Content: content})
}
