package tools

import (
	"bufio"
	"fmt"
	"strings"
)

// FileSummary represents a summary of any file
type FileSummary struct {
	TotalLines   int      `json:"total_lines"`
	FirstLines   []string `json:"first_lines"`
	LastLines    []string `json:"last_lines"`
	LineCount    int      `json:"line_count"`
	FirstN       int      `json:"first_n"`
	LastN        int      `json:"last_n"`
	IsTruncated  bool     `json:"is_truncated"`
	FileType     string   `json:"file_type"`
}

// SummarizeFile creates a summary of a file by showing first and last N lines
func SummarizeFile(content string, firstN, lastN int) *FileSummary {
	summary := &FileSummary{
		FirstLines:  []string{},
		LastLines:   []string{},
		FirstN:      firstN,
		LastN:       lastN,
		FileType:    "generic",
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	lines := []string{}
	
	// Read all lines
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	
	summary.TotalLines = len(lines)
	summary.LineCount = summary.TotalLines
	
	// Check if file needs truncation
	totalToShow := firstN + lastN
	if summary.TotalLines > totalToShow {
		summary.IsTruncated = true
	} else {
		summary.IsTruncated = false
	}
	
	// Get first N lines, or all lines if not truncated
	var endFirst int
	if summary.IsTruncated {
		endFirst = firstN
		if endFirst > summary.TotalLines {
			endFirst = summary.TotalLines
		}
		summary.FirstLines = lines[:endFirst]
	} else {
		// Show all lines if not truncated
		summary.FirstLines = lines
		endFirst = summary.TotalLines // All lines are "first" lines
	}
	
	// Get last N lines only if file is truncated
	if summary.IsTruncated {
		startLast := summary.TotalLines - lastN
		if startLast < 0 {
			startLast = 0
		}
		// Avoid overlap with first lines
		if startLast < endFirst {
			startLast = endFirst
		}
		// Only add last lines if there are any after the overlap adjustment
		if startLast < summary.TotalLines {
			summary.LastLines = lines[startLast:]
		}
	}
	
	return summary
}

// SummarizeTextFile is a convenience function that uses default values
func SummarizeTextFile(content string) *FileSummary {
	// Default: show first 50 and last 50 lines
	return SummarizeFile(content, 50, 50)
}

// FormatFileSummary formats a file summary as a string
func FormatFileSummary(summary *FileSummary) string {
	var sb strings.Builder
	
	sb.WriteString(fmt.Sprintf("File type: %s\n", summary.FileType))
	sb.WriteString(fmt.Sprintf("Total lines: %d\n", summary.TotalLines))
	
	if summary.IsTruncated {
		sb.WriteString(fmt.Sprintf("Showing first %d and last %d lines (file truncated)\n", 
			summary.FirstN, summary.LastN))
	} else {
		sb.WriteString("Showing complete file\n")
	}
	
	if len(summary.FirstLines) > 0 {
		sb.WriteString("\n=== First lines ===\n")
		for i, line := range summary.FirstLines {
			lineNum := i + 1
			sb.WriteString(fmt.Sprintf("%6d: %s\n", lineNum, line))
		}
	}
	
	if len(summary.LastLines) > 0 && summary.IsTruncated {
		sb.WriteString(fmt.Sprintf("\n=== Last %d lines ===\n", len(summary.LastLines)))
		for i, line := range summary.LastLines {
			lineNum := summary.TotalLines - len(summary.LastLines) + i + 1
			sb.WriteString(fmt.Sprintf("%6d: %s\n", lineNum, line))
		}
	}
	
	// Add separator if file was truncated
	if summary.IsTruncated {
		linesOmitted := summary.TotalLines - len(summary.FirstLines) - len(summary.LastLines)
		if linesOmitted > 0 {
			sb.WriteString(fmt.Sprintf("\n... %d lines omitted ...\n", linesOmitted))
		}
	}
	
	return sb.String()
}

// DetectFileType attempts to detect the file type based on extension or content
func DetectFileType(filename, content string) string {
	// Check for common file extensions
	lowerFilename := strings.ToLower(filename)
	
	switch {
	case strings.HasSuffix(lowerFilename, ".md"), strings.HasSuffix(lowerFilename, ".markdown"):
		return "markdown"
	case strings.HasSuffix(lowerFilename, ".json"):
		return "json"
	case strings.HasSuffix(lowerFilename, ".yaml"), strings.HasSuffix(lowerFilename, ".yml"):
		return "yaml"
	case strings.HasSuffix(lowerFilename, ".toml"):
		return "toml"
	case strings.HasSuffix(lowerFilename, ".xml"):
		return "xml"
	case strings.HasSuffix(lowerFilename, ".html"), strings.HasSuffix(lowerFilename, ".htm"):
		return "html"
	case strings.HasSuffix(lowerFilename, ".css"):
		return "css"
	case strings.HasSuffix(lowerFilename, ".js"):
		return "javascript"
	case strings.HasSuffix(lowerFilename, ".ts"):
		return "typescript"
	case strings.HasSuffix(lowerFilename, ".py"):
		return "python"
	case strings.HasSuffix(lowerFilename, ".rb"):
		return "ruby"
	case strings.HasSuffix(lowerFilename, ".java"):
		return "java"
	case strings.HasSuffix(lowerFilename, ".c"):
		return "c"
	case strings.HasSuffix(lowerFilename, ".cpp"), strings.HasSuffix(lowerFilename, ".cc"), strings.HasSuffix(lowerFilename, ".cxx"):
		return "cpp"
	case strings.HasSuffix(lowerFilename, ".h"), strings.HasSuffix(lowerFilename, ".hpp"):
		return "header"
	case strings.HasSuffix(lowerFilename, ".rs"):
		return "rust"
	case strings.HasSuffix(lowerFilename, ".php"):
		return "php"
	case strings.HasSuffix(lowerFilename, ".sh"), strings.HasSuffix(lowerFilename, ".bash"):
		return "shell"
	case strings.HasSuffix(lowerFilename, ".txt"):
		return "text"
	case strings.HasSuffix(lowerFilename, ".csv"):
		return "csv"
	case strings.HasSuffix(lowerFilename, ".sql"):
		return "sql"
	case strings.HasSuffix(lowerFilename, ".dockerfile"), strings.HasSuffix(lowerFilename, "dockerfile"):
		return "dockerfile"
	case strings.HasSuffix(lowerFilename, ".makefile"), strings.HasSuffix(lowerFilename, "makefile"):
		return "makefile"
	case strings.HasSuffix(lowerFilename, ".gitignore"):
		return "gitignore"
	case strings.HasSuffix(lowerFilename, ".env"):
		return "env"
	case strings.HasSuffix(lowerFilename, ".go"):
		return "go"
	default:
		// Try to detect from content
		detected := detectFileTypeFromContent(content)
		if detected == "unknown" && strings.TrimSpace(content) == "" {
			// Empty .txt files should still be text
			if strings.HasSuffix(lowerFilename, ".txt") {
				return "text"
			}
		}
		return detected
	}
}

// detectFileTypeFromContent attempts to detect file type from content
func detectFileTypeFromContent(content string) string {
	if strings.TrimSpace(content) == "" {
		return "unknown"
	}
	
	// Check for shebang
	if strings.HasPrefix(content, "#!") {
		return "script"
	}
	
	// Check for XML declaration
	if strings.HasPrefix(strings.TrimSpace(content), "<?xml") {
		return "xml"
	}
	
	// Check for JSON (starts with { or [)
	trimmed := strings.TrimSpace(content)
	if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
	   (strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
		return "json"
	}
	
	// Check for YAML (common patterns)
	lines := strings.Split(content, "\n")
	yamlIndicators := 0
	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		// YAML key-value pairs with colon followed by space
		if strings.Contains(trimmedLine, ": ") && !strings.HasPrefix(trimmedLine, "#") {
			yamlIndicators++
		}
		// YAML list items
		if strings.HasPrefix(trimmedLine, "- ") {
			yamlIndicators++
		}
		if yamlIndicators > 1 {
			return "yaml"
		}
	}
	
	// Default to text
	return "text"
}

// CreateFileSummary creates an appropriate summary based on file type
func CreateFileSummary(filename, content string) *FileSummary {
	// For other file types, use generic summarizer
	summary := SummarizeTextFile(content)
	return summary
}

// FormatSummaryForDisplay formats a file summary for display in read_file tool
func FormatSummaryForDisplay(filename string, fileSize int64, summary *FileSummary) string {
	var sb strings.Builder
	
	sb.WriteString(fmt.Sprintf("File: %s\n", filename))
	sb.WriteString(fmt.Sprintf("Size: %d bytes\n", fileSize))
	sb.WriteString(fmt.Sprintf("Type: %s\n", summary.FileType))
	sb.WriteString(fmt.Sprintf("Lines: %d\n", summary.TotalLines))
	
	if summary.IsTruncated {
		sb.WriteString("\n⚠️  File is large. Showing summary only.\n")
		sb.WriteString("Use offset/limit parameters to read specific sections.\n\n")
	}
	
	sb.WriteString(FormatFileSummary(summary))
	
	return sb.String()
}
