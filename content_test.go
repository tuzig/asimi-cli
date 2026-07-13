package main

import (
	"strings"
	"testing"

	"github.com/afittestide/asimi/internal/ministers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTabGreetingsAllPresent(t *testing.T) {
	defs, err := ministers.LoadMinisters()
	require.NoError(t, err)

	greetings := make(map[string]string, len(defs))
	for _, d := range defs {
		greetings[d.ID] = d.Greeting
	}

	for _, target := range []string{"chancellor", "sage", "forge", "judge"} {
		g := greetings[target]
		if g == "" {
			t.Errorf("missing greeting for minister %q", target)
		}
	}
}

func TestAddGreetingMessage_UsesGreetingType(t *testing.T) {
	chat := NewChatComponent(80, 20, true)

	chat.AddGreetingMessage("Welcome to the Court")

	assert.Len(t, chat.Messages, 1, "should have exactly one greeting message")
	assert.Equal(t, MessageTypeGreeting, chat.Messages[0].Type,
		"greeting message should have MessageTypeGreeting type")
	assert.Equal(t, "Welcome to the Court", chat.Messages[0].Content,
		"greeting content should be stored without prefix")
}

func TestAddGreetingMessage_RendersWithCastleEmoji(t *testing.T) {
	chat := NewChatComponent(80, 20, true)

	chat.AddGreetingMessage("Welcome, Ruler")

	view := chat.Viewport.View()
	assert.True(t, strings.Contains(view, "🏯"),
		"rendered greeting should contain the 🏯 prefix emoji")
}

func TestAddGreetingMessage_NoSystemPrefix(t *testing.T) {
	chat := NewChatComponent(80, 20, true)

	chat.AddGreetingMessage("Greetings, Ruler")

	view := chat.Viewport.View()
	// The old rendering used systemPrefix ("🛠️  ") — greetings should no longer include it
	assert.False(t, strings.Contains(view, "🛠️"),
		"greeting should not contain the system/tool prefix 🛠️")
}

func TestAddGreetingMessage_MarkdownRendered(t *testing.T) {
	chat := NewChatComponent(80, 20, true)

	// Use markdown bold in the greeting
	chat.AddGreetingMessage("Welcome **Ruler** to the Court")

	view := chat.Viewport.View()
	// Markdown rendering processes the content — the greeting type ensures
	// renderMarkdown is called, which strips raw markdown markers or applies
	// ANSI styling. The view should contain the text regardless.
	assert.True(t, strings.Contains(view, "Ruler"),
		"rendered greeting should contain 'Ruler' text")
	// The greeting should NOT appear as plain system-styled cyan text
	// (that's the default for MessageTypeSystem) — it should have its own style
	assert.True(t, chat.Messages[0].Type == MessageTypeGreeting,
		"greeting message type ensures markdown rendering path")
}

func TestAddGreetingMessage_SetsAutoScroll(t *testing.T) {
	chat := NewChatComponent(80, 20, true)
	chat.AutoScroll = false
	chat.UserScrolled = true

	chat.AddGreetingMessage("Welcome")

	assert.True(t, chat.AutoScroll, "AddGreetingMessage should enable AutoScroll")
	assert.False(t, chat.UserScrolled, "AddGreetingMessage should reset UserScrolled")
}

func TestAddGreetingMessage_RespectsScrollLock(t *testing.T) {
	chat := NewChatComponent(80, 20, true)
	chat.ScrollLocked = true
	chat.AutoScroll = false
	chat.UserScrolled = true

	chat.AddGreetingMessage("Welcome")

	assert.False(t, chat.AutoScroll, "AddGreetingMessage should not change AutoScroll when scroll-locked")
	assert.True(t, chat.UserScrolled, "AddGreetingMessage should not reset UserScrolled when scroll-locked")
}

// --- Welcome screen tests ---

func newTestTabManager() TabManager {
	defs, _ := ministers.LoadMinisters()
	return NewTabManager(80, 24, true, func() string { return "insert" }, defs)
}

func TestTabManager_WelcomeDefaultTrue(t *testing.T) {
	tm := newTestTabManager()
	assert.True(t, tm.IsWelcome(), "new TabManager should start in welcome state")
}

func TestTabManager_DismissWelcome(t *testing.T) {
	tm := newTestTabManager()
	assert.True(t, tm.IsWelcome())

	tm.DismissWelcome()
	assert.False(t, tm.IsWelcome(), "welcome should be dismissed after DismissWelcome")
	assert.Equal(t, 0, tm.activeTab, "active tab should be 0 after dismiss")
}

func TestTabManager_RenderWelcome(t *testing.T) {
	tm := newTestTabManager()

	view := tm.renderWelcome(80, 24)
	assert.NotEmpty(t, view)
	assert.Contains(t, view, "Asimi")
	assert.Contains(t, view, "INSERT")
}

func TestTabManager_RenderWelcome_UpdateAvailable(t *testing.T) {
	tm := newTestTabManager()
	tm.getUpdateAvail = func() bool { return true }

	view := tm.renderWelcome(80, 24)
	assert.Contains(t, view, "Update available")
	assert.Contains(t, view, ":update")
}

func TestTabManager_RenderWelcome_NoUpdateWhenFalse(t *testing.T) {
	tm := newTestTabManager()
	tm.getUpdateAvail = func() bool { return false }

	view := tm.renderWelcome(80, 24)
	assert.NotContains(t, view, "Update available")
}

func TestTabManager_RenderWelcome_ConfigCreated(t *testing.T) {
	tm := newTestTabManager()
	tm.getConfigCreated = func() bool { return true }

	view := tm.renderWelcome(80, 24)
	assert.Contains(t, view, "config file created")
}

func TestTabManager_RenderTabBar_NoActiveInWelcome(t *testing.T) {
	tm := newTestTabManager()
	assert.True(t, tm.IsWelcome())

	bar := tm.RenderTabBar(80)
	assert.NotEmpty(t, bar)
	// In welcome state, no tab should be bold (active)
	// We can't easily assert style, but verify the labels are present
	assert.Contains(t, bar, "Chancellor")
	assert.Contains(t, bar, "Sage")
	assert.Contains(t, bar, "Forge")
	assert.Contains(t, bar, "Judge")
}

func TestTabManager_RenderTabBar_ActiveAfterDismiss(t *testing.T) {
	tm := newTestTabManager()
	tm.DismissWelcome()

	bar := tm.RenderTabBar(80)
	assert.NotEmpty(t, bar)
	assert.Contains(t, bar, "Chancellor")
	//TODO: need to asser it's color is highlighted
}

func TestTabManager_DefaultTabsUseKanjiLabels(t *testing.T) {
	tm := newTestTabManager()
	bar := tm.RenderTabBar(80)
	// Labels should be derived from defs (Kanji + " " + Title)
	assert.Contains(t, bar, "宰相")
	assert.Contains(t, bar, "聖人")
	assert.Contains(t, bar, "工部")
	assert.Contains(t, bar, "刑部")
}

func TestTabManager_DefaultTabIDs(t *testing.T) {
	tm := newTestTabManager()
	assert.Equal(t, "chancellor", string(tm.tabs[0].Type))
	assert.Equal(t, "chancellor", tm.tabs[0].Target)
	assert.Equal(t, "sage", string(tm.tabs[1].Type))
	assert.Equal(t, "sage", tm.tabs[1].Target)
	assert.Equal(t, "forge", string(tm.tabs[2].Type))
	assert.Equal(t, "forge", tm.tabs[2].Target)
	assert.Equal(t, "judge", string(tm.tabs[3].Type))
	assert.Equal(t, "judge", tm.tabs[3].Target)
}

func TestHandleTabNewCommand_DefaultOpensSageTabWithDerivedLabel(t *testing.T) {
	model := newTestModel(t)
	initialTabCount := len(model.tabs.tabs)

	handleTabNewCommand(model, []string{})

	assert.Len(t, model.tabs.tabs, initialTabCount+1)
	newTab := model.tabs.tabs[initialTabCount]
	assert.Equal(t, "sage", string(newTab.Type))
	assert.Equal(t, "sage", newTab.Target)
	// Label should be derived from defs, not hardcoded "Sage"
	assert.Contains(t, newTab.Label, "Sage")
}

func TestHandleTabNewCommand_SageArgOpensSageTab(t *testing.T) {
	model := newTestModel(t)
	initialTabCount := len(model.tabs.tabs)

	handleTabNewCommand(model, []string{"sage"})

	assert.Len(t, model.tabs.tabs, initialTabCount+1)
	newTab := model.tabs.tabs[initialTabCount]
	assert.Equal(t, "sage", string(newTab.Type))
	assert.Equal(t, "sage", newTab.Target)
	assert.Contains(t, newTab.Label, "Sage")
}
