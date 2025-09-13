package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config represents the application configuration structure
type Config struct {
	Server     ServerConfig     `koanf:"server"`
	Database   DatabaseConfig   `koanf:"database"`
	Logging    LoggingConfig    `koanf:"logging"`
	LLM        LLMConfig        `koanf:"llm"`
	Permission PermissionConfig `koanf:"permission"`
	Hooks      HooksConfig      `koanf:"hooks"`
	StatusLine StatusLineConfig `koanf:"statusline"`
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Host string `koanf:"host"`
	Port int    `koanf:"port"`
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Host     string `koanf:"host"`
	Port     int    `koanf:"port"`
	User     string `koanf:"user"`
	Password string `koanf:"password"`
	Name     string `koanf:"name"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string `koanf:"level"`
	Format string `koanf:"format"`
}

// LLMConfig holds LLM configuration
type LLMConfig struct {
	Provider                      string            `koanf:"provider"`
	Model                         string            `koanf:"model"`
	APIKey                        string            `koanf:"api_key"`
	BaseURL                       string            `koanf:"base_url"`
	APIKeyHelper                  string            `koanf:"api_key_helper"`
	CleanupPeriodDays             int               `koanf:"cleanup_period_days"`
	Env                           map[string]string `koanf:"env"`
	IncludeCoAuthoredBy           bool              `koanf:"include_coauthored_by"`
	ForceLoginMethod              string            `koanf:"force_login_method"`
	EnableAllProjectMcpServers    bool              `koanf:"enable_all_project_mcp_servers"`
	EnabledMcpjsonServers         []string          `koanf:"enabled_mcpjson_servers"`
	DisabledMcpjsonServers        []string          `koanf:"disabled_mcpjson_servers"`
	AwsAuthRefresh                string            `koanf:"aws_auth_refresh"`
	AwsCredentialExport           string            `koanf:"aws_credential_export"`
	AutoUpdates                   bool              `koanf:"auto_updates"`
	PreferredNotifChannel         string            `koanf:"preferred_notif_channel"`
	Theme                         string            `koanf:"theme"`
	Verbose                       bool              `koanf:"verbose"`
	AnthropicAPIKey               string            `koanf:"anthropic_api_key"`
	AnthropicAuthToken            string            `koanf:"anthropic_auth_token"`
	AnthropicCustomHeaders        string            `koanf:"anthropic_custom_headers"`
	AnthropicSmallFastModel       string            `koanf:"anthropic_small_fast_model"`
	AnthropicSmallFastModelRegion string            `koanf:"anthropic_small_fast_model_aws_region"`
	AwsBearerTokenBedrock         string            `koanf:"aws_bearer_token_bedrock"`
	BashDefaultTimeoutMs          int               `koanf:"bash_default_timeout_ms"`
	BashMaxTimeoutMs              int               `koanf:"bash_max_timeout_ms"`
	BashMaxOutputLength           int               `koanf:"bash_max_output_length"`
	BashMaintainProjectWorkingDir bool              `koanf:"bash_maintain_project_working_dir"`
	ApiKeyHelperTtlMs             int               `koanf:"api_key_helper_ttl_ms"`
	SkipAutoInstall               bool              `koanf:"skip_auto_install"`
	MaxOutputTokens               int               `koanf:"max_output_tokens"`
	UseBedrock                    bool              `koanf:"use_bedrock"`
	UseVertex                     bool              `koanf:"use_vertex"`
	SkipBedrockAuth               bool              `koanf:"skip_bedrock_auth"`
	SkipVertexAuth                bool              `koanf:"skip_vertex_auth"`
	DisableNonesentialTraffic     bool              `koanf:"disable_nonesential_traffic"`
	DisableTerminalTitle          bool              `koanf:"disable_terminal_title"`
	DisableAutoUpdater            bool              `koanf:"disable_auto_updater"`
	DisableBugCommand             bool              `koanf:"disable_bug_command"`
	DisableCostWarnings           bool              `koanf:"disable_cost_warnings"`
	DisableErrorReporting         bool              `koanf:"disable_error_reporting"`
	DisableNonEssentialModelCalls bool              `koanf:"disable_non_essential_model_calls"`
	DisableTelemetry              bool              `koanf:"disable_telemetry"`
	HttpProxy                     string            `koanf:"http_proxy"`
	HttpsProxy                    string            `koanf:"https_proxy"`
	MaxThinkingTokens             int               `koanf:"max_thinking_tokens"`
	McpTimeout                    int               `koanf:"mcp_timeout"`
	McpToolTimeout                int               `koanf:"mcp_tool_timeout"`
	MaxMcpOutputTokens            int               `koanf:"max_mcp_output_tokens"`
	UseBuiltinRipgrep             bool              `koanf:"use_builtin_ripgrep"`
}

// PermissionConfig holds permission configuration
type PermissionConfig struct {
	Allow                        []string `koanf:"allow"`
	Ask                          []string `koanf:"ask"`
	Deny                         []string `koanf:"deny"`
	AdditionalDirectories        []string `koanf:"additional_directories"`
	DefaultMode                  string   `koanf:"default_mode"`
	DisableBypassPermissionsMode string   `koanf:"disable_bypass_permissions_mode"`
}

// HooksConfig holds hooks configuration
type HooksConfig struct {
	PreTool  []string `koanf:"pre_tool"`
	PostTool []string `koanf:"post_tool"`
}

// StatusLineConfig holds status line configuration
type StatusLineConfig struct {
	Enabled  bool   `koanf:"enabled"`
	Template string `koanf:"template"`
}

// LoadConfig loads configuration from multiple sources
func LoadConfig() (*Config, error) {
    // Create a new koanf instance
    k := koanf.New(".")

    homeDir, err := os.UserHomeDir()
    if err != nil {
        log.Printf("Failed to get user home directory: %v", err)
    } else {
        userConfigPath := filepath.Join(homeDir, ".config", "asimi", "conf.toml")
        if err := k.Load(file.Provider(userConfigPath), toml.Parser()); err != nil {
            log.Printf("Failed to load user config from %s: %v", userConfigPath, err)
        }
    }

    // 2. Load project-local config (overrides user config if present)
    projectConfigPath := filepath.Join(".asimi", "conf.toml")
    if _, err := os.Stat(projectConfigPath); err == nil {
        if err := k.Load(file.Provider(projectConfigPath), toml.Parser()); err != nil {
            log.Printf("Failed to load project config from %s: %v", projectConfigPath, err)
        }
    } else if !os.IsNotExist(err) {
        log.Printf("Unable to stat project config at %s: %v", projectConfigPath, err)
    }

    // 3. Load environment variables
    // Environment variables with prefix "ASIMI_" will override config values
    // e.g., ASIMI_SERVER_PORT=8080 will override the server port
    k.Load(env.Provider(".", env.Opt{
        Prefix: "ASIMI_",
        TransformFunc: func(key, value string) (string, any) {
            // Transform environment variable names to match config keys
            // ASIMI_SERVER_PORT becomes "server.port"
            key = strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(key, "ASIMI_")), "_", ".")
            return key, value
        },
    }), nil)

	// Special handling for API keys from standard environment variables
	// Check for OPENAI_API_KEY if using OpenAI
	if k.String("llm.provider") == "openai" && k.String("llm.api_key") == "" {
		if openaiKey := os.Getenv("OPENAI_API_KEY"); openaiKey != "" {
			k.Set("llm.api_key", openaiKey)
		}
	}

	// Check for ANTHROPIC_API_KEY if using Anthropic
	if k.String("llm.provider") == "anthropic" && k.String("llm.api_key") == "" {
		if anthropicKey := os.Getenv("ANTHROPIC_API_KEY"); anthropicKey != "" {
			k.Set("llm.api_key", anthropicKey)
		}
	}

	// Unmarshal the configuration into our struct
	var config Config
	if err := k.Unmarshal("", &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &config, nil
}
