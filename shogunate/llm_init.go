package shogunate

import (
	"context"
	"fmt"
	"log/slog"

	internalconfig "github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/repo"
	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
)

// ConfigureLLMRequest carries everything the daemon needs to
// initialise a Bifrost client and wire it into every minister session.
// All fields are wire-safe; no Go-side pointers or channels.
type ConfigureLLMRequest struct {
	Provider                 string `msgpack:"provider"`
	Model                    string `msgpack:"model"`
	BaseURL                  string `msgpack:"base_url,omitempty"`
	MaxTurns                 int    `msgpack:"max_turns,omitempty"`
	MaxThinkingTokens        int    `msgpack:"max_thinking_tokens,omitempty"`
	RequestTimeoutSeconds    int    `msgpack:"request_timeout_seconds,omitempty"`
	StreamIdleTimeoutSeconds int    `msgpack:"stream_idle_timeout_seconds,omitempty"`
	ProjectRoot              string `msgpack:"project_root,omitempty"`
	AgentsFile               string `msgpack:"agents_file,omitempty"`
}

// InitBifrost creates a Bifrost client using the shogunate Account
// (keyring-backed credentials) and slog logger. Formerly lived in
// main.go; moved here so the daemon can initialise its own client
// without the TUI having to ship one across the wire.
func InitBifrost(ctx context.Context, requestTimeout, streamIdleTimeout int, baseURL string) (*bifrost.Bifrost, error) {
	return bifrost.Init(ctx, schemas.BifrostConfig{
		Account: NewAccount(requestTimeout, streamIdleTimeout, baseURL),
		Logger:  NewBifrostLogger(slog.Default()),
	})
}

// ConfigureLLM initialises a Bifrost client on the daemon side and
// applies it to every minister via the existing ConfigureModel path.
// This replaces the in-process-only ConfigureModel(LLMProvider, ...)
// flow for callers that speak the wire protocol.
func (s *Shogunate) ConfigureLLM(ctx context.Context, req ConfigureLLMRequest) error {
	if s == nil {
		return fmt.Errorf("shogunate: not initialised")
	}
	client, err := InitBifrost(ctx, req.RequestTimeoutSeconds, req.StreamIdleTimeoutSeconds, req.BaseURL)
	if err != nil {
		return fmt.Errorf("shogunate: init bifrost: %w", err)
	}
	cfg := &SessionConfig{
		LLM: internalconfig.LLMConfig{
			Provider:                 req.Provider,
			Model:                    req.Model,
			BaseURL:                  req.BaseURL,
			MaxTurns:                 req.MaxTurns,
			MaxThinkingTokens:        req.MaxThinkingTokens,
			RequestTimeoutSeconds:    req.RequestTimeoutSeconds,
			StreamIdleTimeoutSeconds: req.StreamIdleTimeoutSeconds,
		},
		AgentsFile: req.AgentsFile,
	}
	repoInfo := repo.RepoInfo{ProjectRoot: req.ProjectRoot}
	s.ConfigureModel(client, cfg, repoInfo)
	s.logger.Info("shogunate LLM configured", "provider", req.Provider, "model", req.Model)
	return nil
}
