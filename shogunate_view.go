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
	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F4DB53"))
	completedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	failedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#CC4444"))
	detailStyle := lipgloss.NewStyle().Foreground(globalTheme.TextColor)
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	sectionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00CCCC"))

	// Split available rows evenly: each section gets header + content rows
	// 2 section headers + 1 blank separator = 3 fixed lines
	halfHeight := (height - 3) / 2
	if halfHeight < 2 {
		halfHeight = 2
	}

	// Ritual log section
	b.WriteString(sectionStyle.Render(" RITUAL LOG"))
	b.WriteString("\n")

	if len(snap.Rituals) == 0 {
		b.WriteString(labelStyle.Render(" No rituals recorded"))
		b.WriteString("\n")
	} else {
		shown := len(snap.Rituals)
		if shown > halfHeight {
			shown = halfHeight
		}
		for _, r := range snap.Rituals[:shown] {
			edictShort := r.EdictID
			if len(edictShort) > 10 {
				edictShort = edictShort[:10]
			}
			stepInfo := fmt.Sprintf("%d/%d %s", r.CurrentStep+1, r.TotalSteps, r.StepName)

			var style lipgloss.Style
			switch {
			case r.State == "pending" || r.State == "running":
				style = activeStyle
			case r.State == "failed" || r.State == "aborted":
				style = failedStyle
			default:
				style = completedStyle
			}

			b.WriteString(fmt.Sprintf(" %s  ",
				labelStyle.Render(r.StartedAt.Format("15:04:05"))))
			b.WriteString(fmt.Sprintf("%-16s ",
				style.Render(r.RitualName)))
			b.WriteString(fmt.Sprintf("%-10s %-18s %s",
				style.Render(string(r.State)), style.Render(stepInfo),
				labelStyle.Render(edictShort)))
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
		shown := len(snap.Events)
		if shown > halfHeight {
			shown = halfHeight
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
