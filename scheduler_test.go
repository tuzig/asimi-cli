package main

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

type mockTool struct {
	name        string
	description string
	callFunc    func(ctx context.Context, input string) (string, error)
}

func (t *mockTool) Name() string {
	return t.name
}

func (t *mockTool) Description() string {
	return t.description
}

func (t *mockTool) Call(ctx context.Context, input string) (string, error) {
	if t.callFunc != nil {
		return t.callFunc(ctx, input)
	}
	return "", nil
}

type mockModel struct {
	messages []tea.Msg
}

func (m *mockModel) Init() tea.Cmd {
	return nil
}

func (m *mockModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.messages = append(m.messages, msg)
	if _, ok := msg.(tea.QuitMsg); ok {
		return m, tea.Quit
	}
	return m, nil
}

func (m *mockModel) View() string {
	return ""
}

func TestCoreToolScheduler(t *testing.T) {
	model := &mockModel{}
    // Use the package-level program so the scheduler can send messages to it.
    program = tea.NewProgram(model, tea.WithoutRenderer(), tea.WithInput(nil))
	scheduler := NewCoreToolScheduler(func(msg any) {
		program.Send(msg)
	})

	done := make(chan struct{})
	go func() {
		if _, err := program.Run(); err != nil {
			t.Log(err)
		}
		close(done)
	}()

	tool := &mockTool{
		name:        "test-tool",
		description: "A tool for testing",
		callFunc: func(ctx context.Context, input string) (string, error) {
			time.Sleep(10 * time.Millisecond)
			return "output for " + input, nil
		},
	}

	resultChan := scheduler.Schedule(tool, "test-input")

	// Wait for the result
	result := <-resultChan

	// Assertions
	assert.NoError(t, result.Error)
	assert.Equal(t, "output for test-input", result.Output)

	program.Quit()
	<-done

	// Check messages sent to the model
	assert.GreaterOrEqual(t, len(model.messages), 3)
	_, ok := model.messages[0].(ToolCallScheduledMsg)
	assert.True(t, ok)
	_, ok = model.messages[1].(ToolCallExecutingMsg)
	assert.True(t, ok)
	_, ok = model.messages[2].(ToolCallSuccessMsg)
	assert.True(t, ok)
}
