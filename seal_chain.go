package main

import (
	"fmt"
	"strings"

	"github.com/afittestide/asimi/storage"
	"github.com/charmbracelet/lipgloss"
)

// Styles shared by the seal chain renderer
var (
	activeStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F4DB53"))
	labelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
)

// renderSealChain displays the seal chain status for an edict
func renderSealChain(seals []storage.Seal, w int) string {
	var b strings.Builder

	// Define required seals in order
	requiredMinisters := []string{"judge", "sage", "ruler"}
	ministerTitles := map[string]string{
		"judge": "Judge",
		"sage":  "Sage",
		"ruler": "Ruler",
	}

	// Build set of granted seals
	granted := make(map[string]bool)
	for _, seal := range seals {
		granted[seal.MinisterID] = true
	}

	// Render each seal
	for i, ministerID := range requiredMinisters {
		if i > 0 {
			b.WriteString(" ")
		}

		title := ministerTitles[ministerID]
		if granted[ministerID] {
			b.WriteString(activeStyle.Render(fmt.Sprintf("[✓ %s]", title)))
		} else {
			b.WriteString(labelStyle.Render(fmt.Sprintf("[○ %s]", title)))
		}
	}

	return lipgloss.NewStyle().Width(w).Render(b.String())
}
