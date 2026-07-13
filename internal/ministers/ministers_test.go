package ministers

import (
	"testing"

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
	for _, expected := range []string{Chancellor, Forge, Judge, Sage, Strategist} {
		assert.True(t, ids[expected], "builtin ministers must include %s", expected)
	}
}

func TestMinisterDef_Label(t *testing.T) {
	tests := []struct {
		name string
		def  MinisterDef
		want string
	}{
		{"both kanji and title", MinisterDef{Kanji: "宰相", Title: "Chancellor"}, "宰相 Chancellor"},
		{"only title", MinisterDef{Title: "Chancellor"}, "Chancellor"},
		{"only kanji", MinisterDef{Kanji: "宰相"}, ""},
		{"neither", MinisterDef{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.def.Label())
		})
	}
}

func TestLookupByID(t *testing.T) {
	defs := []MinisterDef{
		{ID: "chancellor", Title: "Chancellor"},
		{ID: "sage", Title: "Sage"},
	}

	d := LookupByID(defs, "sage")
	assert.Equal(t, "Sage", d.Title)

	// Not found — returns zero-value with just ID set
	d = LookupByID(defs, "ruler")
	assert.Equal(t, "ruler", d.ID)
	assert.Empty(t, d.Title)
}

func TestLookupMap(t *testing.T) {
	defs := []MinisterDef{
		{ID: "chancellor", Title: "Chancellor"},
		{ID: "sage", Title: "Sage"},
	}

	m := LookupMap(defs)
	assert.Equal(t, "Chancellor", m["chancellor"].Title)
	assert.Equal(t, "Sage", m["sage"].Title)
	assert.Len(t, m, 2)
}

func TestDefaultTabIDs(t *testing.T) {
	assert.Equal(t, []string{Chancellor, Sage, Forge, Judge}, DefaultTabIDs)
}

func TestSealChainIDs(t *testing.T) {
	assert.Equal(t, []string{Judge, Sage, Ruler}, SealChainIDs)
}
