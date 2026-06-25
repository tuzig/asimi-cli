package main

import (
	"fmt"
	"strings"

	"github.com/afittestide/asimi/storage"
	"github.com/charmbracelet/lipgloss"
)

// renderSealChain displays the seal chain status for an edict
func renderSealChain(seals []storage.Seal, w int) string {
	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(globalTheme.ChatBorder)
	labelStyle := lipgloss.NewStyle().Foreground(globalTheme.DimTextColor)
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
