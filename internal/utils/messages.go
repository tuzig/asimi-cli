// Package utils provides shared utility functions for message formatting.
package utils

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MustJSON marshals a value to JSON, returning "null" on error.
func MustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

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
