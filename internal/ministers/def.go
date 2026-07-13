package ministers

// MinisterDef defines a minister's identity and capabilities in YAML.
type MinisterDef struct {
	ID          string `yaml:"id"`
	Role        string `yaml:"role"`
	Permissions string `yaml:"permissions"`
	Title       string `yaml:"title,omitempty"`
	Kanji       string `yaml:"kanji,omitempty"`
	Greeting    string `yaml:"greeting,omitempty"`
}

// Label returns the display label for a minister: "Kanji Title" when both
// are present, or just Title otherwise.
func (d MinisterDef) Label() string {
	if d.Kanji != "" && d.Title != "" {
		return d.Kanji + " " + d.Title
	}
	return d.Title
}
