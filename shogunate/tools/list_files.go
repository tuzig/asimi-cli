package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/afittestide/asimi/internal/utils"
)

// ListDirectoryInput is the input for the ListDirectoryTool
type ListDirectoryInput struct {
	Path string `json:"path"`
}

// ListDirectoryTool is a tool for listing directory contents
type ListDirectoryTool struct{}

func (t ListDirectoryTool) Name() string {
	return "list_files"
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
	params.Path = strings.Trim(params.Path, `"'`)

	// If the path is empty, use the current directory
	if params.Path == "" {
		params.Path = "."
	}

	if err := ValidatePathWithinProject(params.Path); err != nil {
		return "", err
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

func (t ListDirectoryTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Directory path (defaults to '.')",
			},
		},
	}
}

// Format formats a list_files tool call for display
func (t ListDirectoryTool) Format(input, result string, err error) string {
	var params ListDirectoryInput
	json.Unmarshal([]byte(input), &params)

	path := params.Path
	if path == "" {
		path = "."
	}

	msg := utils.NewMsgBlockBuilder("List Files ")
	msg.WriteLn(path)

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		files := strings.Split(strings.TrimSpace(result), "\n")
		if result == "" {
			files = []string{}
		}
		msg.Writef("Found %d items", len(files))
	}

	return msg.String() + "\n"
}
