package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func (m TUIModel) renderShogunateView(height int) string {
	if m.shogunate == nil {
		empty := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#004444")).
			Align(lipgloss.Center).
			Width(m.width)
		return lipgloss.NewStyle().
			Width(m.width).Height(height).
			Align(lipgloss.Center, lipgloss.Center).
			Render(empty.Render("Shogunate not active"))
	}

	snap := m.shogunate.TakeSnapshot()

	var b strings.Builder

	// TODO: use globalTheme
	detailStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F4DB53"))
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	sectionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00CCCC"))

	// Live rituals section
	b.WriteString(sectionStyle.Render(fmt.Sprintf(" LIVE RITUALS (%d)", len(snap.LiveRituals))))
	b.WriteString("\n")

	if len(snap.LiveRituals) == 0 {
		b.WriteString(labelStyle.Render(" No running rituals"))
		b.WriteString("\n")
	} else {
		// Column header
		b.WriteString(labelStyle.Render(fmt.Sprintf(" %-16s %-12s %-18s %s", "RITUAL", "EDICT", "STEP", "AGE")))
		b.WriteString("\n")
		for _, lr := range snap.LiveRituals {
			edictShort := lr.EdictID
			if len(edictShort) > 10 {
				edictShort = edictShort[:10]
			}
			stepInfo := fmt.Sprintf("%d/%d %s", lr.CurrentStep+1, lr.TotalSteps, lr.StepName)
			b.WriteString(detailStyle.Render(fmt.Sprintf(" %-16s %-12s %-18s %s",
				lr.RitualName, edictShort, stepInfo, formatAge(lr.Age))))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")

	// Events section
	b.WriteString(sectionStyle.Render(" EVENTS (recent)"))
	b.WriteString("\n")

	if len(snap.Events) == 0 {
		b.WriteString(labelStyle.Render(" No events recorded"))
		b.WriteString("\n")
	} else {
		// Calculate how many events we can show
		usedLines := strings.Count(b.String(), "\n") + 1
		remaining := height - usedLines
		if remaining < 1 {
			remaining = 1
		}
		shown := len(snap.Events)
		if shown > remaining {
			shown = remaining
		}
		for _, ev := range snap.Events[:shown] {
			edictShort := ev.EdictID
			if len(edictShort) > 10 {
				edictShort = edictShort[:10]
			}
			detail := ev.Detail
			if detail == "" {
				detail = "-"
			}
			b.WriteString(fmt.Sprintf(" %s  ",
				labelStyle.Render(ev.Time.Format("15:04:05"))))
			b.WriteString(fmt.Sprintf("%-18s ",
				detailStyle.Render(ev.EventType)))
			b.WriteString(fmt.Sprintf("%-16s %s",
				detailStyle.Render(detail),
				labelStyle.Render(edictShort)))
			b.WriteString("\n")
		}
	}

	return lipgloss.NewStyle().
		Width(m.width).
		Height(height).
		Render(b.String())
}

func formatAge(d time.Duration) string {
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}
