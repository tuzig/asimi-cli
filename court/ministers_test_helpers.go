package court

// Test helpers for constructing ministers from their builtin YAML definitions.
// These preserve the test API (NewSecretary, NewForge, etc.) while the
// underlying implementation is now the generic ministerImpl.

// builtinDefByID returns the builtin MinisterDef for the given id.
func builtinDefByID(id string) MinisterDef {
	defs, _ := LoadMinisters()
	for _, d := range defs {
		if d.ID == id {
			return d
		}
	}
	return MinisterDef{ID: id}
}

// NewSecretary creates a secretary minister for tests.
func NewSecretary(base *MinisterBase) *ministerImpl {
	return NewMinister(builtinDefByID("secretary"), base)
}

// NewForge creates a forge minister for tests.
func NewForge(base *MinisterBase) *ministerImpl {
	return NewMinister(builtinDefByID("forge"), base)
}

// NewJudge creates a judge minister for tests.
// The second parameter is ignored (was CIRunner, now removed).
func NewJudge(base *MinisterBase, _ any) *ministerImpl {
	return NewMinister(builtinDefByID("judge"), base)
}

// NewChancellor creates a chancellor minister for tests.
func NewChancellor(base *MinisterBase) *ministerImpl {
	return NewMinister(builtinDefByID("chancellor"), base)
}

// NewWar creates a war minister for tests.
func NewWar(base *MinisterBase) *ministerImpl {
	return NewMinister(builtinDefByID("war"), base)
}

// WarRole is the role text for the war minister, kept for tests that
// validate its content. Loaded from the embedded YAML definition.
var WarRole = builtinDefByID("war").Role
