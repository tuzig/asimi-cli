package rpc

import "github.com/afittestide/asimi/storage"

// Wire DTOs for each Shogunate RPC method. Every input/output struct
// maps 1:1 to a method, even when the method takes or returns a single
// value — keeps the on-wire schema self-describing and easy to extend.

type HasMinisterParams struct {
	ID string `msgpack:"id"`
}
type HasMinisterResult struct {
	Has bool `msgpack:"has"`
}

type ResetMinisterSessionParams struct {
	ID string `msgpack:"id"`
}

type EdictKeyParams struct {
	EdictID uint `msgpack:"edict_id"`
}
type EdictKeyResult struct {
	Key storage.EdictKey `msgpack:"key"`
}

type CourtEdictKeyResult struct {
	Key storage.EdictKey `msgpack:"key"`
}

type CreateEdictParams struct {
	IssueRef string `msgpack:"issue_ref,omitempty"`
	Intent   string `msgpack:"intent,omitempty"`
}
type CreateEdictResult struct {
	Edict *storage.Edict `msgpack:"edict"`
}

type GetEdictParams struct {
	EdictID uint `msgpack:"edict_id"`
}
type GetEdictResult struct {
	Edict *storage.Edict `msgpack:"edict"`
}

type GrantRulerSealParams struct {
	EdictID uint   `msgpack:"edict_id"`
	Notes   string `msgpack:"notes,omitempty"`
}

type ListActiveEdictsResult struct {
	Edicts []storage.ActiveEdict `msgpack:"edicts"`
}

type CancelZhengmingParams struct {
	RequestID string `msgpack:"request_id"`
}

type HandleZhengmingParams struct {
	RequestID string `msgpack:"request_id"`
	Answer    string `msgpack:"answer"`
}

type AllowRunnerFallbackParams struct {
	Allow bool `msgpack:"allow"`
}

type ClearSessionHistoryParams struct {
	TabTarget string `msgpack:"tab_target"`
}

type RollbackSessionParams struct {
	TabTarget string `msgpack:"tab_target"`
	Snapshot  int    `msgpack:"snapshot"`
}

type AddSessionMessageParams struct {
	TabTarget string `msgpack:"tab_target"`
	Role      string `msgpack:"role"`
	Content   string `msgpack:"content"`
}
