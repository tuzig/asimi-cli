// Package utils provides shared utility functions for message formatting.
package utils

import (
	"fmt"
	"strings"
)

// Tree prefix constants for visual formatting
const (
	TreeFinalPrefix = " ╰ "
	TreeMidPrefix   = " │ "
)

// MsgBlockBuilder builds multi-line messages with tree prefixes.
// It mimics strings.Builder but automatically adds TreeMidPrefix to intermediate
// lines and TreeFinalPrefix to the last line when String() is called.
type MsgBlockBuilder struct {
	prefix      string
	lines       []string
	currentLine strings.Builder
}

// NewMsgBlockBuilder creates a new MsgBlockBuilder with the given prefix for the first line
func NewMsgBlockBuilder(prefix string) *MsgBlockBuilder {
	return &MsgBlockBuilder{
		prefix: prefix,
		lines:  make([]string, 0),
	}
}

// WriteString appends text to the current line (without ending it)
func (b *MsgBlockBuilder) WriteString(s string) *MsgBlockBuilder {
	b.currentLine.WriteString(s)
	return b
}

// Writef appends formatted text to the current line (without ending it)
func (b *MsgBlockBuilder) Writef(format string, args ...interface{}) *MsgBlockBuilder {
	b.currentLine.WriteString(fmt.Sprintf(format, args...))
	return b
}

// WriteLn ends the current line and starts a new one.
// If called with arguments, appends them to the current line first.
func (b *MsgBlockBuilder) WriteLn(s ...string) *MsgBlockBuilder {
	for _, str := range s {
		b.currentLine.WriteString(str)
	}
	b.lines = append(b.lines, b.currentLine.String())
	b.currentLine.Reset()
	return b
}

// WriteLnf appends formatted text to the current line and ends it
func (b *MsgBlockBuilder) WriteLnf(format string, args ...interface{}) *MsgBlockBuilder {
	b.currentLine.WriteString(fmt.Sprintf(format, args...))
	b.lines = append(b.lines, b.currentLine.String())
	b.currentLine.Reset()
	return b
}

// String returns the formatted message with tree prefixes.
// The first line gets the configured prefix, intermediate lines get TreeMidPrefix,
// and the last line gets TreeFinalPrefix.
func (b *MsgBlockBuilder) String() string {
	// Include any pending content in currentLine
	lines := b.lines
	if b.currentLine.Len() > 0 {
		lines = append(lines, b.currentLine.String())
	}

	if len(lines) == 0 {
		return ""
	}

	if len(lines) == 1 {
		return b.prefix + lines[0]
	}

	var result strings.Builder
	result.WriteString(b.prefix)
	result.WriteString(lines[0])

	for i := 1; i < len(lines)-1; i++ {
		result.WriteString("\n")
		result.WriteString(TreeMidPrefix)
		result.WriteString(lines[i])
	}

	result.WriteString("\n")
	result.WriteString(TreeFinalPrefix)
	result.WriteString(lines[len(lines)-1])

	return result.String()
}

// TruncateMiddle truncates a string by keeping the beginning and end portions
// and replacing the middle with an ellipsis. The maxWidth parameter specifies
// the maximum total width of the result. The beginning portion takes 1/3 of
// the available space, and the end portion takes the remaining 2/3.
// If the message fits within maxWidth, it is returned unchanged.
func TruncateMiddle(message string, maxWidth int) string {
	// Handle edge cases
	if maxWidth <= 0 {
		return ""
	}

	// Count runes for proper Unicode handling
	runes := []rune(message)
	msgLen := len(runes)

	// If message fits, return as-is
	if msgLen <= maxWidth {
		return message
	}

	// Need at least 4 characters for truncation to make sense (1 + ellipsis + 1 + 1)
	if maxWidth < 4 {
		return string(runes[:maxWidth])
	}

	// Calculate portions: beginning gets 1/3, end gets 2/3 (minus ellipsis)
	ellipsisLen := 1 // Single rune
	availableWidth := maxWidth - ellipsisLen

	beginLen := availableWidth / 3
	endLen := availableWidth - beginLen

	// Ensure we have at least 1 character on each side
	if beginLen < 1 {
		beginLen = 1
		endLen = availableWidth - 1
	}
	if endLen < 1 {
		endLen = 1
		beginLen = availableWidth - 1
	}

	// Build truncated string
	beginning := string(runes[:beginLen])
	end := string(runes[msgLen-endLen:])

	return beginning + "…" + end
}
