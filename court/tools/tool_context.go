package tools

import (
	"context"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

// SessionIDKey is a context key for propagating the executing session's ID
// to tools via context.Context. This allows tools (e.g. RequestZhengming) to
// know which session invoked them, even when called through the scheduler
// or from a ritual step where the minister's own interactive session differs.
type SessionIDKey struct{}

// SessionIDFromContext extracts the session ID from the context, or returns
// "" if none is set.
func SessionIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(SessionIDKey{}).(string); ok {
		return v
	}
	return ""
}

// ToolContext carries shared identity and daemon-level resources to all tools.
// RepoInfo is a pointer so all contexts see live state (ProjectRoot, Branch, etc.)
// MinisterID is the one field that varies per minister tool set.
type ToolContext struct {
	RepoInfo   *repo.RepoInfo // shared pointer — all contexts see live state
	MinisterID string         // per-minister: "forge", "chancellor", etc.
	Username   string         // daemon-level: OS username for DB scoping
	Project    string         // daemon-level: project slug for DB scoping
	DB         *gorm.DB       // daemon-level singleton
}

// ProjectRoot returns the project root from the shared RepoInfo, or "" if unset.
func (tc ToolContext) ProjectRoot() string {
	if tc.RepoInfo != nil {
		return tc.RepoInfo.ProjectRoot
	}
	return ""
}

// RitualLauncher starts ritual workflows (implemented by MinisterBase).
type RitualLauncher interface {
	StartRitual(name string, key storage.EdictKey, inputs map[string]string) error
}

// MinisterConsultant dispatches work to a minister (implemented by MinisterBase).
// callerID is the minister ID of the caller, used to route output to the caller's tab.
type MinisterConsultant interface {
	ConsultMinister(ctx context.Context, callerID, ministerID string, key storage.EdictKey, work string) (string, error)
}
