package ministers

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

//go:embed userconf/ministers.yaml
var builtinMinistersYAML []byte

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

// LookupByID returns the MinisterDef with the given id from the slice,
// or a zero-value MinisterDef with just the ID set if not found.
func LookupByID(defs []MinisterDef, id string) MinisterDef {
	for _, d := range defs {
		if d.ID == id {
			return d
		}
	}
	return MinisterDef{ID: id}
}

// LookupMap converts a slice of MinisterDefs into a map keyed by ID.
func LookupMap(defs []MinisterDef) map[string]MinisterDef {
	m := make(map[string]MinisterDef, len(defs))
	for _, d := range defs {
		m[d.ID] = d
	}
	return m
}
