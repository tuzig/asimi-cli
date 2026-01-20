package shogunate

// RitualType identifies predefined workflow templates
type RitualType string

const (
	// Edict-level rituals (run once per edict)
	RitualFeature     RitualType = "feature"     // Full cycle: planning → forging → judgment → review
	RitualBugfix      RitualType = "bugfix"      // Similar to feature but may skip some review
	RitualHotfix      RitualType = "hotfix"      // Expedited: minimal review, urgent priority
	RitualChore       RitualType = "chore"       // Maintenance: planning only, no forge/judge/censor
	RitualAssassinate RitualType = "assassinate" // Emergency production fix

	// Ling-level sub-rituals (run per-ling)
	RitualTestMethod   RitualType = "test-method"   // Forge → Judge → Censor for one test
	RitualSimpleChange RitualType = "simple-change" // Just Forge (no CI/review)
	RitualCodeChange   RitualType = "code-change"   // Forge → Judge → Censor for code modification
)

// String returns the string representation of RitualType
func (r RitualType) String() string {
	return string(r)
}

// IsEdictLevel returns true if this ritual type is for edict-level orchestration
func (r RitualType) IsEdictLevel() bool {
	switch r {
	case RitualFeature, RitualBugfix, RitualHotfix, RitualChore, RitualAssassinate:
		return true
	default:
		return false
	}
}

// IsLingLevel returns true if this ritual type is for per-ling processing
func (r RitualType) IsLingLevel() bool {
	switch r {
	case RitualTestMethod, RitualSimpleChange, RitualCodeChange:
		return true
	default:
		return false
	}
}
