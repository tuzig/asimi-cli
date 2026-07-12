package shogunate

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/afittestide/asimi/shogunate/tools"
	"gopkg.in/yaml.v3"
)

//go:embed userconf/ministers.yaml
var builtinMinistersYAML []byte

// MinisterDef defines a minister's identity and capabilities in YAML.
type MinisterDef struct {
	ID          string `yaml:"id"`
	Role        string `yaml:"role"`
	Permissions string `yaml:"permissions"`
	Title       string `yaml:"title,omitempty"`
	Kanji       string `yaml:"kanji,omitempty"`
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

// LoadMinisters parses the embedded builtin ministers YAML.
func LoadMinisters() ([]MinisterDef, error) {
	var defs []MinisterDef
	if err := yaml.Unmarshal(builtinMinistersYAML, &defs); err != nil {
		return nil, fmt.Errorf("failed to parse builtin ministers YAML: %w", err)
	}
	return defs, nil
}

// LoadAllMinisters loads and merges minister definitions from all sources:
// 1. Embedded builtin ministers (userconf/ministers.yaml)
// 2. User home config: ~/.config/asimi/ministers.yaml
// 3. Project config: .agents/ministers.yaml
// Later sources override earlier ones by id.
func LoadAllMinisters(projectDir string) ([]MinisterDef, error) {
	byID := make(map[string]MinisterDef)

	// 1. Embedded builtins
	builtins, err := LoadMinisters()
	if err != nil {
		return nil, err
	}
	for _, d := range builtins {
		byID[d.ID] = d
	}

	// 2. User home config
	home, err := os.UserHomeDir()
	if err == nil {
		userPath := filepath.Join(home, ".config", "asimi", "ministers.yaml")
		if err := mergeMinistersFromFile(userPath, byID); err != nil {
			return nil, fmt.Errorf("loading user ministers: %w", err)
		}
	}

	// 3. Project config
	if projectDir != "" {
		projectPath := filepath.Join(projectDir, ".agents", "ministers.yaml")
		if err := mergeMinistersFromFile(projectPath, byID); err != nil {
			return nil, fmt.Errorf("loading project ministers: %w", err)
		}
	}

	defs := make([]MinisterDef, 0, len(byID))
	for _, d := range byID {
		defs = append(defs, d)
	}
	return defs, nil
}

// mergeMinistersFromFile loads minister defs from a YAML file and merges
// them into the map by id.
func mergeMinistersFromFile(path string, byID map[string]MinisterDef) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var defs []MinisterDef
	if err := yaml.Unmarshal(data, &defs); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	for _, d := range defs {
		byID[d.ID] = d
	}
	return nil
}
