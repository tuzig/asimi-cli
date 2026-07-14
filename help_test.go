package main

import (
	"strings"
	"testing"
)

func TestHelpTopicFromEmbed(t *testing.T) {
	hw := NewHelpWindow()

	topics := []string{
		"index", "modes", "commands", "navigation", "editing",
		"files", "sessions", "context", "models", "login", "config", "quickref",
		"quickstart", "workflows", "troubleshooting",
	}

	for _, topic := range topics {
		t.Run(topic, func(t *testing.T) {
			content := hw.getHelpTopic(topic)
			if content == "" {
				t.Fatalf("getHelpTopic(%q) returned empty content", topic)
			}
			if !strings.HasPrefix(content, "# ") {
				t.Errorf("getHelpTopic(%q) should start with '# ' header, got: %q", topic, content[:min(50, len(content))])
			}
		})
	}
}

func TestHelpTopicEmpty(t *testing.T) {
	hw := NewHelpWindow()
	content := hw.getHelpTopic("")
	if content == "" {
		t.Fatal("getHelpTopic(\"\") should return index content")
	}
	if !strings.HasPrefix(content, "# Asimi Help Index") {
		t.Errorf("getHelpTopic(\"\") should return index content starting with '# Asimi Help Index'")
	}
}

func TestHelpTopicNotFound(t *testing.T) {
	hw := NewHelpWindow()
	content := hw.getHelpTopic("nonexistent")
	if !strings.Contains(content, "Help Topic Not Found") {
		t.Errorf("getHelpTopic(\"nonexistent\") should return 'not found' message")
	}
}

func TestHelpSetGetTopic(t *testing.T) {
	hw := NewHelpWindow()

	if hw.GetTopic() != "index" {
		t.Errorf("default topic should be 'index', got %q", hw.GetTopic())
	}

	hw.SetTopic("modes")
	if hw.GetTopic() != "modes" {
		t.Errorf("topic should be 'modes', got %q", hw.GetTopic())
	}

	hw.SetTopic("")
	if hw.GetTopic() != "index" {
		t.Errorf("empty topic should default to 'index', got %q", hw.GetTopic())
	}
}

func TestHelpRenderContent(t *testing.T) {
	hw := NewHelpWindow()
	hw.SetTopic("modes")

	rendered := hw.RenderContent()
	if rendered == "" {
		t.Fatal("RenderContent() returned empty string")
	}
}

func TestHelpRenderContentAllTopics(t *testing.T) {
	topics := []string{
		"index", "modes", "commands", "navigation", "editing",
		"files", "sessions", "context", "models", "login", "config", "quickref",
		"quickstart", "workflows", "troubleshooting",
	}
	for _, topic := range topics {
		t.Run(topic, func(t *testing.T) {
			hw := NewHelpWindow()
			hw.SetTopic(topic)
			rendered := hw.RenderContent()
			if rendered == "" {
				t.Fatalf("RenderContent() returned empty for topic %q", topic)
			}
			// Glamour renders headers, tables, bold, blockquotes, code blocks.
			// In a TTY context, ** markers are converted to ANSI bold;
			// in a no-TTY context (like CI), the ASCII style keeps them.
			// We only verify non-empty output and no crashes here.
		})
	}
}

func TestHelpRenderContentWordWrap(t *testing.T) {
	hw := NewHelpWindow()
	hw.SetSize(40, 20)
	hw.SetTopic("index")

	rendered := hw.RenderContent()
	if rendered == "" {
		t.Fatal("RenderContent() returned empty string")
	}

	// No line should exceed the wrap width (40 - 2 for padding = 38)
	// We allow some tolerance for ANSI codes by stripping them
	for _, line := range strings.Split(rendered, "\n") {
		stripped := stripANSI(line)
		if len(stripped) > 40 {
			t.Errorf("line length %d exceeds width 40: %q", len(stripped), stripped[:min(40, len(stripped))])
		}
	}
}
