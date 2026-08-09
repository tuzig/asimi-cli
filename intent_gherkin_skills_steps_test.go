package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/afittestide/asimi/court/skills"
	"github.com/cucumber/godog"
	"github.com/stretchr/testify/require"
)

// skillsTestState holds the state for skills discovery feature scenarios.
type skillsTestState struct {
	t           *testing.T
	projectRoot string
	skillsMap   map[string][]skills.SkillInfo
	parsedSkill skills.SkillInfo
	parseErr    error
	formatted   string
	forgeResult []skills.SkillInfo
}

func newSkillsTestState(t *testing.T) *skillsTestState {
	return &skillsTestState{
		t: t,
	}
}

func registerSkillsDiscoveryStepDefs(ctx *godog.ScenarioContext, t *testing.T) {
	s := &skillsTestState{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*s = *newSkillsTestState(t)
		// Create project root with .agents/skills directory
		dir := t.TempDir()
		s.projectRoot = dir
		skillsDir := filepath.Join(dir, ".agents", "skills")
		require.NoError(t, os.MkdirAll(skillsDir, 0755))
		return ctx, nil
	})

	// --- Givens ---

	ctx.Step(`^the skills directory is empty$`, func() error {
		// Already created empty in Before
		return nil
	})

	ctx.Step(`^a skill "([^"]*)" with ministers "([^"]*)"$`, func(skillName, ministersJSON string) error {
		skillDir := filepath.Join(s.projectRoot, ".agents", "skills", skillName)
		require.NoError(t, os.MkdirAll(skillDir, 0755))

		var ministersLine string
		if ministersJSON != "" {
			ministersLine = fmt.Sprintf("ministers: %s\n", ministersJSON)
		}
		content := fmt.Sprintf("---\nname: %s\ndescription: Skill %s\n%s---\n# %s\n",
			skillName, skillName, ministersLine, skillName)
		return os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644)
	})

	ctx.Step(`^a skill "([^"]*)" without ministers$`, func(skillName string) error {
		skillDir := filepath.Join(s.projectRoot, ".agents", "skills", skillName)
		require.NoError(t, os.MkdirAll(skillDir, 0755))

		content := fmt.Sprintf("---\nname: %s\ndescription: Skill %s\n---\n# %s\n",
			skillName, skillName, skillName)
		return os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644)
	})

	ctx.Step(`^a SKILL\.md with:$`, func(body *godog.DocString) error {
		s.parsedSkill, s.parseErr = skills.ParseFrontmatter([]byte(body.Content))
		return nil
	})

	ctx.Step(`^a SKILL\.md with content "([^"]*)"$`, func(content string) error {
		s.parsedSkill, s.parseErr = skills.ParseFrontmatter([]byte(content))
		return nil
	})

	ctx.Step(`^(\d+) skills: "([^"]*)" and "([^"]*)"$`, func(_, name1, name2 string) error {
		s.formatted = skills.FormatIndex([]skills.SkillInfo{
			{Name: name1, Description: "desc " + name1},
			{Name: name2, Description: "desc " + name2},
		})
		return nil
	})

	ctx.Step(`^a nil skills map$`, func() error {
		s.forgeResult = skills.ForMinister(nil, "forge")
		return nil
	})

	// --- Whens ---

	ctx.Step(`^Discover is called$`, func() error {
		s.skillsMap = skills.Discover(s.projectRoot)
		return nil
	})

	ctx.Step(`^ParseFrontmatter is called$`, func() error {
		// Already called in the Given step
		return nil
	})

	ctx.Step(`^FormatIndex is called$`, func() error {
		// Already called in the Given step
		return nil
	})

	ctx.Step(`^ForMinister\("([^"]*)"\) is called$`, func(ministerID string) error {
		s.forgeResult = skills.ForMinister(s.skillsMap, ministerID)
		return nil
	})

	// --- Thens ---

	ctx.Step(`^the result map is non-nil and empty$`, func() error {
		if s.skillsMap == nil {
			return fmt.Errorf("expected non-nil map, got nil")
		}
		if len(s.skillsMap) != 0 {
			return fmt.Errorf("expected empty map, got %d entries", len(s.skillsMap))
		}
		return nil
	})

	ctx.Step(`^ForMinister\("([^"]*)"\) returns (\d+) skill(s)? named "([^"]*)"$`, func(ministerID string, count int, _, name string) error {
		result := skills.ForMinister(s.skillsMap, ministerID)
		if len(result) != count {
			return fmt.Errorf("expected %d skill(s) for minister %q, got %d", count, ministerID, len(result))
		}
		if count > 0 && result[0].Name != name {
			return fmt.Errorf("expected skill %q for minister %q, got %q", name, ministerID, result[0].Name)
		}
		return nil
	})

	ctx.Step(`^ForMinister\("([^"]*)"\) returns (\d+) skill(s)?$`, func(ministerID string, count int, _ string) error {
		result := skills.ForMinister(s.skillsMap, ministerID)
		if len(result) != count {
			return fmt.Errorf("expected %d skill(s) for minister %q, got %d", count, ministerID, len(result))
		}
		return nil
	})

	ctx.Step(`^the result map has no "\*" key$`, func() error {
		if _, ok := s.skillsMap["*"]; ok {
			return fmt.Errorf("expected no '*' key in result map")
		}
		return nil
	})

	ctx.Step(`^the result map has a "\*" key with (\d+) skill(s)? named "([^"]*)"$`, func(count int, _, name string) error {
		all, ok := s.skillsMap["*"]
		if !ok {
			return fmt.Errorf("expected '*' key in result map")
		}
		if len(all) != count {
			return fmt.Errorf("expected %d skill(s) under '*', got %d", count, len(all))
		}
		if count > 0 && all[0].Name != name {
			return fmt.Errorf("expected skill %q under '*', got %q", name, all[0].Name)
		}
		return nil
	})

	ctx.Step(`^ForMinister\("([^"]*)"\) includes both "([^"]*)" and "([^"]*)"$`, func(ministerID, name1, name2 string) error {
		result := skills.ForMinister(s.skillsMap, ministerID)
		names := make(map[string]bool)
		for _, sk := range result {
			names[sk.Name] = true
		}
		if !names[name1] {
			return fmt.Errorf("expected result to include %q, got %v", name1, names)
		}
		if !names[name2] {
			return fmt.Errorf("expected result to include %q, got %v", name2, names)
		}
		return nil
	})

	ctx.Step(`^the parsed name is "([^"]*)"$`, func(expected string) error {
		require.NoError(s.t, s.parseErr)
		if s.parsedSkill.Name != expected {
			return fmt.Errorf("expected name %q, got %q", expected, s.parsedSkill.Name)
		}
		return nil
	})

	ctx.Step(`^the parsed description is "([^"]*)"$`, func(expected string) error {
		require.NoError(s.t, s.parseErr)
		if s.parsedSkill.Description != expected {
			return fmt.Errorf("expected description %q, got %q", expected, s.parsedSkill.Description)
		}
		return nil
	})

	ctx.Step(`^the parsed ministers are \[([^\]]*)\]$`, func(ministersStr string) error {
		require.NoError(s.t, s.parseErr)
		expected := strings.Split(ministersStr, ",")
		for i := range expected {
			expected[i] = strings.TrimSpace(expected[i])
			expected[i] = strings.Trim(expected[i], "\"")
		}
		if len(s.parsedSkill.Ministers) != len(expected) {
			return fmt.Errorf("expected ministers %v, got %v", expected, s.parsedSkill.Ministers)
		}
		for i, m := range expected {
			if s.parsedSkill.Ministers[i] != m {
				return fmt.Errorf("expected minister[%d]=%q, got %q", i, m, s.parsedSkill.Ministers[i])
			}
		}
		return nil
	})

	ctx.Step(`^parsing returns an error$`, func() error {
		if s.parseErr == nil {
			return fmt.Errorf("expected parse error, got none")
		}
		return nil
	})

	ctx.Step(`^the index contains "([^"]*)"$`, func(expected string) error {
		if !strings.Contains(s.formatted, expected) {
			return fmt.Errorf("expected index to contain %q, got:\n%s", expected, s.formatted)
		}
		return nil
	})

	ctx.Step(`^the result is nil$`, func() error {
		if s.forgeResult != nil {
			return fmt.Errorf("expected nil result, got %v", s.forgeResult)
		}
		return nil
	})

	ctx.Step(`^the two SkillInfo structs are deeply equal$`, func() error {
		// Create two identical skills in different temp dirs
		dir1 := s.t.TempDir()
		dir2 := s.t.TempDir()
		for _, dir := range []string{dir1, dir2} {
			skillDir := filepath.Join(dir, ".agents", "skills", "go-testing")
			require.NoError(s.t, os.MkdirAll(skillDir, 0755))
			content := "---\nname: go-testing\ndescription: Patterns for writing Go tests\nministers: [forge]\n---\n# Instructions\n"
			require.NoError(s.t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644))
		}
		m1 := skills.Discover(dir1)
		m2 := skills.Discover(dir2)
		f1 := skills.ForMinister(m1, "forge")
		f2 := skills.ForMinister(m2, "forge")

		if len(f1) != 1 || len(f2) != 1 {
			return fmt.Errorf("expected 1 skill each, got %d and %d", len(f1), len(f2))
		}
		if f1[0].Name != f2[0].Name || f1[0].Description != f2[0].Description {
			return fmt.Errorf("skill info mismatch: %+v vs %+v", f1[0], f2[0])
		}
		if len(f1[0].Ministers) != len(f2[0].Ministers) {
			return fmt.Errorf("minister count mismatch: %d vs %d", len(f1[0].Ministers), len(f2[0].Ministers))
		}
		for i, m := range f1[0].Ministers {
			if f2[0].Ministers[i] != m {
				return fmt.Errorf("minister[%d] mismatch: %q vs %q", i, m, f2[0].Ministers[i])
			}
		}
		return nil
	})

}

// Helper method called by the generic step pattern for asterisk-key assertions.
func (s *skillsTestState) theResultMapHasAKeyWithSkillNamed(key string, count int, name string) error {
	all, ok := s.skillsMap[key]
	if !ok {
		return fmt.Errorf("expected key %q in result map", key)
	}
	if len(all) != count {
		return fmt.Errorf("expected %d skill(s) under %q, got %d", count, key, len(all))
	}
	if count > 0 && all[0].Name != name {
		return fmt.Errorf("expected skill %q under %q, got %q", name, key, all[0].Name)
	}
	return nil
}

// RegisterSkillsDiscovery ensures the step definitions are registered.
// This is called from intent_gherkin_test.go.
func RegisterSkillsDiscoveryStepDefs(ctx *godog.ScenarioContext, t *testing.T) {
	registerSkillsDiscoveryStepDefs(ctx, t)
}
