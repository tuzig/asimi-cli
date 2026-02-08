package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureOllamaConfiguredMissingBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ollama is not expected on Windows hosts")
	}

	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)

	err := ensureOllamaConfigured("http://127.0.0.1:12345")
	require.Error(t, err)
	require.Contains(t, err.Error(), "ollama CLI not found")
}

func TestEnsureOllamaConfiguredSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ollama is not expected on Windows hosts")
	}

	fakePath := prepareFakeOllama(t)
	t.Setenv("PATH", fakePath)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/version", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, writeErr := w.Write([]byte(`{"version":"test"}`))
		require.NoError(t, writeErr)
	}))
	t.Cleanup(server.Close)

	err := ensureOllamaConfigured(server.URL)
	require.NoError(t, err)
}

func TestEnsureOllamaConfiguredServerError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ollama is not expected on Windows hosts")
	}

	fakePath := prepareFakeOllama(t)
	t.Setenv("PATH", fakePath)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/version", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	err := ensureOllamaConfigured(server.URL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "returned status 500")
}

func prepareFakeOllama(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "ollama")
	require.NoError(t, os.WriteFile(binaryPath, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	return dir
}

func TestTruncateMiddle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		message  string
		maxWidth int
		expected string
	}{
		{
			name:     "short message fits",
			message:  "Hello",
			maxWidth: 10,
			expected: "Hello",
		},
		{
			name:     "exact fit",
			message:  "Hello",
			maxWidth: 5,
			expected: "Hello",
		},
		{
			name:     "truncate long message",
			message:  "This is a very long error message that needs truncation",
			maxWidth: 30,
			// 30 - 1 (ellipsis) = 29 available
			// 29 / 3 = 9 for beginning, 20 for end
			expected: "This is a…hat needs truncation",
		},
		{
			name:     "truncate with 1/3 beginning",
			message:  "ABCDEFGHIJKLMNOPQRSTUVWXYZ",
			maxWidth: 16,
			// 16 - 1 (ellipsis) = 15 available
			// 15 / 3 = 5 for beginning, 10 for end
			expected: "ABCDE…QRSTUVWXYZ",
		},
		{
			name:     "very short maxWidth",
			message:  "Hello World",
			maxWidth: 4,
			expected: "H…ld",
		},
		{
			name:     "maxWidth of 3",
			message:  "Hello",
			maxWidth: 3,
			expected: "Hel",
		},
		{
			name:     "maxWidth of 0",
			message:  "Hello",
			maxWidth: 0,
			expected: "",
		},
		{
			name:     "empty message",
			message:  "",
			maxWidth: 10,
			expected: "",
		},
		{
			name:     "unicode characters",
			message:  "こんにちは世界です",
			maxWidth: 6,
			// 6 - 1 = 5 available, 5/3 = 1 beginning, 4 end
			expected: "こ…世界です",
		},
		{
			name:     "model error message",
			message:  "Model Error: API returned status 429: rate limit exceeded, please try again later",
			maxWidth: 50,
			// 50 - 1 = 49 available, 49/3 = 16 beginning, 33 end
			expected: "Model Error: API… exceeded, please try again later",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := TruncateMiddle(tt.message, tt.maxWidth)
			require.Equal(t, tt.expected, result)
			// Verify the result doesn't exceed maxWidth (counting runes)
			if tt.maxWidth > 0 {
				require.LessOrEqual(t, len([]rune(result)), tt.maxWidth, "result exceeds maxWidth")
			}
		})
	}
}
