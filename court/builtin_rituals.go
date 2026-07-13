package court

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

//go:embed builtin_rituals.yaml
var builtinRitualsYAML []byte

// LoadBuiltinRituals loads the embedded builtin rituals from the YAML file
func LoadEmbeddedRituals() (Rituals, error) {
	var rituals []*RitualDef
	if err := yaml.Unmarshal(builtinRitualsYAML, &rituals); err != nil {
		return nil, fmt.Errorf("failed to parse builtin rituals YAML: %w", err)
	}
	for _, r := range rituals {
		if err := ValidateRitual(r); err != nil {
			return nil, fmt.Errorf("invalid builtin ritual %s: %w", r.Name, err)
		}
	}
	return rituals, nil
}

// LoadAllRituals loads and merges rituals from all sources:
// 1. Embedded builtin rituals
// 2. User home config: $HOME/.config/asimi/rituals.yaml
// 3. Project config: .agents/rituals.yaml
// Later sources override earlier ones by name.
func LoadAllRituals(projectDir string) (Rituals, error) {
	byName := make(map[string]*RitualDef)

	// 1. Embedded builtins
	builtins, err := LoadEmbeddedRituals()
	if err != nil {
		return nil, err
	}
	for _, r := range builtins {
		byName[r.Name] = r
	}

	// 2. User home config
	home, err := os.UserHomeDir()
	if err == nil {
		userPath := filepath.Join(home, ".config", "asimi", "rituals.yaml")
		if err := mergeRitualsFromFile(userPath, byName); err != nil {
			return nil, fmt.Errorf("loading user rituals: %w", err)
		}
	}

	// 3. Project config
	if projectDir != "" {
		projectPath := filepath.Join(projectDir, ".agents", "rituals.yaml")
		if err := mergeRitualsFromFile(projectPath, byName); err != nil {
			return nil, fmt.Errorf("loading project rituals: %w", err)
		}
	}

	rituals := make([]*RitualDef, 0, len(byName))
	for _, r := range byName {
		rituals = append(rituals, r)
	}
	return rituals, nil
}

// mergeRitualsFromFile loads rituals from a YAML file and merges them into the map
func mergeRitualsFromFile(path string, byName map[string]*RitualDef) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var rituals []*RitualDef
	if err := yaml.Unmarshal(data, &rituals); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	for _, r := range rituals {
		if err := ValidateRitual(r); err != nil {
			return fmt.Errorf("invalid ritual %s in %s: %w", r.Name, path, err)
		}
		byName[r.Name] = r
	}
	return nil
}
