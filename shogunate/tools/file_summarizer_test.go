package tools

import (
	"fmt"
	"strings"
	"testing"
)

func TestSummarizeFile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		firstN   int
		lastN    int
		expected *FileSummary
	}{
		{
			name:    "small file, no truncation needed",
			content: "line1\nline2\nline3\nline4\nline5",
			firstN:  3,
			lastN:   3,
			expected: &FileSummary{
				TotalLines:   5,
				FirstLines:   []string{"line1", "line2", "line3", "line4", "line5"},
				LastLines:    []string{},
				LineCount:    5,
				FirstN:       3,
				LastN:        3,
				IsTruncated:  false,
				FileType:     "generic",
			},
		},
		{
			name:    "large file, needs truncation",
			content: strings.Join(generateLines(100), "\n"),
			firstN:  10,
			lastN:   10,
			expected: &FileSummary{
				TotalLines:  100,
				FirstLines:  generateLines(10),
				LastLines:   generateLines(10, 91), // lines 91-100
				LineCount:   100,
				FirstN:      10,
				LastN:       10,
				IsTruncated: true,
				FileType:    "generic",
			},
		},
		{
			name:    "exact fit for firstN + lastN",
			content: strings.Join(generateLines(20), "\n"),
			firstN:  10,
			lastN:   10,
			expected: &FileSummary{
				TotalLines:  20,
				FirstLines:  generateLines(20), // All lines since not truncated
				LastLines:   []string{}, // No last lines since all shown in first
				LineCount:   20,
				FirstN:      10,
				LastN:       10,
				IsTruncated: false,
				FileType:    "generic",
			},
		},
		{
			name:    "empty file",
			content: "",
			firstN:  10,
			lastN:   10,
			expected: &FileSummary{
				TotalLines:  0,
				FirstLines:  []string{},
				LastLines:   []string{},
				LineCount:   0,
				FirstN:      10,
				LastN:       10,
				IsTruncated: false,
				FileType:    "generic",
			},
		},
		{
			name:    "single line",
			content: "only one line",
			firstN:  10,
			lastN:   10,
			expected: &FileSummary{
				TotalLines:  1,
				FirstLines:  []string{"only one line"},
				LastLines:   []string{},
				LineCount:   1,
				FirstN:      10,
				LastN:       10,
				IsTruncated: false,
				FileType:    "generic",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := SummarizeFile(tt.content, tt.firstN, tt.lastN)

			// Check total lines
			if summary.TotalLines != tt.expected.TotalLines {
				t.Errorf("TotalLines = %d, want %d", summary.TotalLines, tt.expected.TotalLines)
			}

			// Check first lines count
			if len(summary.FirstLines) != len(tt.expected.FirstLines) {
				t.Errorf("FirstLines count = %d, want %d", len(summary.FirstLines), len(tt.expected.FirstLines))
			} else {
				for i, line := range summary.FirstLines {
					if line != tt.expected.FirstLines[i] {
						t.Errorf("FirstLines[%d] = %q, want %q", i, line, tt.expected.FirstLines[i])
					}
				}
			}

			// Check last lines count
			if len(summary.LastLines) != len(tt.expected.LastLines) {
				t.Errorf("LastLines count = %d, want %d", len(summary.LastLines), len(tt.expected.LastLines))
			} else {
				for i, line := range summary.LastLines {
					if line != tt.expected.LastLines[i] {
						t.Errorf("LastLines[%d] = %q, want %q", i, line, tt.expected.LastLines[i])
					}
				}
			}

			// Check other fields
			if summary.LineCount != tt.expected.LineCount {
				t.Errorf("LineCount = %d, want %d", summary.LineCount, tt.expected.LineCount)
			}
			if summary.FirstN != tt.expected.FirstN {
				t.Errorf("FirstN = %d, want %d", summary.FirstN, tt.expected.FirstN)
			}
			if summary.LastN != tt.expected.LastN {
				t.Errorf("LastN = %d, want %d", summary.LastN, tt.expected.LastN)
			}
			if summary.IsTruncated != tt.expected.IsTruncated {
				t.Errorf("IsTruncated = %v, want %v", summary.IsTruncated, tt.expected.IsTruncated)
			}
			if summary.FileType != tt.expected.FileType {
				t.Errorf("FileType = %q, want %q", summary.FileType, tt.expected.FileType)
			}
		})
	}
}

func TestSummarizeTextFile(t *testing.T) {
	// Generate 150 lines
	lines := generateLines(150)
	content := strings.Join(lines, "\n")
	
	summary := SummarizeTextFile(content)
	
	// Should show first 50 and last 50 lines (default)
	if summary.TotalLines != 150 {
		t.Errorf("TotalLines = %d, want 150", summary.TotalLines)
	}
	if !summary.IsTruncated {
		t.Error("IsTruncated = false, want true for 150-line file")
	}
	if len(summary.FirstLines) != 50 {
		t.Errorf("FirstLines count = %d, want 50", len(summary.FirstLines))
	}
	if len(summary.LastLines) != 50 {
		t.Errorf("LastLines count = %d, want 50", len(summary.LastLines))
	}
	if summary.FirstN != 50 {
		t.Errorf("FirstN = %d, want 50", summary.FirstN)
	}
	if summary.LastN != 50 {
		t.Errorf("LastN = %d, want 50", summary.LastN)
	}
}

func TestDetectFileType(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		content  string
		want     string
	}{
		// File extension tests
		{"Go file", "main.go", "package main", "go"},
		{"Markdown file", "README.md", "# Title", "markdown"},
		{"JSON file", "data.json", `{"key": "value"}`, "json"},
		{"YAML file", "config.yaml", "key: value", "yaml"},
		{"Python file", "script.py", "print('hello')", "python"},
		{"Shell script", "script.sh", "echo hello", "shell"},
		{"Text file", "notes.txt", "some notes", "text"},
		
		// Content-based detection
		{"Shebang script", "script", "#!/bin/bash\necho hello", "script"},
		{"XML from content", "data", "<?xml version='1.0'?>\n<root></root>", "xml"},
		{"JSON from content", "data", `{"name": "test"}`, "json"},
		{"YAML from content", "data", "key: value\nanother: value2", "yaml"},
		
		// Default
		{"Unknown type", "unknown.xyz", "some content", "text"},
		{"Empty file", "empty.txt", "", "text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectFileType(tt.filename, tt.content)
			if got != tt.want {
				t.Errorf("DetectFileType(%q, %q) = %q, want %q", 
					tt.filename, tt.content[:min(20, len(tt.content))], got, tt.want)
			}
		})
	}
}

func TestCreateFileSummary(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		content  string
	}{
		{
			name:     "Text file should use generic summarizer",
			filename: "notes.txt",
			content:  strings.Join(generateLines(200), "\n"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := CreateFileSummary(tt.filename, tt.content)
			
			// Direct assertions instead of check function
			if summary.FileType != "generic" {
				t.Errorf("FileType = %q, want %q", summary.FileType, "generic")
			}
			if !summary.IsTruncated {
				t.Error("IsTruncated = false, want true for 200-line file")
			}
			if len(summary.FirstLines) != 50 {
				t.Errorf("FirstLines count = %d, want 50", len(summary.FirstLines))
			}
			if len(summary.LastLines) != 50 {
				t.Errorf("LastLines count = %d, want 50", len(summary.LastLines))
			}
		})
	}
}

func TestFormatFileSummary(t *testing.T) {
	summary := &FileSummary{
		TotalLines:   100,
		FirstLines:   []string{"First line", "Second line", "Third line"},
		LastLines:    []string{"Line 98", "Line 99", "Line 100"},
		LineCount:    100,
		FirstN:       3,
		LastN:        3,
		IsTruncated:  true,
		FileType:     "text",
	}

	formatted := FormatFileSummary(summary)
	
	// Check for expected content
	expectedParts := []string{
		"File type: text",
		"Total lines: 100",
		"Showing first 3 and last 3 lines",
		"First lines",
		"Last 3 lines",
		"94 lines omitted",
	}
	
	for _, part := range expectedParts {
		if !strings.Contains(formatted, part) {
			t.Errorf("FormatFileSummary() missing expected part: %q", part)
		}
	}
	
	// Check line numbers are formatted correctly
	if !strings.Contains(formatted, "     1: First line") {
		t.Error("FormatFileSummary() missing line number formatting")
	}
}

func TestFormatSummaryForDisplay(t *testing.T) {
	summary := &FileSummary{
		TotalLines:   500,
		FirstLines:   []string{"First line"},
		LastLines:    []string{"Last line"},
		LineCount:    500,
		FirstN:       1,
		LastN:        1,
		IsTruncated:  true,
		FileType:     "text",
	}

	formatted := FormatSummaryForDisplay("large_file.txt", 102400, summary)
	
	// Check for expected content
	expectedParts := []string{
		"File: large_file.txt",
		"Size: 102400 bytes",
		"Type: text",
		"Lines: 500",
		"⚠️  File is large",
		"Use offset/limit parameters",
	}
	
	for _, part := range expectedParts {
		if !strings.Contains(formatted, part) {
			t.Errorf("FormatSummaryForDisplay() missing expected part: %q", part)
		}
	}
}

// Helper function to generate test lines
func generateLines(count int, start ...int) []string {
	startNum := 1
	if len(start) > 0 {
		startNum = start[0]
	}
	
	lines := make([]string, count)
	for i := 0; i < count; i++ {
		lines[i] = fmt.Sprintf("Line %d", startNum+i)
	}
	return lines
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
