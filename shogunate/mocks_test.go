package shogunate

import (
	"testing"

	"github.com/afittestide/asimi/internal/mocks"
	"github.com/stretchr/testify/require"
)

// TestMockLLMBridge_ImplementsLLMProvider verifies that MockLLMBridge implements
// the LLMProvider interface at compile time. This test exists in shogunate package
// to avoid import cycles between shogunate and internal/mocks.
func TestMockLLMBridge_ImplementsLLMProvider(t *testing.T) {
	var provider LLMProvider
	provider = mocks.NewLLMProvider()
	require.NotNil(t, provider)
}
