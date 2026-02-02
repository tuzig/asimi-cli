package config

import (
	"regexp"
	"strings"
)

// =============================================================================
// TOML Comment-Preserving Helper Functions
// =============================================================================
// These functions use regex-based patching to modify TOML files while preserving
// all comments (both full-line comments and inline comments).

// EscapeTOMLString escapes special characters for TOML string values.
func EscapeTOMLString(s string) string {
	// Basic escaping for quotes and backslashes
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

// FindTOMLSectionBounds finds the start and end positions of a TOML section.
// Returns (sectionStart, sectionEnd, found) where:
// - sectionStart is the index of the line containing [section]
// - sectionEnd is the index of the first line of the next section (or len(lines))
// - found indicates whether the section was found
func FindTOMLSectionBounds(lines []string, section string) (int, int, bool) {
	sectionStart := -1
	sectionEnd := len(lines)

	// Build regex to match the section header
	sectionPattern := regexp.MustCompile(`^\s*\[` + regexp.QuoteMeta(section) + `\]\s*$`)
	nextSectionPattern := regexp.MustCompile(`^\s*\[[^\]]+\]\s*$`)

	for i, line := range lines {
		if sectionStart == -1 {
			if sectionPattern.MatchString(line) {
				sectionStart = i
			}
		} else {
			// Look for the next section
			if nextSectionPattern.MatchString(line) {
				sectionEnd = i
				break
			}
		}
	}

	return sectionStart, sectionEnd, sectionStart != -1
}

// UpdateTOMLValue updates a single key's value in a TOML section, preserving comments.
// Returns the modified content and whether the key was found and updated.
func UpdateTOMLValue(content, section, key, newValue string) (string, bool) {
	lines := strings.Split(content, "\n")
	sectionStart, sectionEnd, found := FindTOMLSectionBounds(lines, section)
	if !found {
		return content, false
	}

	// Build regex to match the key within the section
	// Matches: key = "value", key = 'value', key = value
	// Preserves inline comments
	keyPattern := regexp.MustCompile(`^(\s*` + regexp.QuoteMeta(key) + `\s*=\s*)("[^"]*"|'[^']*'|[^#\n]*)(.*)$`)

	for i := sectionStart + 1; i < sectionEnd; i++ {
		line := lines[i]
		if matches := keyPattern.FindStringSubmatch(line); matches != nil {
			// matches[1] = "key = " (with any leading whitespace)
			// matches[2] = the old value
			// matches[3] = inline comment (if any)
			newLine := matches[1] + `"` + EscapeTOMLString(newValue) + `"` + matches[3]
			lines[i] = newLine
			return strings.Join(lines, "\n"), true
		}
	}

	return content, false
}

// InsertTOMLValue inserts a new key=value in a section (at the end of the section).
// If the section doesn't exist, it returns the content unchanged.
func InsertTOMLValue(content, section, key, value string) string {
	lines := strings.Split(content, "\n")
	sectionStart, sectionEnd, found := FindTOMLSectionBounds(lines, section)
	if !found {
		return content
	}

	// Find the best insertion point (before the next section or at end of section content)
	insertAt := sectionEnd

	// Look for the last non-empty, non-comment line in the section to insert after it
	for i := sectionEnd - 1; i > sectionStart; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			insertAt = i + 1
			break
		}
	}

	// If section is empty (only header), insert right after header
	if insertAt == sectionEnd && sectionEnd == sectionStart+1 {
		insertAt = sectionStart + 1
	} else if insertAt == sectionEnd {
		// Check if we're at the section header still
		for i := sectionStart + 1; i < sectionEnd; i++ {
			trimmed := strings.TrimSpace(lines[i])
			if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
				break
			}
			if i == sectionEnd-1 {
				insertAt = sectionEnd
			}
		}
	}

	newLine := key + ` = "` + EscapeTOMLString(value) + `"`

	// Insert the new line
	newLines := make([]string, 0, len(lines)+1)
	newLines = append(newLines, lines[:insertAt]...)
	newLines = append(newLines, newLine)
	newLines = append(newLines, lines[insertAt:]...)

	return strings.Join(newLines, "\n")
}

// RemoveTOMLKey removes a key from a section, preserving surrounding comments.
// Returns the modified content.
func RemoveTOMLKey(content, section, key string) string {
	lines := strings.Split(content, "\n")
	sectionStart, sectionEnd, found := FindTOMLSectionBounds(lines, section)
	if !found {
		return content
	}

	// Build regex to match the key
	keyPattern := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)

	for i := sectionStart + 1; i < sectionEnd; i++ {
		if keyPattern.MatchString(lines[i]) {
			// Remove this line
			newLines := make([]string, 0, len(lines)-1)
			newLines = append(newLines, lines[:i]...)
			newLines = append(newLines, lines[i+1:]...)
			return strings.Join(newLines, "\n")
		}
	}

	return content
}

// EnsureTOMLSection ensures a section exists in the content.
// If the section doesn't exist, it appends it at the end.
// Returns the modified content.
func EnsureTOMLSection(content, section string) string {
	lines := strings.Split(content, "\n")
	_, _, found := FindTOMLSectionBounds(lines, section)
	if found {
		return content
	}

	// Append the section at the end
	var result strings.Builder
	result.WriteString(content)

	// Ensure there's a newline before the new section
	if len(content) > 0 && !strings.HasSuffix(content, "\n") {
		result.WriteString("\n")
	}
	// Add blank line if content doesn't end with one
	if len(content) > 0 && !strings.HasSuffix(content, "\n\n") {
		result.WriteString("\n")
	}

	result.WriteString("[" + section + "]\n")
	return result.String()
}

// UpdateOrInsertTOMLValue updates a key if it exists, or inserts it if it doesn't.
// If the section doesn't exist, it creates the section first.
func UpdateOrInsertTOMLValue(content, section, key, value string) string {
	// Ensure section exists
	content = EnsureTOMLSection(content, section)

	// Try to update existing key
	updated, found := UpdateTOMLValue(content, section, key, value)
	if found {
		return updated
	}

	// Key doesn't exist, insert it
	return InsertTOMLValue(content, section, key, value)
}
