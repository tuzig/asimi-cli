package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCodexConstants verifies Codex OAuth constants match the Codex CLI fingerprint
func TestCodexConstants(t *testing.T) {
	assert.Equal(t, "app_EMoamEEZ73f0CkXaXp7hrann", codexClientID)
	assert.Equal(t, "https://auth.openai.com/oauth/authorize", codexAuthURL)
	assert.Equal(t, "https://auth.openai.com/oauth/token", codexTokenURL)
	assert.Equal(t, "http://localhost:1455/auth/callback", codexRedirectURI)
	assert.Equal(t, "openid profile email offline_access", codexScope)
	assert.Equal(t, 1455, codexCallbackPort)
	assert.Equal(t, "/auth/callback", codexCallbackPath)
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

// TestCodexRedirectURIPath verifies the redirect URI path matches the callback path
func TestCodexRedirectURIPath(t *testing.T) {
	assert.True(t, strings.HasSuffix(codexRedirectURI, codexCallbackPath))
}

// makeTestJWT builds a minimal JWT with the given chatgpt_account_id in the
// "https://api.openai.com/auth" claim.
func makeTestJWT(accountID string) string {
	header := `{"alg":"RS256","typ":"JWT"}`
	payload := fmt.Sprintf(`{"https://api.openai.com/auth":{"chatgpt_account_id":"%s"}}`, accountID)
	signature := "fake-signature"
	return base64.RawURLEncoding.EncodeToString([]byte(header)) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
		signature
}

// TestExtractCodexAccountID tests extracting the chatgpt_account_id from a JWT
func TestExtractCodexAccountID(t *testing.T) {
	t.Run("valid JWT with account ID", func(t *testing.T) {
		jwt := makeTestJWT("org-abc123")
		id, err := extractCodexAccountID(jwt)
		assert.NoError(t, err)
		assert.Equal(t, "org-abc123", id)
	})

	t.Run("JWT missing auth claim", func(t *testing.T) {
		header := `{"alg":"RS256","typ":"JWT"}`
		payload := `{"sub":"user123"}`
		jwt := base64.RawURLEncoding.EncodeToString([]byte(header)) + "." +
			base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
			"sig"
		_, err := extractCodexAccountID(jwt)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing https://api.openai.com/auth")
	})

	t.Run("JWT with auth claim but no account ID", func(t *testing.T) {
		header := `{"alg":"RS256","typ":"JWT"}`
		payload := `{"https://api.openai.com/auth":{}}`
		jwt := base64.RawURLEncoding.EncodeToString([]byte(header)) + "." +
			base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
			"sig"
		_, err := extractCodexAccountID(jwt)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing chatgpt_account_id")
	})

	t.Run("invalid JWT (not 3 parts)", func(t *testing.T) {
		_, err := extractCodexAccountID("not.a.jwt.extra")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected 3 parts")
	})

	t.Run("invalid base64", func(t *testing.T) {
		_, err := extractCodexAccountID("header.!!!.signature")
		assert.Error(t, err)
	})
}

// TestCodexOAuthCredentialRoundtrip tests serialize/deserialize of the
// codexOAuthCredential struct.
func TestCodexOAuthCredentialRoundtrip(t *testing.T) {
	original := codexOAuthCredential{
		AccessToken:  "access-token-123",
		RefreshToken: "refresh-token-456",
		ExpiresAt:    1700000000,
		AccountID:    "org-test",
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded codexOAuthCredential
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original, decoded)

	// Verify parseCodexOAuthCredential recognizes the JSON blob
	parsed, ok := parseCodexOAuthCredential(string(data))
	assert.True(t, ok)
	assert.Equal(t, original, parsed)
}

// TestParseCodexOAuthCredentialPlainString tests that a plain API key string
// is not recognized as an OAuth credential.
func TestParseCodexOAuthCredentialPlainString(t *testing.T) {
	_, ok := parseCodexOAuthCredential("sk-proj-plaintext-key")
	assert.False(t, ok)
}

// TestParseCodexOAuthCredentialEmptyAccessToken tests that JSON with an empty
// access token is not recognized as a valid OAuth credential.
func TestParseCodexOAuthCredentialEmptyAccessToken(t *testing.T) {
	raw := `{"access_token":"","refresh_token":"rt","expires_at":0,"account_id":""}`
	_, ok := parseCodexOAuthCredential(raw)
	assert.False(t, ok)
}

// TestRefreshCodexToken tests the full refreshCodexToken() flow.
// It uses a mock HTTP server (by overriding codexTokenURL) and requires
// keyring access — tests skip gracefully if keyring is unavailable.
func TestRefreshCodexToken(t *testing.T) {
	// Save and restore codexTokenURL
	originalTokenURL := codexTokenURL
	defer func() { codexTokenURL = originalTokenURL }()

	newJWT := makeTestJWT("org-refreshed")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "refresh_token", r.FormValue("grant_type"))
		assert.Equal(t, "old-refresh-token", r.FormValue("refresh_token"))
		assert.Equal(t, codexClientID, r.FormValue("client_id"))

		resp := codexTokenResponse{
			AccessToken:  newJWT,
			RefreshToken: "new-refresh-token",
			ExpiresIn:    3600,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	codexTokenURL = srv.URL

	t.Run("expired token refreshes via mock server", func(t *testing.T) {
		oldCred := codexOAuthCredential{
			AccessToken:  "expired-access-token",
			RefreshToken: "old-refresh-token",
			ExpiresAt:    1, // expired
			AccountID:    "org-old",
		}
		data, _ := json.Marshal(oldCred)

		if err := SaveAPIKeyToKeyring("openai", string(data)); err != nil {
			t.Skipf("keyring not available: %v", err)
		}
		defer DeleteAPIKeyFromKeyring("openai")

		token, err := refreshCodexToken()
		require.NoError(t, err)
		assert.Equal(t, newJWT, token)

		// Verify the keyring was updated with the new credential
		raw, err := GetAPIKeyFromKeyring("openai")
		require.NoError(t, err)
		updatedCred, ok := parseCodexOAuthCredential(raw)
		require.True(t, ok)
		assert.Equal(t, newJWT, updatedCred.AccessToken)
		assert.Equal(t, "new-refresh-token", updatedCred.RefreshToken)
		assert.Equal(t, "org-refreshed", updatedCred.AccountID)
		assert.Greater(t, updatedCred.ExpiresAt, time.Now().Unix())
	})

	t.Run("non-expired token returns cached without HTTP call", func(t *testing.T) {
		cachedCred := codexOAuthCredential{
			AccessToken:  "cached-token",
			RefreshToken: "rt",
			ExpiresAt:    9999999999, // far future
			AccountID:    "org-cached",
		}
		data, _ := json.Marshal(cachedCred)

		if err := SaveAPIKeyToKeyring("openai", string(data)); err != nil {
			t.Skipf("keyring not available: %v", err)
		}
		defer DeleteAPIKeyFromKeyring("openai")

		token, err := refreshCodexToken()
		require.NoError(t, err)
		assert.Equal(t, "cached-token", token)
	})

	t.Run("refresh response with empty refresh_token falls back to old", func(t *testing.T) {
		srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := codexTokenResponse{
				AccessToken: makeTestJWT("org-fb"),
				ExpiresIn:   3600,
				// RefreshToken intentionally empty
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		}))
		defer srv2.Close()

		savedURL := codexTokenURL
		codexTokenURL = srv2.URL
		defer func() { codexTokenURL = savedURL }()

		oldCred := codexOAuthCredential{
			AccessToken:  "expired",
			RefreshToken: "original-rt",
			ExpiresAt:    1,
			AccountID:    "org-old",
		}
		data, _ := json.Marshal(oldCred)

		if err := SaveAPIKeyToKeyring("openai", string(data)); err != nil {
			t.Skipf("keyring not available: %v", err)
		}
		defer DeleteAPIKeyFromKeyring("openai")

		token, err := refreshCodexToken()
		require.NoError(t, err)
		assert.NotEqual(t, "expired", token)

		// Verify the updated credential retains the old refresh token
		raw, err := GetAPIKeyFromKeyring("openai")
		require.NoError(t, err)
		updatedCred, ok := parseCodexOAuthCredential(raw)
		require.True(t, ok)
		assert.Equal(t, "original-rt", updatedCred.RefreshToken)
	})

	t.Run("no credential in keyring — returns error", func(t *testing.T) {
		_ = DeleteAPIKeyFromKeyring("openai")

		_, err := refreshCodexToken()
		assert.Error(t, err)
	})

	t.Run("plain API key in keyring — returns error", func(t *testing.T) {
		if err := SaveAPIKeyToKeyring("openai", "sk-plain-key"); err != nil {
			t.Skipf("keyring not available: %v", err)
		}
		defer DeleteAPIKeyFromKeyring("openai")

		_, err := refreshCodexToken()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no OAuth credential")
	})
}

// TestGetEffectiveAPIKeyKeyring tests getEffectiveAPIKey with a real keyring.
// Requires ASIMI_TEST_KEYRING=1.
func TestGetEffectiveAPIKeyKeyring(t *testing.T) {
	if os.Getenv("ASIMI_TEST_KEYRING") != "1" {
		t.Skip("Skipping keyring integration tests. Set ASIMI_TEST_KEYRING=1 to run.")
	}

	t.Run("env var takes precedence", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "sk-env-key")
		_ = SaveAPIKeyToKeyring("openai", "sk-keyring-key")

		key := getEffectiveAPIKey("openai")
		assert.Equal(t, "sk-env-key", key)

		t.Setenv("OPENAI_API_KEY", "")
		_ = DeleteAPIKeyFromKeyring("openai")
	})

	t.Run("plain keyring key", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "")
		_ = SaveAPIKeyToKeyring("openai", "sk-plain-key")

		key := getEffectiveAPIKey("openai")
		assert.Equal(t, "sk-plain-key", key)

		_ = DeleteAPIKeyFromKeyring("openai")
	})

	t.Run("non-expired OAuth credential", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "")
		cred := codexOAuthCredential{
			AccessToken:  "oauth-access-token",
			RefreshToken: "oauth-refresh-token",
			ExpiresAt:    9999999999,
			AccountID:    "org-test",
		}
		data, _ := json.Marshal(cred)
		_ = SaveAPIKeyToKeyring("openai", string(data))

		key := getEffectiveAPIKey("openai")
		assert.Equal(t, "oauth-access-token", key)

		_ = DeleteAPIKeyFromKeyring("openai")
	})
}

// TestCheckProviderAuthOAuth tests that checkProviderAuth recognizes OAuth
// JSON credentials in the keyring for the openai provider.
// Requires ASIMI_TEST_KEYRING=1.
func TestCheckProviderAuthOAuth(t *testing.T) {
	if os.Getenv("ASIMI_TEST_KEYRING") != "1" {
		t.Skip("Skipping keyring integration tests. Set ASIMI_TEST_KEYRING=1 to run.")
	}

	t.Setenv("OPENAI_API_KEY", "")

	cred := codexOAuthCredential{
		AccessToken:  "oauth-access-token",
		RefreshToken: "oauth-refresh-token",
		ExpiresAt:    9999999999,
		AccountID:    "org-test",
	}
	data, _ := json.Marshal(cred)
	require.NoError(t, SaveAPIKeyToKeyring("openai", string(data)))

	assert.True(t, checkProviderAuth("openai"))

	_ = DeleteAPIKeyFromKeyring("openai")
}

// TestGetCodexAccountIDKeyring tests extracting the account ID from the keyring
// credential for setContextParams. Requires ASIMI_TEST_KEYRING=1.
func TestGetCodexAccountIDKeyring(t *testing.T) {
	if os.Getenv("ASIMI_TEST_KEYRING") != "1" {
		t.Skip("Skipping keyring integration tests. Set ASIMI_TEST_KEYRING=1 to run.")
	}

	t.Setenv("OPENAI_API_KEY", "")

	t.Run("OAuth credential with account ID", func(t *testing.T) {
		cred := codexOAuthCredential{
			AccessToken:  "token",
			RefreshToken: "rt",
			ExpiresAt:    9999999999,
			AccountID:    "org-account-123",
		}
		data, _ := json.Marshal(cred)
		_ = SaveAPIKeyToKeyring("openai", string(data))

		assert.Equal(t, "org-account-123", getCodexAccountID())

		_ = DeleteAPIKeyFromKeyring("openai")
	})

	t.Run("plain API key — no account ID", func(t *testing.T) {
		_ = SaveAPIKeyToKeyring("openai", "sk-plain")
		assert.Equal(t, "", getCodexAccountID())
		_ = DeleteAPIKeyFromKeyring("openai")
	})

	t.Run("env var set — no account ID", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "sk-env")
		cred := codexOAuthCredential{
			AccessToken: "token",
			AccountID:   "org-x",
		}
		data, _ := json.Marshal(cred)
		_ = SaveAPIKeyToKeyring("openai", string(data))

		assert.Equal(t, "", getCodexAccountID())

		t.Setenv("OPENAI_API_KEY", "")
		_ = DeleteAPIKeyFromKeyring("openai")
	})
}
