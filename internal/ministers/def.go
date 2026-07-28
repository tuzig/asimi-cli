package ministers

// MinisterDef defines a minister's identity and capabilities in YAML.
type MinisterDef struct {
	ID          string   `yaml:"id"`
	Role        string   `yaml:"role"`
	Permissions string   `yaml:"permissions"`
	Title       string   `yaml:"title,omitempty"`
	Kanji       string   `yaml:"kanji,omitempty"`
	Greeting    string   `yaml:"greeting,omitempty"`
	ExtraTools  []string `yaml:"extra_tools,omitempty"`
}

// Label returns the display label for a minister: just the English title.
// The kanji field is kept for minister identity (greetings, system prompts)
// but is not shown in tab labels.
func (d MinisterDef) Label() string {
	return d.Title
}
