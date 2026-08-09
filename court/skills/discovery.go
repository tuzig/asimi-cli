package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillInfo represents a discovered skill with its metadata.
type SkillInfo struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	// Ministers is the list of minister IDs this skill applies to.
	// Empty/nil means "all ministers".
	Ministers []string `yaml:"ministers,omitempty"`
}

// Discover scans .agents/skills/*/SKILL.md relative to projectRoot
// and returns a map of skills keyed by ministerID.
// The key "*" holds skills that apply to all ministers (no ministers field set).
// Returns nil when the skills directory is missing or empty.
func Discover(projectRoot string) map[string][]SkillInfo {
	if projectRoot == "" {
		return nil
	}
	skillsDir := filepath.Join(projectRoot, ".agents", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil
	}

	result := make(map[string][]SkillInfo)

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		info, err := parseSkillFile(skillPath)
		if err != nil {
			continue
		}

		if len(info.Ministers) == 0 {
			// No ministers specified — applies to all
			result["*"] = append(result["*"], info)
		} else {
			for _, ministerID := range info.Ministers {
				result[ministerID] = append(result[ministerID], info)
			}
		}
	}

	return result
}

// ForMinister returns the skills applicable to a given minister from the map.
// Skills with no ministers field (keyed "*") are always included.
func ForMinister(skillsMap map[string][]SkillInfo, ministerID string) []SkillInfo {
	if skillsMap == nil {
		return nil
	}
	all := skillsMap["*"]
	specific := skillsMap[ministerID]
	if len(all) == 0 && len(specific) == 0 {
		return nil
	}
	combined := make([]SkillInfo, 0, len(all)+len(specific))
	combined = append(combined, all...)
	combined = append(combined, specific...)
	return combined
}

// FormatIndex returns a compact markdown block of the skills index.
// Returns empty string when skills is empty.
func FormatIndex(skills []SkillInfo) string {
	if len(skills) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("### Available Skills\n\n")
	for _, s := range skills {
		b.WriteString("- **")
		b.WriteString(s.Name)
		b.WriteString("**: ")
		b.WriteString(s.Description)
		b.WriteString("\n")
	}
	b.WriteString("\nWhen a task requires a described skill, read the full SKILL.md and follow the instructions")
	return b.String()
}

func parseSkillFile(path string) (SkillInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SkillInfo{}, err
	}
	return ParseFrontmatter(data)
}

// ParseFrontmatter extracts YAML frontmatter from raw SKILL.md content.
func ParseFrontmatter(data []byte) (SkillInfo, error) {
	content := string(data)
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return SkillInfo{}, fmt.Errorf("no frontmatter found")
	}

	start := 4
	if strings.HasPrefix(content, "---\r\n") {
		start = 5
	}

	endIdx := strings.Index(content[start:], "---")
	if endIdx == -1 {
		return SkillInfo{}, fmt.Errorf("unclosed frontmatter")
	}

	fmData := content[start : start+endIdx]

	var info SkillInfo
	if err := yaml.Unmarshal([]byte(fmData), &info); err != nil {
		return SkillInfo{}, err
	}

	return info, nil
}
