package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/afittestide/asimi/shogunate"
	"github.com/maximhq/bifrost/core/schemas"
	"github.com/stretchr/testify/assert"
)

func TestNewResumeWindowDefaults(t *testing.T) {
	window := NewResumeWindow()

	assert.Equal(t, 70, window.Width)
	assert.Equal(t, 15, window.Height)
	assert.False(t, window.Loading)
	assert.Empty(t, window.Items)
}

func TestResumeWindowSetSizeAdjustsVisibleSlots(t *testing.T) {
	window := NewResumeWindow()

	window.SetSize(80, 10)
	assert.Equal(t, 80, window.Width)
	assert.Equal(t, 10, window.Height)

	window.SetSize(50, 2)
	assert.Equal(t, 2, window.Height) // min clamp
}

func TestResumeWindowSetSessionsAndRender(t *testing.T) {
	window := NewResumeWindow()
	now := time.Now()

	sessions := []shogunate.Session{
		testSession("s-1", "Refactor prompt", now, "Need to refactor"),
		testSession("s-2", "Investigate bug", now.Add(-2*time.Hour), "Bug details"),
	}

	window.SetSessions(sessions)
	assert.False(t, window.Loading)
	assert.Equal(t, 2, window.GetItemCount())

	render := window.RenderList(0, 0, window.GetVisibleSlots())
	assert.Contains(t, render, sessionTitlePreview(sessions[0]))
	assert.Contains(t, render, sessionTitlePreview(sessions[1]))
	assert.Contains(t, render, "▶ ")
	assert.Contains(t, render, "]    1 Need to refactor")
}

func TestResumeWindowLoadingAndErrorStates(t *testing.T) {
	window := NewResumeWindow()

	window.SetLoading(true)
	assert.Contains(t, window.RenderList(0, 0, window.GetVisibleSlots()), "Loading sessions")

	window.SetLoading(false)
	window.SetError(assert.AnError)
	render := window.RenderList(0, 0, window.GetVisibleSlots())
	assert.Contains(t, render, "Error loading sessions")
	assert.NotContains(t, render, "Loading sessions")
}

func TestResumeWindowEmptyState(t *testing.T) {
	window := NewResumeWindow()
	window.SetSessions(nil)

	render := window.RenderList(0, 0, window.GetVisibleSlots())
	assert.Contains(t, render, "No previous sessions found")
	assert.Contains(t, render, "Start chatting to create a new session")
}

func TestResumeWindowScrollInfo(t *testing.T) {
	window := NewResumeWindow()
	now := time.Now()

	var sessions []shogunate.Session
	for i := 0; i < 20; i++ {
		sessions = append(sessions, testSession(
			fmt.Sprintf("s-%d", i+1),
			fmt.Sprintf("Prompt %d", i+1),
			now.Add(-time.Duration(i)*time.Minute),
			fmt.Sprintf("Message %d", i+1),
		))
	}

	window.SetSessions(sessions)
	render := window.RenderList(5, 5, 5)

	assert.Contains(t, render, "Message 6")    // Uses last human message when Messages is populated
	assert.NotContains(t, render, "Message 2") // scrolled past
}

func TestResumeWindowGetSelectedSession(t *testing.T) {
	window := NewResumeWindow()
	window.SetSessions([]shogunate.Session{
		testSession("one", "First", time.Now(), "msg"),
	})

	assert.Nil(t, window.GetSelectedSession(-1))
	assert.Nil(t, window.GetSelectedSession(2))

	session := window.GetSelectedSession(0)
	assert.NotNil(t, session)
	assert.Equal(t, "one", session.ID)
}

func TestSessionTitlePreviewFallbacks(t *testing.T) {
	session := testSession("s-1", "", time.Now(), "")
	session.SetMessages(nil)
	assert.Equal(t, "Recent activity", sessionTitlePreview(session))

	session.FirstPrompt = " initial "
	assert.Equal(t, "initial", sessionTitlePreview(session))

	session.SetMessages([]schemas.ChatMessage{
		textMessage(schemas.ChatMessageRoleUser, "User question"),
	})
	assert.Equal(t, "User question", sessionTitlePreview(session))
}

func testSession(id, prompt string, updated time.Time, messageTexts ...string) shogunate.Session {
	var messages []schemas.ChatMessage
	for _, text := range messageTexts {
		messages = append(messages, textMessage(schemas.ChatMessageRoleUser, text))
	}

	s := shogunate.Session{
		ID:           id,
		FirstPrompt:  prompt,
		LastUpdated:  updated,
		MessageCount: len(messages),
		Model:        "test",
	}
	s.SetMessages(messages)
	return s
}
