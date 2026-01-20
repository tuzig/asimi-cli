package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"
)

// textMessage creates a dummy text message for testing
func textMessage(role llms.ChatMessageType, text string) llms.MessageContent {
	return llms.MessageContent{
		Role: role,
		Parts: []llms.ContentPart{
			llms.TextContent{Text: text},
		},
	}
}

// repoInfoWithProjectRoot creates a RepoInfo with the current working directory as project root.
func repoInfoWithProjectRoot(t *testing.T) RepoInfo {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	return MakeRepoInfo(cwd, "")
}
