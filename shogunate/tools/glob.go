package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/afittestide/asimi/internal/utils"
	"github.com/yargevad/filepathx"
)

// GlobInput is the input for the GlobTool
type GlobInput struct {
	Pattern string `json:"pattern"`
}

// GlobTool finds files by glob pattern
type GlobTool struct{}

func (t GlobTool) Name() string {
	return "glob"
}

func (t GlobTool) Description() string {
	return "Finds files by glob pattern. Supports ** for recursive matching (e.g. **/*.go). Returns matching file paths, one per line."
}

func (t GlobTool) Call(ctx context.Context, input string) (string, error) {
	var params GlobInput
	if err := json.Unmarshal([]byte(input), &params); err != nil {
		params.Pattern = strings.Trim(input, `"'`)
	}

	if params.Pattern == "" {
		params.Pattern = "*"
	}

	matches, err := filepathx.Glob(params.Pattern)
	if err != nil {
		return "", fmt.Errorf("invalid glob pattern: %w", err)
	}

	var results []string
	maxResults := 200
	for _, path := range matches {
		if err := ValidatePathWithinProject(path); err != nil {
			continue
		}
		results = append(results, path)
		if len(results) >= maxResults {
			break
		}
	}

	if len(results) == 0 {
		return "No matches found", nil
	}

	output := strings.Join(results, "\n")
	if len(results) >= maxResults {
		output += fmt.Sprintf("\n... (truncated at %d results)", maxResults)
	}
	return output, nil
}

func (t GlobTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "Glob pattern to match files (e.g. **/*.go, src/*.ts). Defaults to * (current directory).",
			},
		},
		"required": []string{"pattern"},
	}
}

// Format formats a glob tool call for display
func (t GlobTool) Format(input, result string, err error) string {
	var params GlobInput
	json.Unmarshal([]byte(input), &params)

	pattern := params.Pattern
	if pattern == "" {
		pattern = "*"
	}

	msg := utils.NewMsgBlockBuilder("Glob ")
	msg.WriteLn(pattern)

	if err != nil {
		msg.Writef("Error: %v", err)
	} else if result == "No matches found" {
		msg.WriteString("No matches found")
	} else {
		files := strings.Split(strings.TrimSpace(result), "\n")
		msg.Writef("Found %d files", len(files))
	}

	return msg.String() + "\n"
}
