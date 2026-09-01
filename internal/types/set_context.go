package types

// SetContextParams carries client-side credentials, project context,
// and per-connection flags to the court. Both the in-process Court
// and the daemon-side handler consume this struct to initialise (or
// re-initialise) the Bifrost LLM client.
type SetContextParams struct {
	Project        string            `msgpack:"project"`
	Username       string            `msgpack:"username"`
	ProjectRoot    string            `msgpack:"project_root"`
	WorktreePath   string            `msgpack:"worktree_path"`
	Branch         string            `msgpack:"branch"`
	APIKeys        map[string]string `msgpack:"api_keys,omitempty"`
	CodexAccountID string            `msgpack:"codex_account_id,omitempty"`
	IsolatedHost   bool              `msgpack:"isolated_host"`
	AtifAgentName  string            `msgpack:"atif_agent_name,omitempty"`
	Model          string            `msgpack:"model,omitempty"`
	Provider       string            `msgpack:"provider,omitempty"`
}
