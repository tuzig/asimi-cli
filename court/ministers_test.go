package court

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/afittestide/asimi/internal/ministers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadMinisters_BuiltinHasAllFive(t *testing.T) {
	defs, err := LoadMinisters()
	require.NoError(t, err)

	ids := make(map[string]bool)
	for _, d := range defs {
		ids[d.ID] = true
	}
	for _, expected := range []string{ministers.Chancellor, ministers.Forge, ministers.Judge, ministers.Sage, ministers.Strategist} {
		assert.True(t, ids[expected], "builtin ministers must include %s", expected)
	}
}

func TestLoadMinisters_FieldsPopulated(t *testing.T) {
	defs, err := LoadMinisters()
	require.NoError(t, err)

	for _, d := range defs {
		assert.NotEmpty(t, d.ID, "ID must not be empty")
		assert.NotEmpty(t, d.Role, "Role must not be empty for %s", d.ID)
		assert.Len(t, d.Permissions, 9, "Permissions must be 9 chars for %s", d.ID)
	}
}

func TestNewMinister_KanjiInSystemPrompt(t *testing.T) {
	def := MinisterDef{ID: "test", Kanji: "工部", Role: "You are forge.", Permissions: "rwxrwxrwx"}
	base := NewMinisterBase(nil, nil, nil, "u", "p", nil)
	m := NewMinister(def, base)

	expected := "工部\n\nYou are forge."
	assert.Equal(t, expected, m.SystemPrompt())
}

func TestNewMinister_SystemPromptNoKanji(t *testing.T) {
	def := MinisterDef{ID: "test", Role: "You are a test minister.", Permissions: "rwxrwxrwx"}
	base := NewMinisterBase(nil, nil, nil, "u", "p", nil)
	m := NewMinister(def, base)

	assert.Equal(t, "You are a test minister.", m.SystemPrompt())
}

func TestLoadMinisters_KanjiPopulated(t *testing.T) {
	defs, err := LoadMinisters()
	require.NoError(t, err)

	// Build expected kanji from loaded defs instead of hard-coding
	for _, d := range defs {
		assert.NotEmpty(t, d.Kanji, "kanji must not be empty for %s", d.ID)
	}
}

func TestLoadMinisters_KanjiNotInRole(t *testing.T) {
	defs, err := LoadMinisters()
	require.NoError(t, err)

	// After the refactor, the kanji should be in the Kanji field,
	// not woven into the role text.
	for _, d := range defs {
		if d.Kanji != "" {
			assert.False(t, strings.Contains(d.Role, d.Kanji),
				"role for %s should not contain kanji %q", d.ID, d.Kanji)
		}
	}
}

func TestNewMinister_ToolsNilWithoutRegistry(t *testing.T) {
	def := MinisterDef{ID: "test", Permissions: "rwxrwxrwx"}
	base := NewMinisterBase(nil, nil, nil, "u", "p", nil)
	m := NewMinister(def, base)

	assert.Nil(t, m.Tools())
}

func TestLoadAllMinisters_ProjectOverride(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, ".agents")
	require.NoError(t, os.Mkdir(agentsDir, 0755))

	override := `- id: forge
  role: "custom forge role"
  permissions: "rwxr---w-"
`
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "ministers.yaml"), []byte(override), 0644))

	defs, err := LoadAllMinisters(dir)
	require.NoError(t, err)

	found := false
	for _, d := range defs {
		if d.ID == "forge" {
			assert.Equal(t, "custom forge role", d.Role, "project override should replace role")
			found = true
		}
	}
	assert.True(t, found, "forge should be present after override")
}
