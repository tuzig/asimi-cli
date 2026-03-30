package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/afittestide/asimi/internal/config"
	"github.com/afittestide/asimi/internal/utils"
)

// ReadFileInput is the input for the ReadFileTool
type ReadFileInput struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// readFileInputRaw is used to handle string values for numeric fields (workaround for Claude Code CLI)
type readFileInputRaw struct {
	Path   string `json:"path"`
	Offset any    `json:"offset,omitempty"`
	Limit  any    `json:"limit,omitempty"`
}

// ReadFileTool is a tool for reading files
type ReadFileTool struct {
	maxFileSize int // Maximum file size to read fully (bytes)
}

// NewReadFileTool creates a new ReadFileTool with configuration
func NewReadFileTool(config config.LLMConfig) *ReadFileTool {
	return &ReadFileTool{
		maxFileSize: config.MaxToolOutput,
	}
}

func (t ReadFileTool) Name() string {
	return "read_file"
}

func (t ReadFileTool) Description() string {
	return "Reads a file and returns its content. The input should be a JSON object with a 'path' field. Optionally specify 'offset' (line number to start from, 1-based) and 'limit' (number of lines to read)."
}

func (t ReadFileTool) Call(ctx context.Context, input string) (string, error) {
	var params ReadFileInput
	err := json.Unmarshal([]byte(input), &params)
	if err != nil {
		// If unmarshalling fails, assume the input is a raw path
		params.Path = input
	}

	// Workaround for Claude Code CLI bug: numeric params come as strings
	// If offset/limit are zero but the input contains them, try flexible parsing
	if (params.Offset == 0 && strings.Contains(input, `"offset"`)) ||
		(params.Limit == 0 && strings.Contains(input, `"limit"`)) {
		var rawParams readFileInputRaw
		if json.Unmarshal([]byte(input), &rawParams) == nil {
			params.Path = rawParams.Path
			if s, ok := rawParams.Offset.(string); ok && s != "" {
				fmt.Sscanf(s, "%d", &params.Offset)
			}
			if s, ok := rawParams.Limit.(string); ok && s != "" {
				fmt.Sscanf(s, "%d", &params.Limit)
			}
		}
	}

	// Clean up the path to remove any surrounding quotes
	params.Path = strings.Trim(params.Path, `"'`)

	if err := ValidatePathWithinProject(params.Path); err != nil {
		return "", err
	}

	// Get file info to check size
	fileInfo, err := os.Stat(params.Path)
	if err != nil {
		return "", err
	}

	fileSize := fileInfo.Size()
	
	// Check if file is too large
	isLargeFile := t.maxFileSize > 0 && fileSize > int64(t.maxFileSize)
	
	// If file is large and no offset/limit specified, return summary instead of full content
	if isLargeFile && params.Offset == 0 && params.Limit == 0 {
		// File is large and no specific section requested - return summary
		return t.handleLargeFile(params.Path, fileSize)
	}

	// Read file content
	content, err := os.ReadFile(params.Path)
	if err != nil {
		return "", err
	}

	contentStr := string(content)
	lines := strings.Split(contentStr, "\n")
	totalLines := len(lines)

	// Handle offset (1-based, convert to 0-based)
	startLine := 0
	if params.Offset > 0 {
		startLine = params.Offset - 1
		if startLine >= totalLines {
			// Offset beyond file end - still return file metadata if large
			if isLargeFile {
				return t.formatFileMetadata(params.Path, fileSize, totalLines) + "\n(Offset beyond file end)", nil
			}
			return "", nil // Offset beyond file end
		}
	}

	// Handle limit
	endLine := totalLines
	if params.Limit > 0 {
		endLine = startLine + params.Limit
		if endLine > totalLines {
			endLine = totalLines
		}
	}

	selectedLines := lines[startLine:endLine]
	result := strings.Join(selectedLines, "\n")
	
	// If file is large, prepend metadata
	if isLargeFile {
		metadata := t.formatFileMetadata(params.Path, fileSize, totalLines)
		metadata += fmt.Sprintf("\n\nShowing lines %d-%d of %d:\n", startLine+1, endLine, totalLines)
		metadata += strings.Repeat("=", 50) + "\n"
		return metadata + result, nil
	}
	
	return result, nil
}

// handleLargeFile generates a summary for files that exceed the size limit
func (t ReadFileTool) handleLargeFile(filePath string, fileSize int64) (string, error) {
	// Read file content for summary
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	contentStr := string(content)
	
	// Create file summary using our summarizer
	summary := CreateFileSummary(filePath, contentStr)
	
	// Format the summary for display
	formattedSummary := FormatSummaryForDisplay(filePath, fileSize, summary)
	
	// Build the complete response with metadata
	var result strings.Builder
	result.WriteString(t.formatFileMetadata(filePath, fileSize, summary.TotalLines))
	result.WriteString("\n\n")
	result.WriteString("📋 File summary:\n")
	result.WriteString(strings.Repeat("=", 50) + "\n")
	result.WriteString(formattedSummary)
	
	return result.String(), nil
}

// formatFileMetadata creates a standardized metadata header for large files
func (t ReadFileTool) formatFileMetadata(filePath string, fileSize int64, totalLines int) string {
	var metadata strings.Builder
	
	metadata.WriteString(fmt.Sprintf("⚠️  File is large: %s\n", formatFileSize(fileSize)))
	metadata.WriteString(fmt.Sprintf("   Size: %d bytes (%.1f KB)\n", fileSize, float64(fileSize)/1024))
	metadata.WriteString(fmt.Sprintf("   Lines: %d\n", totalLines))
	metadata.WriteString(fmt.Sprintf("   Max size configured: %d bytes (%.1f KB)\n", t.maxFileSize, float64(t.maxFileSize)/1024))
	
	// Add file type if we can determine it
	fileType := "unknown"
	if content, err := readFirstBytes(filePath, 1024); err == nil {
		fileType = DetectFileType(filePath, string(content))
	}
	metadata.WriteString(fmt.Sprintf("   Type: %s\n", fileType))
	
	metadata.WriteString("\n💡 Tip: Use offset/limit parameters to read specific sections.")
	metadata.WriteString("\n      Example: {\"path\": \"file.txt\", \"offset\": 500, \"limit\": 100}")
	
	return metadata.String()
}

// formatFileSize formats file size in human-readable format
func formatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// readFirstBytes reads the first N bytes of a file for type detection
func readFirstBytes(filePath string, maxBytes int) ([]byte, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	
	buffer := make([]byte, maxBytes)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return nil, err
	}
	
	return buffer[:n], nil
}

func (t ReadFileTool) ParameterSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Absolute or relative path to the file",
			},
		},
		"required": []string{"path"},
	}
}

// Format formats a read_file tool call for display
func (t ReadFileTool) Format(input, result string, err error) string {
	var params ReadFileInput
	json.Unmarshal([]byte(input), &params)

	msg := utils.NewMsgBlockBuilder("Read File")
	if params.Path != "" {
		msg.Writef(" %s", params.Path)
	}
	msg.WriteLn()

	if err != nil {
		msg.Writef("Error: %v", err)
	} else {
		// Check if result contains file metadata (large file)
		if strings.Contains(result, "⚠️") || strings.Contains(result, "File is large") {
			// Try to extract line count from result
			lines := 0
			if strings.Contains(result, "Showing lines") {
				// Extract line range from "Showing lines X-Y of Z"
				linesStart := strings.Index(result, "Showing lines")
				if linesStart != -1 {
					linesEnd := strings.Index(result[linesStart:], "\n")
					if linesEnd != -1 {
						lineInfo := result[linesStart : linesStart+linesEnd]
						// Try to parse "Showing lines X-Y of Z"
						var start, end, total int
						if _, err := fmt.Sscanf(lineInfo, "Showing lines %d-%d of %d", &start, &end, &total); err == nil && end >= start {
							lines = end - start + 1
						}
					}
				}
			}
			
			if lines > 0 {
				msg.Writef("Read %d lines (file is large)", lines)
			} else if strings.Contains(result, "📋 File summary:") {
				msg.WriteString("Returned file summary (file is large)")
			} else {
				msg.WriteString("File is large")
			}
		} else {
			lines := strings.Count(result, "\n") + 1
			if result == "" {
				lines = 0
			}
			msg.Writef("Read %d lines", lines)
		}
	}

	return msg.String() + "\n"
}
