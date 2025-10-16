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
