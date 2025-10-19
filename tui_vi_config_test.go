package main

import (
	"testing"
)

func TestTUIRespectsViModeConfig(t *testing.T) {
	tests := []struct {
		name           string
		viMode         *bool
		expectedViMode bool
	}{
		{
			name:           "default config enables vi mode",
			viMode:         nil,
			expectedViMode: true,
		},
		{
			name:           "explicitly enabled",
			viMode:         boolPtr(true),
			expectedViMode: true,
		},
		{
			name:           "explicitly disabled",
			viMode:         boolPtr(false),
			expectedViMode: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				LLM: LLMConfig{
					ViMode: tt.viMode,
				},
			}

			model := NewTUIModel(config)

			if model.prompt.ViMode != tt.expectedViMode {
				t.Errorf("TUI prompt.ViMode = %v, want %v", model.prompt.ViMode, tt.expectedViMode)
			}
		})
	}
}
