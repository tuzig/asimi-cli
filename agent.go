package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tmc/langchaingo/agents"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/tools"
)

// ReadFileInput represents the input for reading a file
type ReadFileInput struct {
	Path string `json:"path"`
}

// ReadFileTool is a tool for reading files
type ReadFileTool struct{}

// Name returns the name of the tool
func (r ReadFileTool) Name() string {
	return "read_file"
}

// Description returns a description of what the tool does
func (r ReadFileTool) Description() string {
	return "Useful for reading the contents of a file. Input should be a JSON string with the path to the file.\n\tExample: {\"path\": \"file.txt\"}"
}

// Call executes the file read operation
func (r ReadFileTool) Call(ctx context.Context, input string) (string, error) {
	var readFileInput ReadFileInput
	err := json.Unmarshal([]byte(input), &readFileInput)
	if err != nil {
		return "", fmt.Errorf("invalid input format: %w", err)
	}

	content, err := os.ReadFile(readFileInput.Path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	return string(content), nil
}

// WriteFileInput represents the input for writing a file
type WriteFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// WriteFileTool is a tool for writing files
type WriteFileTool struct{}

// Name returns the name of the tool
func (w WriteFileTool) Name() string {
	return "write_file"
}

// Description returns a description of what the tool does
func (w WriteFileTool) Description() string {
	return "Useful for writing content to a file. Input should be a JSON string with the path and content.\n\tExample: {\"path\": \"file.txt\", \"content\": \"Hello World\"}"
}

// Call executes the file write operation
func (w WriteFileTool) Call(ctx context.Context, input string) (string, error) {
	var writeFileInput WriteFileInput
	err := json.Unmarshal([]byte(input), &writeFileInput)
	if err != nil {
		return "", fmt.Errorf("invalid input format: %w", err)
	}

	err = os.WriteFile(writeFileInput.Path, []byte(writeFileInput.Content), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	return fmt.Sprintf("Successfully wrote to %s", writeFileInput.Path), nil
}

// ListDirectoryInput represents the input for listing a directory
type ListDirectoryInput struct {
	Path string `json:"path"`
}

// ListDirectoryTool is a tool for listing directory contents
type ListDirectoryTool struct{}

// Name returns the name of the tool
func (l ListDirectoryTool) Name() string {
	return "list_directory"
}

// Description returns a description of what the tool does
func (l ListDirectoryTool) Description() string {
	return "Useful for listing the contents of a directory. Input should be a JSON string with the path to the directory.\n\tExample: {\"path\": \"./\"}"
}

// Call executes the directory listing operation
func (l ListDirectoryTool) Call(ctx context.Context, input string) (string, error) {
	var listDirInput ListDirectoryInput
	err := json.Unmarshal([]byte(input), &listDirInput)
	if err != nil {
		return "", fmt.Errorf("invalid input format: %w", err)
	}

	entries, err := os.ReadDir(listDirInput.Path)
	if err != nil {
		return "", fmt.Errorf("failed to list directory: %w", err)
	}

	var result string
	for _, entry := range entries {
		if entry.IsDir() {
			result += fmt.Sprintf("[DIR] %s\n", entry.Name())
		} else {
			result += fmt.Sprintf("[FILE] %s\n", entry.Name())
		}
	}
	return result, nil
}

// ReplaceTextInput represents the input for replacing text in a file
type ReplaceTextInput struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

// ReplaceTextTool is a tool for replacing text in a file
type ReplaceTextTool struct{}

// Name returns the name of the tool
func (r ReplaceTextTool) Name() string {
	return "replace_text"
}

// Description returns a description of what the tool does
func (r ReplaceTextTool) Description() string {
	return "Useful for replacing text in a file. Input should be a JSON string with the path and text to replace.\n\tExample: {\"path\": \"file.txt\", \"old_text\": \"old content\", \"new_text\": \"new content\"}"
}

// Call executes the text replacement operation
func (r ReplaceTextTool) Call(ctx context.Context, input string) (string, error) {
	var replaceInput ReplaceTextInput
	err := json.Unmarshal([]byte(input), &replaceInput)
	if err != nil {
		return "", fmt.Errorf("invalid input format: %w", err)
	}

	// Read the file
	content, err := os.ReadFile(replaceInput.Path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Replace the text
	newContent := strings.ReplaceAll(string(content), replaceInput.OldText, replaceInput.NewText)

	// Write the file back
	err = os.WriteFile(replaceInput.Path, []byte(newContent), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return fmt.Sprintf("Successfully replaced text in %s", replaceInput.Path), nil
}

// CreateAgent creates and returns a new agent with the specified LLM and tools
func CreateAgent(llm llms.Model, tools []tools.Tool) agents.Agent {
	return agents.NewOneShotAgent(llm, tools)
}

// CreateExecutor creates and returns a new executor for the agent
func CreateExecutor(agent agents.Agent) *agents.Executor {
	return agents.NewExecutor(agent, agents.WithMaxIterations(5))
}