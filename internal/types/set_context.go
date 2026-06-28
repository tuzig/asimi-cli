// Package types contains shared type definitions used across packages,
// breaking import cycles between rpc, shogunate, and shogunateapi.
package types

// SetContextParams carries client-side credentials and project context
// to the shogunate. Both the in-process Shogunate and the daemon-side
// handler consume this struct to initialise (or re-initialise) the
// Bifrost LLM client.
type SetContextParams struct {
	Project        string            `msgpack:"project"`
	Username       string            `msgpack:"username"`
	ProjectRoot    string            `msgpack:"project_root"`
	WorktreePath   string            `msgpack:"worktree_path"`
	Branch         string            `msgpack:"branch"`
	APIKeys        map[string]string `msgpack:"api_keys,omitempty"`
	CodexAccountID string            `msgpack:"codex_account_id,omitempty"`
}
