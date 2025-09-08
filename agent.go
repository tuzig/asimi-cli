package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

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
	"github.com/tmc/langchaingo/prompts"
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

var promptPartials = map[string]any{
	"SandboxStatus":  "none",
	"UserMemory":     "",
	"ProjectContext": "",
	"Env":            "",
	"ReadFile":       "read_file",
	"WriteFile":      "write_file",
	"Grep":           "grep",
	"Glob":           "glob",
	"Edit":           "replace_text",
	"Shell":          "run_shell_command",
	"ReadManyFiles":  "read_many_files",
	"Memory":         "",
	"LS":             "list_files",
	"history":        "",
}

//go:embed prompts/system_prompt.tmpl
var systemPromptTemplate string

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
func getLLMClient(config *Config, cb callbacks.Handler) (llms.Model, error) {
	switch config.LLM.Provider {
	case "fake":
		llm := fake.NewFakeLLM([]string{})
		return llm, nil
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

	llm, err := getLLMClient(config, options.handler)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM client: %w", err)
	}

	return NewAgentWithLLM(config, llm, opts...)
}

// NewAgentWithLLM creates and returns a new conversational agent with a given LLM
func NewAgentWithLLM(config *Config, llm llms.Model, opts ...AgentOption) (*Agent, error) {
	options := &agentOptions{}
	for _, opt := range opts {
		opt(options)
	}

	agentTools := []tools.Tool{
		&toolWrapper{t: ReadFileTool{}, handler: options.handler},
		&toolWrapper{t: WriteFileTool{}, handler: options.handler},
		&toolWrapper{t: ListDirectoryTool{}, handler: options.handler},
		&toolWrapper{t: ReplaceTextTool{}, handler: options.handler},
		&toolWrapper{t: RunShellCommand{}, handler: options.handler},
		&toolWrapper{t: ReadManyFilesTool{}, handler: options.handler},
	}

	partials := make(map[string]any, len(promptPartials))
	for k, v := range promptPartials {
		partials[k] = v
	}
	partials["UserMemory"] = readAgentsMDFromCWD()
	partials["Env"] = buildEnvBlock()

	promptTemplate := prompts.PromptTemplate{
		Template:         systemPromptTemplate,
		TemplateFormat:   prompts.TemplateFormatGoTemplate,
		InputVariables:   []string{"input", "agent_scratchpad"},
		PartialVariables: partials,
	}

	agent := agents.NewConversationalAgent(llm, agentTools, agents.WithPrompt(promptTemplate))

	executorOpts := []agents.Option{
		agents.WithMemory(memory.NewConversationBuffer()),
	}
	if options.handler != nil {
		executorOpts = append(executorOpts, agents.WithCallbacksHandler(options.handler))
	}

	executor := agents.NewExecutor(
		agent,
		executorOpts...,
	)

	var scheduler *CoreToolScheduler
	if options.program != nil {
		scheduler = NewCoreToolScheduler(options.program)
	}

	agentInstance := &Agent{
		executor:  executor,
		scheduler: scheduler,
	}

	return agentInstance, nil
}

// buildEnvBlock constructs a minimal <env> XML snippet containing OS and paths.
func buildEnvBlock() string {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	root := findProjectRoot(cwd)
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "bash"
	}
	osName := map[string]string{
		"darwin":    "macOS",
		"linux":     "Linux",
		"windows":   "Windows",
		"freebsd":   "FreeBSD",
		"openbsd":   "OpenBSD",
		"netbsd":    "NetBSD",
		"dragonfly": "DragonFlyBSD",
		"solaris":   "Solaris",
		"aix":       "AIX",
		"android":   "Android",
		"ios":       "iOS",
	}[runtime.GOOS]
	if osName == "" {
		osName = runtime.GOOS
	}
	return fmt.Sprintf(`
<env>
 <os>%s</os>
 <paths>
  <cwd>%s</cwd>
  <project_root>%s</project_root>
  <home>%s</home>
 </paths>
</env>\n `, runtime.GOOS, shell, root, home)
}

// readAgentsMDFromCWD reads the contents of AGENTS.md from the current
// working directory. If the file does not exist or an error occurs, it
// returns an empty string.
func readAgentsMDFromCWD() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	path := filepath.Join(wd, "AGENTS.md")
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// sendPromptToAgent sends a prompt to the agent and returns the response
func sendPromptToAgent(config *Config, prompt string) (string, error) {
	agent, err := NewAgent(config)
	if err != nil {
		return "", fmt.Errorf("failed to create agent: %w", err)
	}

	response, err := chains.Call(context.Background(), agent.executor, map[string]any{
		"input": prompt,
	})
	if err != nil {
		return "", fmt.Errorf("failed to compile system prompt: %w", err)
	}

	output, ok := response["output"].(string)
	if !ok {
		return "", fmt.Errorf("invalid agent output type")
	}

	return output, nil
}
