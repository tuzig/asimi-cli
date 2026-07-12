package shogunate

// Test helpers for constructing ministers from their builtin YAML definitions.
// These preserve the test API (NewChancellor, NewForge, etc.) while the
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

// NewChancellor creates a chancellor minister for tests.
func NewChancellor(base *MinisterBase) *ministerImpl {
	return NewMinister(builtinDefByID("chancellor"), base)
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

// NewSage creates a sage minister for tests.
func NewSage(base *MinisterBase) *ministerImpl {
	return NewMinister(builtinDefByID("sage"), base)
}

// NewStrategist creates a strategist minister for tests.
func NewStrategist(base *MinisterBase) *ministerImpl {
	return NewMinister(builtinDefByID("strategist"), base)
}

// StrategistRole is the role text for the strategist, kept for tests that
// validate its content. Loaded from the embedded YAML definition.
var StrategistRole = builtinDefByID("strategist").Role
