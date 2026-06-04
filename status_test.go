package main

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/require"
)

func TestShortenProviderModel(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    string
		expected string
	}{
		{
			name:     "Claude Haiku 4.5",
			provider: "anthropic",
			model:    "Claude-Haiku-4.5",
			expected: "Claude-Haiku-4.5",
		},
		{
			name:     "Claude 3.5 Sonnet",
			provider: "anthropic",
			model:    "Claude 3.5 Sonnet",
			expected: "Claude-3.5-Sonnet",
		},
		{
			name:     "claude-3-5-sonnet-20241022",
			provider: "anthropic",
			model:    "claude-3-5-sonnet-20241022",
			expected: "Claude-3.5-Sonnet",
		},
		{
			name:     "claude-3-haiku-20240307",
			provider: "anthropic",
			model:    "claude-3-haiku-20240307",
			expected: "Claude-3-Haiku",
		},
		{
			name:     "claude-3-5-haiku-latest",
			provider: "anthropic",
			model:    "claude-3-5-haiku-latest",
			expected: "Claude-3.5-Haiku",
		},
		{
			name:     "GPT-4 Turbo",
			provider: "openai",
			model:    "gpt-4-turbo",
			expected: "GPT-4T",
		},
		{
			name:     "GPT-3.5",
			provider: "openai",
			model:    "gpt-3.5-turbo",
			expected: "GPT-3.5",
		},
		{
			name:     "Gemini Pro",
			provider: "google",
			model:    "gemini-pro",
			expected: "Gemini-Pro",
		},
		{
			name:     "Gemini Flash",
			provider: "googleai",
			model:    "gemini-1.5-flash",
			expected: "Gemini-Flash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shortenProviderModel(tt.provider, tt.model)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestStatusComponentIcon(t *testing.T) {
	tests := []struct {
		name     string
		verified bool
		hasError bool
		expected string
	}{
		{name: "error takes priority", verified: true, hasError: true, expected: "❌"},
		{name: "error without verified", verified: false, hasError: true, expected: "❌"},
		{name: "verified shows check", verified: true, hasError: false, expected: "✅"},
		{name: "unverified shows question", verified: false, hasError: false, expected: "❓"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := StatusComponent{Verified: tt.verified, HasError: tt.hasError}
			require.Equal(t, tt.expected, s.getStatusIcon())
		})
	}
}

func TestSetVerified(t *testing.T) {
	s := StatusComponent{}
	require.False(t, s.Verified)
	s.SetVerified()
	require.True(t, s.Verified)
}

func TestRenderRightSectionIcon(t *testing.T) {
	tests := []struct {
		name     string
		verified bool
		hasError bool
		icon     string
	}{
		{name: "unverified shows question icon", verified: false, hasError: false, icon: "❓"},
		{name: "verified shows check icon", verified: true, hasError: false, icon: "✅"},
		{name: "error shows x icon", verified: false, hasError: true, icon: "❌"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := StatusComponent{
				Provider: "anthropic",
				Model:    "claude-3-5-sonnet-20241022",
				Verified: tt.verified,
				HasError: tt.hasError,
				Style:    lipgloss.NewStyle(),
			}
			rendered := s.renderRightSection()
			// Strip ANSI escape codes by checking icon presence
			require.Contains(t, rendered, tt.icon)
		})
	}
}
