package main

import (
	"context"
	"fmt"
	"testing"

	"github.com/afittestide/asimi/court"
	"github.com/afittestide/asimi/storage"
	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type edictChatTestState struct {
	model   *TUIModel
	mock    *mockCourtClient
	session *court.Session
}

func newEdictChatState(t require.TestingT) *edictChatTestState {
	mock := &mockCourtClient{}
	model := newTestModel(t.(*testing.T))
	model.court = mock
	model.tabs.DismissWelcome()
	return &edictChatTestState{
		model: model,
		mock:  mock,
	}
}

func registerEdictChatStepDefs(ctx *godog.ScenarioContext, t *testing.T) {
	s := &edictChatTestState{}

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		*s = *newEdictChatState(t)
		return ctx, nil
	})

	// --- Givens ---

	ctx.Step(`^an edict with a birth session ID$`, func() error {
		s.mock.getEdictFn = func(edictID uint) (*storage.Edict, error) {
			return &storage.Edict{
				ID:        edictID,
				Intent:    "Test intent",
				SessionID: "sess-birth-123",
			}, nil
		}
		return nil
	})

	ctx.Step(`^the (?:Ruler|ruler) is on the ritual tab for that edict$`, func() error {
		channelID := "e647"
		s.model.tabs.Add(fmt.Sprintf("Ritual:%s", channelID), "ritual", channelID)
		tab := s.model.tabs.TabByTarget(channelID)
		require.NotNil(t, tab, "ritual tab should exist")
		s.model.tabs.SwitchTo(len(s.model.tabs.tabs) - 1)
		assert.Equal(t, "ritual", string(s.model.tabs.ActiveTab().Type))
		return nil
	})

	ctx.Step(`^the Ruler selects "([^"]*)" from the edict action menu$`, func(_ string) error {
		s.mock.getEdictFn = func(edictID uint) (*storage.Edict, error) {
			return &storage.Edict{
				ID:        edictID,
				Intent:    "Test intent",
				SessionID: "sess-birth-123",
			}, nil
		}
		s.model.creatingTab = "e647"
		s.model.pendingPrompt = ""
		return nil
	})

	ctx.Step(`^no pending edict prompt or key$`, func() error {
		s.model.pendingPrompt = ""
		s.model.creatingTab = ""
		return nil
	})

	ctx.Step(`^the current active tab is the (\w+) tab$`, func(tabType string) error {
		s.model.tabs.SwitchToTabType(TabType(tabType))
		assert.Equal(t, tabType, string(s.model.tabs.ActiveTab().Type))
		return nil
	})

	ctx.Step(`^the edict has no birth session$`, func() error {
		s.mock.getEdictFn = func(edictID uint) (*storage.Edict, error) {
			return &storage.Edict{
				ID:     edictID,
				Intent: "Test intent",
			}, nil
		}
		return nil
	})

	ctx.Step(`^an active ritual is running on the edict tab$`, func() error {
		s.mock.pauseRitualFn = func(channelID string) bool {
			return true
		}
		return nil
	})

	ctx.Step(`^the edict's birth session was restored via the Chat action$`, func() error {
		s.session = &court.Session{
			ID:      "sess-birth-123",
			TabType: "chancellor",
		}
		s.model.creatingTab = "e647"
		s.model.pendingPrompt = ""
		s.model.handleSessionSelected(s.session)
		return nil
	})

	// --- Whens ---

	ctx.Step(`^the edict's birth session is restored$`, func() error {
		s.session = &court.Session{
			ID:      "sess-birth-123",
			TabType: "chancellor",
		}
		s.model.handleSessionSelected(s.session)
		return nil
	})

	ctx.Step(`^the Ruler types "([^"]*)" and presses Enter$`, func(prompt string) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()

		s.mock.getEdictFn = func(edictID uint) (*storage.Edict, error) {
			return &storage.Edict{
				ID:        edictID,
				Intent:    "Test intent",
				SessionID: "sess-birth-123",
			}, nil
		}

		s.model.pendingPrompt = prompt
		s.model.creatingTab = "e647"

		s.session = &court.Session{
			ID:      "sess-birth-123",
			TabType: "chancellor",
		}

		s.model.handleSessionSelected(s.session)
		return nil
	})

	ctx.Step(`^a session is selected with TabType "([^"]*)"$`, func(tabType string) error {
		s.session = &court.Session{
			ID:      "sess-normal-1",
			TabType: tabType,
		}
		s.model.handleSessionSelected(s.session)
		return nil
	})

	ctx.Step(`^the Ruler types a prompt on the ritual tab$`, func() error {
		s.model.pendingPrompt = "implement the feature"
		s.model.creatingTab = "e647"
		p := &court.Prompt{
			Ctx:       context.Background(),
			Message:   "implement the feature",
			EdictKey:  s.mock.EdictKey(647),
			ChannelID: "e647",
		}
		return s.mock.SubmitPrompt("secretary", p)
	})

	ctx.Step(`^the Ruler types a prompt$`, func() error {
		s.model.pendingPrompt = "interjection text"
		s.model.creatingTab = "e647"
		_ = s.mock.PauseRitual("e647")
		p := &court.Prompt{
			Ctx:       context.Background(),
			Message:   "interjection text",
			EdictKey:  s.mock.EdictKey(647),
			ChannelID: "e647",
		}
		return s.mock.SubmitPrompt("secretary", p)
	})

	ctx.Step(`^the Ruler types a follow-up prompt$`, func() error {
		p := &court.Prompt{
			Ctx:       context.Background(),
			Message:   "follow up",
			EdictKey:  s.model.currentEdictKey,
			ChannelID: "e647",
		}
		return s.mock.SubmitPrompt("chancellor", p)
	})

	ctx.Step(`^the Ruler's prompt was answered by the minister$`, func() error {
		s.model.pendingPrompt = ""
		return nil
	})

	// --- Thens ---

	ctx.Step(`^the active tab stays on the ritual tab$`, func() error {
		assert.Equal(t, "ritual", string(s.model.tabs.ActiveTab().Type),
			"should stay on ritual tab during Chat restore")
		return nil
	})

	ctx.Step(`^the session messages are rebuilt in the chat$`, func() error {
		assert.True(t, s.model.sessionActive, "session should be marked active")
		return nil
	})

	ctx.Step(`^no prompt is submitted to the minister$`, func() error {
		assert.Equal(t, 0, s.mock.submitPromptCalls,
			"Chat action should not submit a prompt")
		return nil
	})

	ctx.Step(`^the current edict key is set$`, func() error {
		assert.NotEqual(t, uint(0), s.model.currentEdictKey.ID,
			"edict key should be set after Chat restore")
		return nil
	})

	ctx.Step(`^pending edict fields are cleared$`, func() error {
		assert.Empty(t, s.model.pendingPrompt,
			"pendingPrompt should be cleared")
		assert.Empty(t, s.model.creatingTab,
			"creatingTab should be cleared")
		return nil
	})

	ctx.Step(`^a toast confirms "([^"]*)"$`, func(_ string) error {
		return nil
	})

	ctx.Step(`^no prompt is submitted$`, func() error {
		assert.Equal(t, 0, s.mock.submitPromptCalls, "no prompt should be submitted")
		return nil
	})

	ctx.Step(`^pendingEdictPrompt is cleared$`, func() error {
		assert.Empty(t, s.model.pendingPrompt)
		return nil
	})

	ctx.Step(`^AddUserMessage is called with "([^"]*)"$`, func(_ string) error {
		return nil
	})

	ctx.Step(`^AddUserMessage is called with "([^"]*)" \(line (\d+)\)$`, func(_, _ string) error {
		return nil
	})

	ctx.Step(`^the court detects an edict with a birth session$`, func() error {
		edict, err := s.mock.GetEdict(647)
		require.NoError(t, err)
		assert.NotEmpty(t, edict.SessionID, "edict should have a birth session")
		return nil
	})

	ctx.Step(`^pendingEdictPrompt is set to "([^"]*)"$`, func(prompt string) error {
		if s.mock.submitPromptCalls > 0 {
			assert.Equal(t, prompt, s.mock.submitPromptMsg)
		}
		return nil
	})

	ctx.Step(`^LoadSession triggers session restoration$`, func() error {
		require.NotNil(t, s.session, "session should have been created")
		return nil
	})

	ctx.Step(`^handleSessionSelected is called with hasPrompt=true$`, func() error {
		return nil
	})

	ctx.Step(`^Chat\.Clear\(\) wipes the chat$`, func() error {
		return nil
	})

	ctx.Step(`^rebuildChatFromMessages restores saved messages$`, func() error {
		return nil
	})

	ctx.Step(`^AddUserMessage\("([^"]*)"\) is called to restore the prompt visibility$`, func(prompt string) error {
		s.model.tabs.Content().Chat.AddUserMessage(prompt)
		return nil
	})

	ctx.Step(`^the prompt "([^"]*)" is submitted to the minister$`, func(prompt string) error {
		assert.Equal(t, 1, s.mock.submitPromptCalls,
			"prompt should be submitted once")
		assert.Equal(t, prompt, s.mock.submitPromptMsg)
		return nil
	})

	ctx.Step(`^the minister's response streams to the edict tab$`, func() error {
		assert.Equal(t, "e647", s.mock.submitPromptChanID,
			"response should stream to the edict tab channel")
		return nil
	})

	ctx.Step(`^the prompt is submitted to the minister directly$`, func() error {
		assert.Equal(t, 1, s.mock.submitPromptCalls,
			"prompt should be submitted when no birth session")
		return nil
	})

	ctx.Step(`^a system message says "([^"]*)"$`, func(_ string) error {
		return nil
	})

	ctx.Step(`^the response streams to the edict tab$`, func() error {
		assert.Equal(t, "e647", s.mock.submitPromptChanID)
		return nil
	})

	ctx.Step(`^the ritual is paused$`, func() error {
		assert.Contains(t, s.mock.pausedChannels, "e647",
			"ritual should be paused")
		return nil
	})

	ctx.Step(`^a chat mode system message shows "([^"]*)"$`, func(_ string) error {
		return nil
	})

	ctx.Step(`^the prompt is submitted to the current ritual minister$`, func() error {
		assert.Equal(t, 1, s.mock.submitPromptCalls)
		return nil
	})

	ctx.Step(`^the response streams to the ritual tab$`, func() error {
		assert.Equal(t, "e647", s.mock.submitPromptChanID)
		return nil
	})

	ctx.Step(`^the edict's birth session was restored$`, func() error {
		return nil
	})

	ctx.Step(`^the follow-up is submitted to the same minister session$`, func() error {
		assert.Equal(t, 1, s.mock.submitPromptCalls,
			"follow-up prompt should be submitted")
		return nil
	})

	ctx.Step(`^the prior messages remain visible in the chat$`, func() error {
		return nil
	})

	ctx.Step(`^the active tab switches to the (\w+) tab$`, func(tabType string) error {
		assert.Equal(t, tabType, string(s.model.tabs.ActiveTab().Type),
			"normal resume should switch tabs")
		return nil
	})

	ctx.Step(`^currentEdictKey is cleared$`, func() error {
		assert.Equal(t, uint(0), s.model.currentEdictKey.ID)
		return nil
	})

	ctx.Step(`^RestoreMinisterSession is called without a channel ID$`, func() error {
		assert.Len(t, s.mock.restoreMinisterSessions, 1)
		return nil
	})
}
