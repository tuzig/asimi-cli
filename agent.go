package main

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tmc/langchaingo/agents"
	"github.com/tmc/langchaingo/callbacks"
	"github.com/tmc/langchaingo/chains"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/fake"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
	"github.com/tmc/langchaingo/memory"
	"github.com/tmc/langchaingo/tools"
)

// Agent struct encapsulates the executor and the scheduler
type Agent struct {
	executor  *agents.Executor
	scheduler *CoreToolScheduler
}

// AgentOption is a functional option for configuring the agent.
type AgentOption func(*agentOptions)

type agentOptions struct {
	handler callbacks.Handler
	program *tea.Program
}

// WithCallbacks sets the callback handler for the agent.
func WithCallbacks(handler callbacks.Handler) AgentOption {
	return func(opts *agentOptions) {
		opts.handler = handler
	}
}

// WithTea sets the bubbletea program for the agent.
func WithTea(p *tea.Program) AgentOption {
	return func(opts *agentOptions) {
		opts.program = p
	}
}

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

// NewAgent creates and returns a new conversational agent
func NewAgent(config *Config, opts ...AgentOption) (*Agent, error) {
	options := &agentOptions{}
	for _, opt := range opts {
		opt(options)
	}

	llm, err := getLLMClient(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM client: %w", err)
	}

	agentTools := []tools.Tool{
		&toolWrapper{t: ReadFileTool{}, handler: options.handler},
		&toolWrapper{t: WriteFileTool{}, handler: options.handler},
		&toolWrapper{t: ListDirectoryTool{}, handler: options.handler},
		&toolWrapper{t: ReplaceTextTool{}, handler: options.handler},
		&toolWrapper{t: RunShellCommand{}, handler: options.handler},
	}

	executorOpts := []agents.Option{
		agents.WithMemory(memory.NewConversationBuffer()),
	}
	if options.handler != nil {
		executorOpts = append(executorOpts, agents.WithCallbacksHandler(options.handler))
	}

	executor := agents.NewExecutor(
		agents.NewConversationalAgent(llm, agentTools),
		executorOpts...,
	)

	var scheduler *CoreToolScheduler
	if options.program != nil {
		scheduler = NewCoreToolScheduler(options.program)
	}

	agent := &Agent{
		executor:  executor,
		scheduler: scheduler,
	}

	return agent, nil
}

// sendPromptToAgent sends a prompt to the agent and returns the response
func sendPromptToAgent(config *Config, prompt string) (string, error) {
	agent, err := NewAgent(config)
	if err != nil {
		return "", fmt.Errorf("failed to create agent: %w", err)
	}

	response, err := chains.Run(context.Background(), agent.executor, prompt)
	if err != nil {
		return "", fmt.Errorf("failed to run agent: %w", err)
	}

	return response, nil
}
