package utils

import (
	"strings"
	"testing"
)

func TestMsgBlockBuilder_NewMsgBlockBuilder(t *testing.T) {
	builder := NewMsgBlockBuilder("prefix")
	if builder == nil {
		t.Fatal("NewMsgBlockBuilder() returned nil")
	}
	if builder.prefix != "prefix" {
		t.Errorf("NewMsgBlockBuilder().prefix = %q, want %q", builder.prefix, "prefix")
	}
	if len(builder.lines) != 0 {
		t.Errorf("NewMsgBlockBuilder().lines = %v, want empty", builder.lines)
	}
}

func TestMsgBlockBuilder_WriteString(t *testing.T) {
	builder := NewMsgBlockBuilder("")
	result := builder.WriteString("hello")
	if result != builder {
		t.Error("WriteString() should return the builder for chaining")
	}
	if builder.currentLine.String() != "hello" {
		t.Errorf("WriteString() currentLine = %q, want %q", builder.currentLine.String(), "hello")
	}

	// Test chaining
	builder.WriteString(" world").WriteString("!")
	if builder.currentLine.String() != "hello world!" {
		t.Errorf("WriteString() chaining failed, got %q", builder.currentLine.String())
	}
}

func TestMsgBlockBuilder_Writef(t *testing.T) {
	builder := NewMsgBlockBuilder("")
	result := builder.Writef("number: %d, string: %s", 42, "test")
	if result != builder {
		t.Error("Writef() should return the builder for chaining")
	}
	if builder.currentLine.String() != "number: 42, string: test" {
		t.Errorf("Writef() currentLine = %q, want %q", builder.currentLine.String(), "number: 42, string: test")
	}
}

func TestMsgBlockBuilder_WriteLn(t *testing.T) {
	builder := NewMsgBlockBuilder("> ")
	builder.WriteString("line1")
	builder.WriteLn()
	if builder.currentLine.Len() != 0 {
		t.Errorf("WriteLn() should reset currentLine, got %q", builder.currentLine.String())
	}
	if len(builder.lines) != 1 {
		t.Errorf("WriteLn() lines count = %d, want 1", len(builder.lines))
	}
	if builder.lines[0] != "line1" {
		t.Errorf("WriteLn() lines[0] = %q, want %q", builder.lines[0], "line1")
	}

	// Test WriteLn with arguments
	builder.WriteLn("arg1", "arg2")
	if len(builder.lines) != 2 {
		t.Errorf("WriteLn(args) lines count = %d, want 2", len(builder.lines))
	}
	if builder.lines[1] != "arg1arg2" {
		t.Errorf("WriteLn(args) lines[1] = %q, want %q", builder.lines[1], "arg1arg2")
	}
}

func TestMsgBlockBuilder_WriteLnf(t *testing.T) {
	builder := NewMsgBlockBuilder("> ")
	result := builder.WriteLnf("value=%d", 99)
	if result != builder {
		t.Error("WriteLnf() should return the builder for chaining")
	}
	if len(builder.lines) != 1 {
		t.Errorf("WriteLnf() lines count = %d, want 1", len(builder.lines))
	}
	if builder.lines[0] != "value=99" {
		t.Errorf("WriteLnf() lines[0] = %q, want %q", builder.lines[0], "value=99")
	}
}

func TestMsgBlockBuilder_String_Empty(t *testing.T) {
	builder := NewMsgBlockBuilder("> ")
	result := builder.String()
	if result != "" {
		t.Errorf("String() on empty builder = %q, want %q", result, "")
	}
}

func TestMsgBlockBuilder_String_SingleLine(t *testing.T) {
	builder := NewMsgBlockBuilder("> ")
	builder.WriteString("single line")
	result := builder.String()
	expected := "> single line"
	if result != expected {
		t.Errorf("String() = %q, want %q", result, expected)
	}
}

func TestMsgBlockBuilder_String_MultipleLines(t *testing.T) {
	builder := NewMsgBlockBuilder("> ")
	builder.WriteString("first").WriteLn()
	builder.WriteString("second").WriteLn()
	builder.WriteString("third")
	result := builder.String()

	lines := strings.Split(result, "\n")
	if len(lines) != 3 {
		t.Fatalf("String() produced %d lines, want 3: %q", len(lines), result)
	}

	if lines[0] != "> first" {
		t.Errorf("String() first line = %q, want %q", lines[0], "> first")
	}
	if lines[1] != TreeMidPrefix+"second" {
		t.Errorf("String() second line = %q, want %q", lines[1], TreeMidPrefix+"second")
	}
	if lines[2] != TreeFinalPrefix+"third" {
		t.Errorf("String() third line = %q, want %q", lines[2], TreeFinalPrefix+"third")
	}
}

func TestMsgBlockBuilder_String_OnlyWritesWithLn(t *testing.T) {
	builder := NewMsgBlockBuilder("> ")
	builder.WriteString("line1").WriteLn()
	builder.WriteString("line2") // No WriteLn - should be in currentLine
	result := builder.String()

	lines := strings.Split(result, "\n")
	if len(lines) != 2 {
		t.Fatalf("String() produced %d lines, want 2: %q", len(lines), result)
	}

	if lines[0] != "> line1" {
		t.Errorf("String() first line = %q, want %q", lines[0], "> line1")
	}
	if lines[1] != TreeFinalPrefix+"line2" {
		t.Errorf("String() second line = %q, want %q", lines[1], TreeFinalPrefix+"line2")
	}
}

func TestMsgBlockBuilder_Chaining(t *testing.T) {
	result := NewMsgBlockBuilder("").
		WriteString("a").
		Writef("-%d", 1).
		WriteLn().
		WriteString("b").
		WriteLnf("-%d", 2).
		WriteString("c").
		String()

	expected := TreeFinalPrefix + "c"
	if !strings.HasSuffix(result, expected) {
		t.Errorf("Chained String() = %q, expected to end with %q", result, expected)
	}
}

func TestTreePrefixConstants(t *testing.T) {
	if TreeFinalPrefix == "" {
		t.Error("TreeFinalPrefix should not be empty")
	}
	if TreeMidPrefix == "" {
		t.Error("TreeMidPrefix should not be empty")
	}
	if TreeFinalPrefix == TreeMidPrefix {
		t.Error("TreeFinalPrefix and TreeMidPrefix should be different")
	}
}
