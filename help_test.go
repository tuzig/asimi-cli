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
