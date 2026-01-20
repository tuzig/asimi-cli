package config

// LLMConfig holds LLM provider settings.
// This type is shared between main and shogunate packages.
type LLMConfig struct {
	Provider                   string `koanf:"provider"`
	Model                      string `koanf:"model"`
	APIKey                     string `koanf:"api_key"`
	BaseURL                    string `koanf:"base_url"`
	MaxThinkingTokens          int    `koanf:"max_thinking_tokens"`
	MaxTurns                   int    `koanf:"max_turns"`
	DisableContextSanitization bool   `koanf:"disable_sanitization"`
	AuthToken                  string `koanf:"auth_token"`
	RefreshToken               string `koanf:"refresh_token"`
	ExperimentalModels         bool   `koanf:"experimental_models"`
}
