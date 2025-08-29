package main

import (
	"context"
	"fmt"
	"os"

	"github.com/tmc/langchaingo/agents"
	"github.com/tmc/langchaingo/chains"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/fake"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
	"github.com/tmc/langchaingo/memory"
	"github.com/tmc/langchaingo/tools"
	"github.com/tmc/langchaingo/callbacks"
)

// getLLMClient creates and returns an LLM client based on the configuration
func getLLMClient(config *Config) (llms.Model, error) {
	switch config.LLM.Provider {
	case "fake":
		return fake.NewFakeLLM([]string{"AI:I am a large language model, trained by Google."}), nil
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
	case "googleai":
		// For GoogleAI, we need to set the API key
		apiKey := config.LLM.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("GEMINI_API_KEY")
			if apiKey == "" {
				return nil, fmt.Errorf("missing Google AI API key. Set it in the config file or via GEMINI_API_KEY environment variable")
			}
		}

		opts := []googleai.Option{
			googleai.WithDefaultModel(config.LLM.Model),
			googleai.WithAPIKey(apiKey),
		}

		// TODO: Add WithBaseURL to langchaingo's googleai implementation and send a PR
		// if config.LLM.BaseURL != "" {
		// 	opts = append(opts, googleai.WithAPIEndpoint(config.LLM.BaseURL))
		// }

		return googleai.New(context.Background(), opts...)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", config.LLM.Provider)
	}
}

// getAgent creates and returns a new conversational agent executor
func getAgent(config *Config, handler callbacks.Handler) (*agents.Executor, error) {
	llm, err := getLLMClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM client: %w", err)
	}

	agentTools := []tools.Tool{
		&toolWrapper{t: ReadFileTool{}, handler: handler},
		&toolWrapper{t: WriteFileTool{}, handler: handler},
		&toolWrapper{t: ListDirectoryTool{}, handler: handler},
		&toolWrapper{t: ReplaceTextTool{}, handler: handler},
	}

	agent := agents.NewConversationalAgent(llm, agentTools)
	executor := agents.NewExecutor(
		agent,
		agents.WithMemory(memory.NewConversationBuffer()),
		agents.WithCallbacksHandler(handler),
	)

	return executor, nil
}

// sendPromptToAgent sends a prompt to the agent and returns the response
func sendPromptToAgent(config *Config, prompt string) (string, error) {
	executor, err := getAgent(config, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create agent: %w", err)
	}

	response, err := chains.Run(context.Background(), executor, prompt)
	if err != nil {
		return "", fmt.Errorf("failed to run agent: %w", err)
	}

	return response, nil
}
