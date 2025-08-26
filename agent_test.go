package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms/fake"
	"github.com/tmc/langchaingo/tools"
)

func TestReadFileTool(t *testing.T) {
	// Create a test file
	testFilePath := filepath.Join("testdata", "test_read.txt")
	testContent := "This is test content for reading."
	err := os.WriteFile(testFilePath, []byte(testContent), 0644)
	require.NoError(t, err)
	defer os.Remove(testFilePath) // Clean up

	// Create the tool
	tool := ReadFileTool{}

	// Create input JSON
	input := ReadFileInput{Path: testFilePath}
	inputJSON, err := json.Marshal(input)
	require.NoError(t, err)

	// Call the tool
	result, err := tool.Call(context.Background(), string(inputJSON))
	require.NoError(t, err)
	assert.Equal(t, testContent, result)
}

func TestWriteFileTool(t *testing.T) {
	// Create the tool
	tool := WriteFileTool{}

	// Create input JSON
	testFilePath := filepath.Join("testdata", "test_write.txt")
	testContent := "This is test content for writing."
	input := WriteFileInput{
		Path:    testFilePath,
		Content: testContent,
	}
	inputJSON, err := json.Marshal(input)
	require.NoError(t, err)

	// Call the tool
	result, err := tool.Call(context.Background(), string(inputJSON))
	require.NoError(t, err)
	assert.Contains(t, result, "Successfully wrote to")

	// Verify the file was created with correct content
	content, err := os.ReadFile(testFilePath)
	require.NoError(t, err)
	assert.Equal(t, testContent, string(content))

	// Clean up
	defer os.Remove(testFilePath)
}

func TestListDirectoryTool(t *testing.T) {
	// Create the tool
	tool := ListDirectoryTool{}

	// Create input JSON
	input := ListDirectoryInput{Path: "testdata"}
	inputJSON, err := json.Marshal(input)
	require.NoError(t, err)

	// Call the tool
	result, err := tool.Call(context.Background(), string(inputJSON))
	require.NoError(t, err)
	assert.Contains(t, result, "test.txt")
}

func TestReplaceTextTool(t *testing.T) {
	// Create a test file
	testFilePath := filepath.Join("testdata", "test_replace.txt")
	testContent := "This is the original text.\nWe will replace the word original.\n"
	err := os.WriteFile(testFilePath, []byte(testContent), 0644)
	require.NoError(t, err)
	defer os.Remove(testFilePath) // Clean up

	// Create the tool
	tool := ReplaceTextTool{}

	// Create input JSON
	input := ReplaceTextInput{
		Path:    testFilePath,
		OldText: "original",
		NewText: "modified",
	}
	inputJSON, err := json.Marshal(input)
	require.NoError(t, err)

	// Call the tool
	result, err := tool.Call(context.Background(), string(inputJSON))
	require.NoError(t, err)
	assert.Contains(t, result, "Successfully replaced text")

	// Verify the file was modified correctly
	content, err := os.ReadFile(testFilePath)
	require.NoError(t, err)
	expectedContent := "This is the modified text.\nWe will replace the word modified.\n"
	assert.Equal(t, expectedContent, string(content))
}

func TestAgentWithFakeLLM(t *testing.T) {
	// Create a fake LLM with predefined responses
	// This simulates how the LLM would respond when using our tools
	fakeLLM := fake.NewFakeLLM([]string{
		"Action: read_file\nAction Input: {\"path\": \"testdata/test.txt\"}",
		"Action: write_file\nAction Input: {\"path\": \"testdata/output.txt\", \"content\": \"Hello, World!\"}",
		"Final Answer: I have read the test file and created an output file.",
	})

	// Create tools for the agent
	agentTools := []tools.Tool{
		ReadFileTool{},
		WriteFileTool{},
		ListDirectoryTool{},
		ReplaceTextTool{},
	}

	// Create the agent
	agent := CreateAgent(fakeLLM, agentTools)

	// Create the executor
	executor := CreateExecutor(agent)

	// Execute the agent with a test prompt
	ctx := context.Background()
	result, err := executor.Call(ctx, map[string]any{
		"input": "Read the test file and create an output file with 'Hello, World!'",
	})
	require.NoError(t, err)

	// Check the result
	output, ok := result["output"]
	require.True(t, ok)
	assert.Equal(t, " I have read the test file and created an output file.", output)

	// Verify the output file was created
	outputFilePath := filepath.Join("testdata", "output.txt")
	content, err := os.ReadFile(outputFilePath)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", string(content))

	// Clean up
	defer os.Remove(outputFilePath)
}