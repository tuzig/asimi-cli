package main

import (
	"strings"
	"testing"

	"github.com/afittestide/asimi/storage"
	"github.com/stretchr/testify/assert"
)

func TestNewEdictSelectWindowDefaults(t *testing.T) {
	window := NewEdictSelectWindow()

	assert.Equal(t, 70, window.Width)
	assert.Equal(t, 15, window.Height)
	assert.Empty(t, window.Items)
}

func TestEdictSelectWindowRenderList_ShowsBothSeals(t *testing.T) {
	window := NewEdictSelectWindow()
	window.SetSize(80, 10)

	edicts := []storage.ActiveEdict{
		{Edict: storage.Edict{ID: 1, Intent: "Fix the login bug"}, HasJudgeSeal: true, HasSageSeal: true},
	}
	window.SetItems(edicts)

	render := window.RenderList(0, 0, window.GetVisibleSlots())
	lines := strings.Split(render, "\n")

	assert.True(t, strings.Contains(lines[0], "Select edict"), "Should have title")
	assert.True(t, strings.Contains(lines[1], "[  1]"), "Should show edict number")
	assert.True(t, strings.Contains(lines[1], "刑"), "Should show judge seal (刑)")
	assert.True(t, strings.Contains(lines[1], "聖"), "Should show sage seal (聖)")
	assert.True(t, strings.Contains(lines[1], "Fix the login bug"), "Should show intent")
}

func TestEdictSelectWindowRenderList_ShowsJudgeSealOnly(t *testing.T) {
	window := NewEdictSelectWindow()
	window.SetSize(80, 10)

	edicts := []storage.ActiveEdict{
		{Edict: storage.Edict{ID: 2, Intent: "Add new feature"}, HasJudgeSeal: true, HasSageSeal: false},
	}
	window.SetItems(edicts)

	render := window.RenderList(0, 0, window.GetVisibleSlots())
	lines := strings.Split(render, "\n")

	assert.True(t, strings.Contains(lines[1], "[  2]"), "Should show edict number")
	assert.True(t, strings.Contains(lines[1], "刑"), "Should show judge seal (刑)")
	assert.False(t, strings.Contains(lines[1], "聖"), "Should NOT show sage seal (聖)")
	assert.True(t, strings.Contains(lines[1], "  "), "Should have space where sage seal is absent")
}

func TestEdictSelectWindowRenderList_ShowsSageSealOnly(t *testing.T) {
	window := NewEdictSelectWindow()
	window.SetSize(80, 10)

	edicts := []storage.ActiveEdict{
		{Edict: storage.Edict{ID: 3, Intent: "Refactor code"}, HasJudgeSeal: false, HasSageSeal: true},
	}
	window.SetItems(edicts)

	render := window.RenderList(0, 0, window.GetVisibleSlots())
	lines := strings.Split(render, "\n")

	assert.True(t, strings.Contains(lines[1], "[  3]"), "Should show edict number")
	assert.False(t, strings.Contains(lines[1], "刑"), "Should NOT show judge seal (刑)")
	assert.True(t, strings.Contains(lines[1], "聖"), "Should show sage seal (聖)")
	assert.True(t, strings.Contains(lines[1], "  "), "Should have space where judge seal is absent")
}

func TestEdictSelectWindowRenderList_NoSeals(t *testing.T) {
	window := NewEdictSelectWindow()
	window.SetSize(80, 10)

	edicts := []storage.ActiveEdict{
		{Edict: storage.Edict{ID: 4, Intent: "Pending review"}, HasJudgeSeal: false, HasSageSeal: false},
	}
	window.SetItems(edicts)

	render := window.RenderList(0, 0, window.GetVisibleSlots())
	lines := strings.Split(render, "\n")

	assert.True(t, strings.Contains(lines[1], "[  4]"), "Should show edict number")
	assert.False(t, strings.Contains(lines[1], "刑"), "Should NOT show judge seal (刑)")
	assert.False(t, strings.Contains(lines[1], "聖"), "Should NOT show sage seal (聖)")
}

func TestEdictSelectWindowRenderList_Selection(t *testing.T) {
	window := NewEdictSelectWindow()
	window.SetSize(80, 10)

	edicts := []storage.ActiveEdict{
		{Edict: storage.Edict{ID: 1, Intent: "First"}, HasJudgeSeal: true, HasSageSeal: false},
		{Edict: storage.Edict{ID: 2, Intent: "Second"}, HasJudgeSeal: true, HasSageSeal: true},
	}
	window.SetItems(edicts)

	render := window.RenderList(1, 0, window.GetVisibleSlots())
	lines := strings.Split(render, "\n")

	assert.True(t, strings.HasPrefix(lines[1], "  "), "Unselected item should have space prefix: %q", lines[1])
	assert.True(t, strings.Contains(lines[1], "First"), "Unselected item should show 'First'")
	assert.True(t, strings.Contains(lines[1], "刑"), "Unselected item should show judge seal")
	assert.False(t, strings.Contains(lines[1], "聖"), "Unselected item should NOT show sage seal")

	assert.True(t, strings.HasPrefix(lines[2], "▶ "), "Selected item should have ▶ prefix: %q", lines[2])
	assert.True(t, strings.Contains(lines[2], "Second"), "Selected item should show 'Second'")
	assert.True(t, strings.Contains(lines[2], "刑"), "Selected item should show judge seal")
	assert.True(t, strings.Contains(lines[2], "聖"), "Selected item should show sage seal")
}

func TestEdictSelectWindowRenderList_Empty(t *testing.T) {
	window := NewEdictSelectWindow()
	window.SetSize(80, 10)

	window.SetItems([]storage.ActiveEdict{})

	render := window.RenderList(0, 0, window.GetVisibleSlots())

	assert.Contains(t, render, "No active edicts")
}

func TestEdictSelectWindowRenderList_MultipleEdicts(t *testing.T) {
	window := NewEdictSelectWindow()
	window.SetSize(80, 10)

	edicts := []storage.ActiveEdict{
		{Edict: storage.Edict{ID: 1, Intent: "All seals"}, HasJudgeSeal: true, HasSageSeal: true},
		{Edict: storage.Edict{ID: 2, Intent: "Judge only"}, HasJudgeSeal: true, HasSageSeal: false},
		{Edict: storage.Edict{ID: 3, Intent: "No seals"}, HasJudgeSeal: false, HasSageSeal: false},
		{Edict: storage.Edict{ID: 4, Intent: "Sage only"}, HasJudgeSeal: false, HasSageSeal: true},
	}
	window.SetItems(edicts)

	render := window.RenderList(0, 0, window.GetVisibleSlots())
	lines := strings.Split(render, "\n")

	assert.Contains(t, lines[1], "[  1]")
	assert.Contains(t, lines[1], "刑")
	assert.Contains(t, lines[1], "聖")

	assert.Contains(t, lines[2], "[  2]")
	assert.Contains(t, lines[2], "刑")
	assert.NotContains(t, lines[2], "聖")

	assert.Contains(t, lines[3], "[  3]")
	assert.NotContains(t, lines[3], "刑")
	assert.NotContains(t, lines[3], "聖")

	assert.Contains(t, lines[4], "[  4]")
	assert.NotContains(t, lines[4], "刑")
	assert.Contains(t, lines[4], "聖")
}
