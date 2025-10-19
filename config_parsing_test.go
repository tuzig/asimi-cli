package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestViModeConfigParsing(t *testing.T) {
	// Create a temporary directory for test config
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".asimi")
	err := os.MkdirAll(configDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}

	tests := []struct {
		name           string
		configContent  string
		expectedViMode bool
	}{
		{
			name: "vi_mode = false",
			configContent: `[llm]
provider = "anthropic"
model = "test-model"
vi_mode = false
`,
			expectedViMode: false,
		},
		{
			name: "vi_mode = true",
			configContent: `[llm]
provider = "anthropic"
model = "test-model"
vi_mode = true
`,
			expectedViMode: true,
		},
		{
			name: "vi_mode not specified (default)",
			configContent: `[llm]
provider = "anthropic"
model = "test-model"
`,
			expectedViMode: true, // Default is true
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write test config
			configPath := filepath.Join(configDir, "conf.toml")
			err := os.WriteFile(configPath, []byte(tt.configContent), 0644)
			if err != nil {
				t.Fatalf("Failed to write config: %v", err)
			}

			// Change to temp directory so LoadConfig finds our test config
			originalWd, _ := os.Getwd()
			defer os.Chdir(originalWd)
			os.Chdir(tmpDir)

			// Load config
			config, err := LoadConfig()
			if err != nil {
				t.Fatalf("Failed to load config: %v", err)
			}

			// Check vi_mode setting
			result := config.IsViModeEnabled()
			if result != tt.expectedViMode {
				t.Errorf("IsViModeEnabled() = %v, want %v", result, tt.expectedViMode)
			}

			// Clean up for next test
			os.Remove(configPath)
		})
	}
}
