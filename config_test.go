package main

import (
	"testing"
)

func TestIsViModeEnabled(t *testing.T) {
	tests := []struct {
		name     string
		viMode   *bool
		expected bool
	}{
		{
			name:     "nil (default)",
			viMode:   nil,
			expected: true, // Default is enabled
		},
		{
			name:     "explicitly enabled",
			viMode:   boolPtr(true),
			expected: true,
		},
		{
			name:     "explicitly disabled",
			viMode:   boolPtr(false),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				LLM: LLMConfig{
					ViMode: tt.viMode,
				},
			}

			result := config.IsViModeEnabled()
			if result != tt.expected {
				t.Errorf("IsViModeEnabled() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// Helper function to create a bool pointer
func boolPtr(b bool) *bool {
	return &b
}
