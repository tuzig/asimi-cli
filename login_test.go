package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCodexConstants verifies Codex OAuth constants match the Codex CLI fingerprint
func TestCodexConstants(t *testing.T) {
	assert.Equal(t, "app_EMoamEEZ73f0CkXaXp7hrann", codexClientID)
	assert.Equal(t, "https://auth.openai.com/oauth/authorize", codexAuthURL)
	assert.Equal(t, "https://auth.openai.com/oauth/token", codexTokenURL)
	assert.Equal(t, "http://localhost:1455/callback", codexRedirectURI)
	assert.Contains(t, codexScope, "api.connectors.invoke")
	assert.Contains(t, codexScope, "openid")
	assert.Contains(t, codexScope, "offline_access")
	assert.Equal(t, 1455, codexCallbackPort)
}

// TestBrowserCommand tests the browserCommand function without opening a real browser
func TestBrowserCommand(t *testing.T) {
	cmd, err := browserCommand("https://example.com")
	assert.NoError(t, err)
	assert.NotNil(t, cmd)
	assert.NotEmpty(t, cmd.Path)
}

// TestRandomString tests the randomString helper
func TestRandomString(t *testing.T) {
	s1 := randomString(32)
	s2 := randomString(32)

	assert.Len(t, s1, 32)
	assert.Len(t, s2, 32)
	assert.NotEqual(t, s1, s2, "Two random strings should differ")
}
