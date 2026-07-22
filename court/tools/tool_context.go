package tools

import (
	"context"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/storage"
	"gorm.io/gorm"
)

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
