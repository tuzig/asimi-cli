package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tmc/langchaingo/tools"
)

// ReadFileInput is the input for the ReadFileTool
type ReadFileInput struct {
	Path string `json:"path"`
}

// ReadFileTool is a tool for reading files
type ReadFileTool struct{}

func (t ReadFileTool) Name() string {
	return "read_file"
}

func (t ReadFileTool) Description() string {
	return "Reads a file and returns its content. The input should be a JSON object with a 'path' field."
}

func (t ReadFileTool) Call(ctx context.Context, input string) (string, error) {
	var params ReadFileInput
	err := json.Unmarshal([]byte(input), &params)
	if err != nil {
		// If unmarshalling fails, assume the input is a raw path
		params.Path = input
	}

	// Clean up the path to remove any surrounding quotes
	params.Path = strings.Trim(params.Path, `"'`)

	content, err := os.ReadFile(params.Path)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// WriteFileInput is the input for the WriteFileTool
type WriteFileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// WriteFileTool is a tool for writing to files
type WriteFileTool struct{}

func (t WriteFileTool) Name() string {
	return "write_file"
}

func (t WriteFileTool) Description() string {
	return "Writes content to a file. The input should be a JSON object with 'path' and 'content' fields."
}

func (t WriteFileTool) Call(ctx context.Context, input string) (string, error) {
	var params WriteFileInput
	err := json.Unmarshal([]byte(input), &params)
	if err != nil {
		return "", fmt.Errorf("invalid input: %w. The input should be a JSON object with 'path' and 'content' fields", err)
	}

	// Clean up path and content
	params.Path = strings.Trim(params.Path, `"'`)
	params.Content = strings.Trim(params.Content, `"'`)

	err = os.WriteFile(params.Path, []byte(params.Content), 0644)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Successfully wrote to %s", params.Path), nil
}

// ListDirectoryInput is the input for the ListDirectoryTool
type ListDirectoryInput struct {
	Path string `json:"path"`
}

// ListDirectoryTool is a tool for listing directory contents
type ListDirectoryTool struct{}

func (t ListDirectoryTool) Name() string {
	return "list_directory"
}

func (t ListDirectoryTool) Description() string {
	return "Lists the contents of a directory. The input should be a JSON object with a 'path' field."
}

func (t ListDirectoryTool) Call(ctx context.Context, input string) (string, error) {
	var params ListDirectoryInput
	err := json.Unmarshal([]byte(input), &params)
	if err != nil {
		// If unmarshalling fails, assume the input is a raw path
		params.Path = input
	}

	// Clean up the path to remove any surrounding quotes
	params.Path = strings.Trim(params.Path, `"'`) // Corrected: escaped the double quote within the backticks

	// If the path is empty, use the current directory
	if params.Path == "" {
		params.Path = "."
	}

	files, err := os.ReadDir(params.Path)
	if err != nil {
		return "", err
	}

	var fileNames []string
	for _, file := range files {
		fileNames = append(fileNames, file.Name())
	}
	return strings.Join(fileNames, "\n"), nil
}

// ReplaceTextInput is the input for the ReplaceTextTool
type ReplaceTextInput struct {
	Path    string `json:"path"`
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

// ReplaceTextTool is a tool for replacing text in a file
type ReplaceTextTool struct{}

func (t ReplaceTextTool) Name() string {
	return "replace_text"
}

func (t ReplaceTextTool) Description() string {
	return "Replaces all occurrences of a string in a file with another string. The input should be a JSON object with 'path', 'old_text', and 'new_text' fields."
}

func (t ReplaceTextTool) Call(ctx context.Context, input string) (string, error) {
	var params ReplaceTextInput
	err := json.Unmarshal([]byte(input), &params)
	if err != nil {
		return "", fmt.Errorf("invalid input: %w. The input should be a JSON object with 'path', 'old_text', and 'new_text' fields", err)
	}

	content, err := os.ReadFile(params.Path)
	if err != nil {
		return "", err
	}

	newContent := strings.ReplaceAll(string(content), params.OldText, params.NewText)

	err = os.WriteFile(params.Path, []byte(newContent), 0644)
	if err != nil {
		return "", err
	}
	return "Successfully replaced text", nil
}

var _ tools.Tool = ReadFileTool{}
var _ tools.Tool = WriteFileTool{}
var _ tools.Tool = ListDirectoryTool{}
var _ tools.Tool = ReplaceTextTool{}
