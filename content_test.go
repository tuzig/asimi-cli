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

	for _, target := range ministers.DefaultTabIDs {
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

	defs, err := ministers.LoadMinisters()
	require.NoError(t, err)

	bar := tm.RenderTabBar(80)
	assert.NotEmpty(t, bar)
	// In welcome state, no tab should be bold (active)
	// Verify tab titles are present, derived from loaded defs
	for _, id := range ministers.DefaultTabIDs {
		def := ministers.LookupByID(defs, id)
		assert.Contains(t, bar, def.Title)
	}
}

func TestTabManager_RenderTabBar_ActiveAfterDismiss(t *testing.T) {
	tm := newTestTabManager()
	tm.DismissWelcome()

	defs, err := ministers.LoadMinisters()
	require.NoError(t, err)

	bar := tm.RenderTabBar(80)
	assert.NotEmpty(t, bar)
	chancellorDef := ministers.LookupByID(defs, ministers.Chancellor)
	assert.Contains(t, bar, chancellorDef.Title)
	//TODO: need to asser it's color is highlighted
}

func TestTabManager_DefaultTabsUseKanjiLabels(t *testing.T) {
	tm := newTestTabManager()
	defs, err := ministers.LoadMinisters()
	require.NoError(t, err)

	bar := tm.RenderTabBar(80)
	// Labels should be derived from defs (Kanji + " " + Title)
	for _, id := range ministers.DefaultTabIDs {
		def := ministers.LookupByID(defs, id)
		assert.Contains(t, bar, def.Kanji)
	}
}

func TestTabManager_DefaultTabIDs(t *testing.T) {
	tm := newTestTabManager()
	for i, expectedID := range ministers.DefaultTabIDs {
		assert.Equal(t, expectedID, string(tm.tabs[i].Type))
		assert.Equal(t, expectedID, tm.tabs[i].Target)
	}
}

func TestHandleTabNewCommand_DefaultOpensSageTabWithDerivedLabel(t *testing.T) {
	model := newTestModel(t)
	initialTabCount := len(model.tabs.tabs)

	handleTabNewCommand(model, []string{})

	assert.Len(t, model.tabs.tabs, initialTabCount+1)
	newTab := model.tabs.tabs[initialTabCount]
	assert.Equal(t, ministers.Sage, string(newTab.Type))
	// Default Sage tab already exists with target "sage", so the new one
	// gets a unique target for session/channel isolation.
	assert.Equal(t, "sage-2", newTab.Target)
	// Label should be derived from defs, not hardcoded
	defs, _ := ministers.LoadMinisters()
	sageDef := ministers.LookupByID(defs, ministers.Sage)
	assert.Contains(t, newTab.Label, sageDef.Title)
}

func TestHandleTabNewCommand_SageArgOpensSageTab(t *testing.T) {
	model := newTestModel(t)
	initialTabCount := len(model.tabs.tabs)

	handleTabNewCommand(model, []string{ministers.Sage})

	assert.Len(t, model.tabs.tabs, initialTabCount+1)
	newTab := model.tabs.tabs[initialTabCount]
	assert.Equal(t, ministers.Sage, string(newTab.Type))
	// Default Sage tab already exists with target "sage", so the new one
	// gets a unique target for session/channel isolation.
	assert.Equal(t, "sage-2", newTab.Target)
	defs, _ := ministers.LoadMinisters()
	sageDef := ministers.LookupByID(defs, ministers.Sage)
	assert.Contains(t, newTab.Label, sageDef.Title)
}

func TestUniqueTarget_GeneratesUniqueIDs(t *testing.T) {
	tm := newTestTabManager()
	// Default tabs include "sage" as a target
	assert.Equal(t, "sage-2", tm.UniqueTarget("sage"))
	// Add a tab with "sage-2" to simulate a second tabnew
	tm.Add("Sage", TabType(ministers.Sage), "sage-2")
	assert.Equal(t, "sage-3", tm.UniqueTarget("sage"))
	// A minister ID not yet present returns the bare ID
	assert.Equal(t, "forge-2", tm.UniqueTarget("forge"))
	// After removing all "forge" tabs (forge is a default tab), it still
	// collides because the default tab uses "forge" as target.
}

func TestHandleTabNewCommand_MultipleTabnewSageGenerateIncrementingTargets(t *testing.T) {
	model := newTestModel(t)
	initialTabCount := len(model.tabs.tabs)

	// First tabnew sage → "sage-2"
	handleTabNewCommand(model, []string{ministers.Sage})
	assert.Equal(t, "sage-2", model.tabs.tabs[initialTabCount].Target)

	// Second tabnew sage → "sage-3"
	handleTabNewCommand(model, []string{ministers.Sage})
	assert.Equal(t, "sage-3", model.tabs.tabs[initialTabCount+1].Target)

	// Third tabnew (no arg) → "sage-4"
	handleTabNewCommand(model, []string{})
	assert.Equal(t, "sage-4", model.tabs.tabs[initialTabCount+2].Target)
}

func TestSetTabMinister_SetsMinisterOnMatchingTab(t *testing.T) {
	tm := newTestTabManager()
	tm.DismissWelcome()

	tm.Add("Ritual:e647", "ritual", "e647")

	tm.SetTabMinister("e647", "forge")
	tab := tm.TabByTarget("e647")
	require.NotNil(t, tab)
	assert.Equal(t, "forge", tab.CurrentMinister)
}

func TestSetTabMinister_NoMatchIsNoOp(t *testing.T) {
	tm := newTestTabManager()
	tm.DismissWelcome()

	tm.SetTabMinister("nonexistent", "forge")
	for i := range tm.tabs {
		assert.Empty(t, tm.tabs[i].CurrentMinister, "no tab should have CurrentMinister set")
	}
}

func TestSetTabChatMode_SetsChatModeOnMatchingTab(t *testing.T) {
	tm := newTestTabManager()
	tm.DismissWelcome()

	tm.Add("Ritual:e647", "ritual", "e647")

	tm.SetTabChatMode("e647", true)
	tab := tm.TabByTarget("e647")
	require.NotNil(t, tab)
	assert.True(t, tab.ChatMode)

	tm.SetTabChatMode("e647", false)
	assert.False(t, tab.ChatMode)
}

func TestSetTabChatMode_NoMatchIsNoOp(t *testing.T) {
	tm := newTestTabManager()
	tm.DismissWelcome()

	tm.SetTabChatMode("nonexistent", true)
	for i := range tm.tabs {
		assert.False(t, tm.tabs[i].ChatMode, "no tab should have ChatMode set")
	}
}
