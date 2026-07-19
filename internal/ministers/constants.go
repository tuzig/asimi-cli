package ministers

// Well-known minister IDs. These carry structural meaning (tab ownership,
// seal chain order, ritual routing) and are used throughout the codebase
// instead of raw string literals.
const (
	Chancellor = "chancellor"
	Forge      = "forge"
	Judge      = "judge"
	Sage       = "sage"
	Strategist = "strategist"
	Ruler      = "ruler"
)

// DefaultTabIDs is the ordered list of ministers that get interactive tabs
// in the TUI: Forge, Sage, Judge, Chancellor.
var DefaultTabIDs = []string{Forge, Sage, Judge, Chancellor}

// SealChainIDs is the ordered list of minister IDs whose seals form the
// ascension chain: Judge → Sage → Ruler.
var SealChainIDs = []string{Judge, Sage, Ruler}
