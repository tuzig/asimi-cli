# Configuration System Review & Improvement Suggestions

## Current State Analysis

The current configuration system in `config.go` demonstrates a solid foundation with several good practices:

### Strengths
- **Multi-source configuration**: Supports user-level, project-level, and environment variable configuration
- **Secure credential storage**: Implements keyring-based storage for API keys and OAuth tokens
- **Hierarchical loading**: Proper precedence order (env vars > project config > user config)
- **TOML format**: Human-readable configuration format
- **OAuth support**: Modern authentication flow implementation

### Areas for Improvement

## 1. Configuration Structure & Organization

### Current Issues
- **Monolithic LLMConfig**: The `LLMConfig` struct has 50+ fields, making it unwieldy
- **Mixed concerns**: Authentication, behavior, and provider-specific settings are all mixed together
- **Inconsistent naming**: Some fields use camelCase, others use snake_case in struct tags
- **No validation**: No built-in validation for configuration values

### Proposed Structure
```go
type Config struct {
    Server      ServerConfig      `koanf:"server"`
    Database    DatabaseConfig    `koanf:"database"`
    Logging     LoggingConfig     `koanf:"logging"`
    LLM         LLMConfig         `koanf:"llm"`
    Auth        AuthConfig        `koanf:"auth"`
    Tools       ToolsConfig       `koanf:"tools"`
    UI          UIConfig          `koanf:"ui"`
    Security    SecurityConfig    `koanf:"security"`
    Performance PerformanceConfig `koanf:"performance"`
    Permission  PermissionConfig  `koanf:"permission"`
    Hooks       HooksConfig       `koanf:"hooks"`
    StatusLine  StatusLineConfig  `koanf:"statusline"`
}

type LLMConfig struct {
    Provider string `koanf:"provider" validate:"required,oneof=anthropic openai googleai"`
    Model    string `koanf:"model" validate:"required"`
    ReasoningEffort string `koanf:"reasoning_effort"` // validated manually via config.ValidateReasoningEffort: empty|low|medium|high
    BaseURL  string `koanf:"base_url" validate:"omitempty,url"`
}

type AuthConfig struct {
    Method           string            `koanf:"method" validate:"oneof=apikey oauth"`
    APIKey           string            `koanf:"api_key"`
    KeyringEnabled   bool              `koanf:"keyring_enabled"`
    OAuthConfig      OAuthConfig       `koanf:"oauth"`
    ProviderSettings map[string]string `koanf:"provider_settings"`
}

type ToolsConfig struct {
    Bash BashConfig `koanf:"bash"`
    MCP  MCPConfig  `koanf:"mcp"`
}

type BashConfig struct {
    DefaultTimeoutMs          int  `koanf:"default_timeout_ms" validate:"min=1000,max=300000"`
    MaxTimeoutMs              int  `koanf:"max_timeout_ms" validate:"min=1000,max=600000"`
    MaxOutputLength           int  `koanf:"max_output_length" validate:"min=1024,max=1048576"`
    MaintainProjectWorkingDir bool `koanf:"maintain_project_working_dir"`
}

type MCPConfig struct {
    Timeout                    int      `koanf:"timeout" validate:"min=1000,max=60000"`
    ToolTimeout                int      `koanf:"tool_timeout" validate:"min=1000,max=30000"`
    MaxOutputTokens            int      `koanf:"max_output_tokens" validate:"min=100,max=100000"`
    EnableAllProjectServers    bool     `koanf:"enable_all_project_servers"`
    EnabledServers             []string `koanf:"enabled_servers"`
    DisabledServers            []string `koanf:"disabled_servers"`
}

type UIConfig struct {
    Theme                 string `koanf:"theme" validate:"oneof=dark light auto"`
    DisableTerminalTitle  bool   `koanf:"disable_terminal_title"`
    PreferredNotifChannel string `koanf:"preferred_notif_channel"`
}

type SecurityConfig struct {
    DisableErrorReporting     bool   `koanf:"disable_error_reporting"`
    DisableTelemetry          bool   `koanf:"disable_telemetry"`
    DisableNonEssentialTraffic bool  `koanf:"disable_non_essential_traffic"`
    HttpProxy                 string `koanf:"http_proxy" validate:"omitempty,url"`
    HttpsProxy                string `koanf:"https_proxy" validate:"omitempty,url"`
}

type PerformanceConfig struct {
    MaxOutputTokens       int  `koanf:"max_output_tokens" validate:"min=100,max=200000"`
    MaxThinkingTokens     int  `koanf:"max_thinking_tokens" validate:"min=0,max=100000"`
    DisableCostWarnings   bool `koanf:"disable_cost_warnings"`
    UseBuiltinRipgrep     bool `koanf:"use_builtin_ripgrep"`
    CleanupPeriodDays     int  `koanf:"cleanup_period_days" validate:"min=1,max=365"`
}
```

## 2. Configuration Validation

### Current Issues
- No validation of configuration values
- Runtime errors for invalid configurations
- No type checking for enum-like values

### Proposed Solution
```go
import "github.com/go-playground/validator/v10"

type ConfigValidator struct {
    validator *validator.Validate
}

func NewConfigValidator() *ConfigValidator {
    v := validator.New()
    
    // Register custom validators
    v.RegisterValidation("provider", validateProvider)
    v.RegisterValidation("theme", validateTheme)
    
    return &ConfigValidator{validator: v}
}

func (cv *ConfigValidator) Validate(config *Config) error {
    if err := cv.validator.Struct(config); err != nil {
        return fmt.Errorf("configuration validation failed: %w", err)
    }
    
    // Custom validation logic
    if err := cv.validateProviderModel(config.LLM.Provider, config.LLM.Model); err != nil {
        return err
    }
    
    return nil
}

func validateProvider(fl validator.FieldLevel) bool {
    provider := fl.Field().String()
    validProviders := []string{"anthropic", "openai", "googleai"}
    for _, v := range validProviders {
        if provider == v {
            return true
        }
    }
    return false
}
```

## 3. Configuration Schema & Documentation

### Current Issues
- No schema definition for configuration files
- Limited documentation of available options
- No auto-completion support

### Proposed Solution
Create JSON Schema for configuration validation and IDE support:

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "Asimi Configuration",
  "type": "object",
  "properties": {
    "llm": {
      "type": "object",
      "properties": {
        "provider": {
          "type": "string",
          "enum": ["anthropic", "openai", "googleai"],
          "description": "LLM provider to use"
        },
        "model": {
          "type": "string",
          "description": "Model name to use with the provider"
        },
        "reasoning_effort": {
          "type": "string",
          "enum": ["low", "medium", "high"],
          "description": "Reasoning effort level"
        }
      },
      "required": ["provider", "model"]
    }
  }
}
```

## 4. Environment Variable Handling

### Current Issues
- Inconsistent environment variable naming
- No clear documentation of available env vars
- Manual handling of provider-specific env vars

### Proposed Improvements
```go
// Environment variable mapping with clear naming convention
var envVarMap = map[string]string{
    "ASIMI_LLM_PROVIDER":     "llm.provider",
    "ASIMI_LLM_MODEL":        "llm.model",
    "ASIMI_REASONING_EFFORT": "llm.reasoning_effort",
    "ASIMI_AUTH_METHOD":      "auth.method",
    "ASIMI_BASH_TIMEOUT":     "tools.bash.default_timeout_ms",
    "ASIMI_MCP_TIMEOUT":      "tools.mcp.timeout",
    "ASIMI_UI_THEME":         "ui.theme",
    "ASIMI_SECURITY_PROXY":   "security.http_proxy",
    
    // Provider-specific API keys
    "ANTHROPIC_API_KEY":      "auth.provider_settings.anthropic_api_key",
    "OPENAI_API_KEY":         "auth.provider_settings.openai_api_key",
    "GOOGLE_API_KEY":         "auth.provider_settings.google_api_key",
}

func loadEnvironmentVariables(k *koanf.Koanf) error {
    for envVar, configPath := range envVarMap {
        if value := os.Getenv(envVar); value != "" {
            k.Set(configPath, value)
        }
    }
    return nil
}
```

## 5. Configuration Profiles

### Current Issues
- No support for different configuration profiles
- Difficult to switch between development/production settings

### Proposed Solution
```go
type ProfileConfig struct {
    Name        string `koanf:"name"`
    Description string `koanf:"description"`
    Config      Config `koanf:"config"`
}

func LoadProfile(profileName string) (*Config, error) {
    profilePath := filepath.Join(getConfigDir(), "profiles", profileName+".toml")
    // Load profile-specific configuration
}

// Example profiles:
// ~/.config/asimi/profiles/development.toml
// ~/.config/asimi/profiles/production.toml
// ~/.config/asimi/profiles/testing.toml
```

## 6. Configuration Migration

### Current Issues
- No versioning of configuration format
- No migration path for breaking changes

### Proposed Solution
```go
type ConfigVersion struct {
    Version int    `koanf:"version"`
    Config  Config `koanf:"config"`
}

type ConfigMigrator struct {
    migrations map[int]MigrationFunc
}

type MigrationFunc func(oldConfig map[string]interface{}) (map[string]interface{}, error)

func (cm *ConfigMigrator) Migrate(configData []byte) ([]byte, error) {
    // Parse version and apply necessary migrations
}
```

## 7. Improved Error Handling

### Current Issues
- Generic error messages
- No context about which configuration file failed
- No suggestions for fixing configuration errors

### Proposed Solution
```go
type ConfigError struct {
    File    string
    Section string
    Key     string
    Value   interface{}
    Err     error
    Suggestion string
}

func (ce *ConfigError) Error() string {
    return fmt.Sprintf("configuration error in %s [%s.%s]: %v. Suggestion: %s", 
        ce.File, ce.Section, ce.Key, ce.Err, ce.Suggestion)
}

func validateAndSuggest(config *Config) error {
    if config.LLM.Provider == "" {
        return &ConfigError{
            Section: "llm",
            Key: "provider",
            Err: errors.New("provider is required"),
            Suggestion: "Set provider to one of: anthropic, openai, googleai",
        }
    }
    return nil
}
```

## 8. Configuration Templates

### Current Issues
- No easy way to bootstrap configuration
- Users need to manually create configuration files

### Proposed Solution
```go
func GenerateConfigTemplate(provider string) ([]byte, error) {
    template := ConfigTemplate{
        LLM: LLMTemplate{
            Provider: provider,
            Model: getDefaultModel(provider),
        },
        Auth: AuthTemplate{
            Method: "apikey",
            KeyringEnabled: true,
        },
        // ... other sensible defaults
    }
    
    return toml.Marshal(template)
}

// CLI command: asimi config init --provider=anthropic
```

## 9. Configuration Watching

### Current Issues
- Configuration changes require restart
- No hot-reloading of configuration

### Proposed Solution
```go
import "github.com/fsnotify/fsnotify"

type ConfigWatcher struct {
    watcher *fsnotify.Watcher
    config  *Config
    onChange func(*Config)
}

func (cw *ConfigWatcher) Watch() error {
    go func() {
        for {
            select {
            case event := <-cw.watcher.Events:
                if event.Op&fsnotify.Write == fsnotify.Write {
                    newConfig, err := LoadConfig()
                    if err == nil {
                        cw.onChange(newConfig)
                    }
                }
            }
        }
    }()
    return nil
}
```

## 10. Security Improvements

### Current Issues
- File permissions could be more restrictive
- No encryption for sensitive data in fallback mode
- API keys visible in process environment

### Proposed Solutions
```go
// More restrictive file permissions
const (
    ConfigFileMode = 0o600  // User read/write only
    ConfigDirMode  = 0o700  // User access only
)

// Encrypted fallback storage
func encryptSensitiveData(data []byte, key []byte) ([]byte, error) {
    // Use AES-GCM for encryption
}

// Environment variable scrubbing
func scrubEnvironment() {
    sensitiveVars := []string{
        "ANTHROPIC_API_KEY",
        "OPENAI_API_KEY", 
        "GOOGLE_API_KEY",
    }
    for _, v := range sensitiveVars {
        os.Unsetenv(v)
    }
}
```

## Implementation Priority

1. **High Priority**
   - Configuration structure refactoring
   - Validation implementation
   - Better error handling

2. **Medium Priority**
   - Configuration profiles
   - Environment variable improvements
   - Configuration templates

3. **Low Priority**
   - Configuration watching
   - Migration system
   - Advanced security features

## Breaking Changes Considerations

- Implement gradual migration with deprecation warnings
- Maintain backward compatibility for at least 2 major versions
- Provide migration tools and clear upgrade documentation
- Use semantic versioning for configuration schema

## Testing Strategy

- Unit tests for each configuration component
- Integration tests for multi-source loading
- Validation tests for all configuration options
- Security tests for credential handling
- Performance tests for large configuration files

This refactoring would significantly improve the maintainability, security, and user experience of the configuration system while providing a solid foundation for future enhancements.