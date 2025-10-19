package main

import "testing"

func TestCommandRegistryOrder(t *testing.T) {
	registry := NewCommandRegistry()
	commands := registry.GetAllCommands()
	if len(commands) == 0 {
		t.Fatalf("expected commands to be registered")
	}
	if commands[0].Name != "/help" {
		t.Fatalf("expected first command to be /help, got %s", commands[0].Name)
	}
}

func TestHandleHelpCommandLeader(t *testing.T) {
	t.Run("vi mode uses colon leader", func(t *testing.T) {
		prompt := NewPromptComponent(80, 5)
		prompt.SetViMode(true)
		model := &TUIModel{prompt: prompt}

		cmd := handleHelpCommand(model, nil)
		if cmd == nil {
			t.Fatalf("expected non-nil command")
		}

		msg := cmd()
		helpMsg, ok := msg.(showHelpMsg)
		if !ok {
			t.Fatalf("expected showHelpMsg got %T", msg)
		}
		if helpMsg.leader != ":" {
			t.Fatalf("expected leader ':' got %q", helpMsg.leader)
		}
	})

	t.Run("default leader is slash", func(t *testing.T) {
		model := &TUIModel{}

		cmd := handleHelpCommand(model, nil)
		if cmd == nil {
			t.Fatalf("expected non-nil command")
		}

		msg := cmd()
		helpMsg, ok := msg.(showHelpMsg)
		if !ok {
			t.Fatalf("expected showHelpMsg got %T", msg)
		}
		if helpMsg.leader != "/" {
			t.Fatalf("expected leader '/' got %q", helpMsg.leader)
		}
	})
}
