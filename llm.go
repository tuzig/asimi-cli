package main

import (
	"context"
	"fmt"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
)

// getLLMClient creates and returns an LLM client based on the configuration
func getLLMClient(config *Config) (llms.Model, error) {
	switch config.LLM.Provider {
	case "ollama":
		// For Ollama, we can use default options or customize based on config
		opts := []ollama.Option{
			ollama.WithModel(config.LLM.Model),
		}
		
		if config.LLM.BaseURL != "" {
			opts = append(opts, ollama.WithServerURL(config.LLM.BaseURL))
		}
		
		return ollama.New(opts...)
	case "openai":
		// For OpenAI, we need to set the API key
		opts := []openai.Option{
			openai.WithModel(config.LLM.Model),
		}
		
		if config.LLM.APIKey != "" {
			opts = append(opts, openai.WithToken(config.LLM.APIKey))
		}
		
		if config.LLM.BaseURL != "" {
			opts = append(opts, openai.WithBaseURL(config.LLM.BaseURL))
		}
		
		return openai.New(opts...)
	case "anthropic":
		// For Anthropic, we need to set the API key
		opts := []anthropic.Option{
			anthropic.WithModel(config.LLM.Model),
		}
		
		if config.LLM.APIKey != "" {
			opts = append(opts, anthropic.WithToken(config.LLM.APIKey))
		}
		
		if config.LLM.BaseURL != "" {
			opts = append(opts, anthropic.WithBaseURL(config.LLM.BaseURL))
		}
		
		return anthropic.New(opts...)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", config.LLM.Provider)
	}
}

// sendPrompt sends a prompt to the configured LLM and returns the response
func sendPrompt(config *Config, prompt string) (string, error) {
	// Get the LLM client based on configuration
	llm, err := getLLMClient(config)
	if err != nil {
		return "", fmt.Errorf("failed to create LLM client: %w", err)
	}

	// Create a context for the operation
	ctx := context.Background()

	// Send the prompt to the LLM
	completion, err := llms.GenerateFromSinglePrompt(ctx, llm, prompt)
	if err != nil {
		return "", fmt.Errorf("failed to call LLM: %w", err)
	}

	return completion, nil
}