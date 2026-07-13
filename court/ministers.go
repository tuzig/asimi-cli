package court

import (
	"context"

	"github.com/afittestide/asimi/internal/ministers"
	"github.com/afittestide/asimi/court/tools"
)

// MinisterDef is an alias for ministers.MinisterDef so existing code in
// the court package continues to work without mass refactoring.
type MinisterDef = ministers.MinisterDef

// LoadMinisters delegates to ministers.LoadMinisters.
func LoadMinisters() ([]MinisterDef, error) {
	return ministers.LoadMinisters()
}

// LoadAllMinisters delegates to ministers.LoadAllMinisters.
func LoadAllMinisters(projectDir string) ([]MinisterDef, error) {
	return ministers.LoadAllMinisters(projectDir)
}

// ministerImpl is the generic, YAML-driven minister type. It replaces the
// five hand-coded minister structs (Chancellor, Forge, Judge, Sage,
// Strategist) with a single implementation that derives its behaviour
// from a MinisterDef.
type ministerImpl struct {
	*MinisterBase
	def MinisterDef
}

// NewMinister creates a generic minister from a MinisterDef and shared base.
func NewMinister(def MinisterDef, base *MinisterBase) *ministerImpl {
	base.ministerID = def.ID
	m := &ministerImpl{
		MinisterBase: base,
		def:          def,
	}
	m.self = m
	return m
}

// ID returns the minister's unique identifier (from the def).
func (m *ministerImpl) ID() string {
	return m.def.ID
}

// SystemPrompt returns the kanji (if present) followed by the role text,
// fed to system_base.tmpl as the Role template variable.
func (m *ministerImpl) SystemPrompt() string {
	if m.def.Kanji != "" {
		return m.def.Kanji + "\n\n" + m.def.Role
	}
	return m.def.Role
}

// Tools returns the minister's LLM tools, derived from the permission
// string in the def via the central tool registry. No fallback path —
// the toolRegistry is always wired before ministers run.
func (m *ministerImpl) Tools() []Tool {
	if m.toolRegistry == nil {
		return nil
	}
	perm, err := tools.ParsePermissions(m.def.Permissions)
	if err != nil {
		m.logger.Warn("invalid permissions in minister def", "id", m.def.ID, "perm", m.def.Permissions, "error", err)
		return nil
	}
	registered := m.toolRegistry.ForPermissions(m.def.ID, perm)
	result := make([]Tool, len(registered))
	for i, t := range registered {
		result[i] = t
	}
	return result
}

// Run starts the minister's processing loop.
func (m *ministerImpl) Run(ctx context.Context) {
	m.RunLoop(ctx, m, nil, m.MinisterBase.processTask)
}


