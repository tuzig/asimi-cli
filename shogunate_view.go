package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/afittestide/asimi/shogunate"
	"github.com/afittestide/asimi/storage"
	"github.com/charmbracelet/lipgloss"
)

// Styles shared across panes
var (
	activeStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F4DB53"))
	completedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	failedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#CC4444"))
	urgentStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF6633"))
	labelStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	sectionStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#00CCCC"))
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

	// Height allocation: zhengming gets min 2 lines, +1 per entry, capped at height/3
	zhengH := 2 + len(snap.Zhengming)
	maxZheng := height / 3
	if maxZheng < 2 {
		maxZheng = 2
	}
	if zhengH > maxZheng {
		zhengH = maxZheng
	}
	bottomH := height - zhengH
	if bottomH < 2 {
		bottomH = 2
	}
	halfW := m.width / 2

	zhengPane := renderZhengmingPane(snap, m.width, zhengH)
	eventsPane := renderEventsPane(snap, halfW, bottomH)
	ritualsPane := renderRitualsPane(snap, m.width-halfW, bottomH)

	bottom := lipgloss.JoinHorizontal(lipgloss.Top, eventsPane, ritualsPane)
	return lipgloss.JoinVertical(lipgloss.Left, zhengPane, bottom)
}

func renderZhengmingPane(snap shogunate.Snapshot, w, h int) string {
	var b strings.Builder
	b.WriteString(sectionStyle.Render(" ZHENGMING"))
	b.WriteString("\n")

	if len(snap.Zhengming) == 0 {
		b.WriteString(labelStyle.Render(" No pending requests"))
		b.WriteString("\n")
	} else {
		maxRows := h - 1 // header takes 1 line
		shown := len(snap.Zhengming)
		if shown > maxRows {
			shown = maxRows
		}
		for _, z := range snap.Zhengming[:shown] {
			style := labelStyle
			priorityMark := " "
			if z.Priority == "urgent" {
				style = urgentStyle
				priorityMark = "!"
			}
			question := "-"
			if len(z.Questions) > 0 {
				question = z.Questions[0]
			}
			maxQ := w - 32 // space for time + minister + priority
			if maxQ < 10 {
				maxQ = 10
			}
			if len(question) > maxQ {
				question = question[:maxQ-3] + "..."
			}
			minister := z.MinisterID
			if len(minister) > 10 {
				minister = minister[:10]
			}
			b.WriteString(fmt.Sprintf(" %s %s %-10s %s",
				labelStyle.Render(z.CreatedAt.Format("15:04:05")),
				style.Render(priorityMark),
				style.Render(minister),
				style.Render(question)))
			b.WriteString("\n")
		}
	}

	return lipgloss.NewStyle().Width(w).Height(h).Render(b.String())
}

func renderEventsPane(snap shogunate.Snapshot, w, h int) string {
	detailStyle := lipgloss.NewStyle().Foreground(globalTheme.TextColor)
	var b strings.Builder
	b.WriteString(sectionStyle.Render(" EVENTS"))
	b.WriteString("\n")

	if len(snap.Events) == 0 {
		b.WriteString(labelStyle.Render(" No events recorded"))
		b.WriteString("\n")
	} else {
		maxRows := h - 1
		shown := len(snap.Events)
		if shown > maxRows {
			shown = maxRows
		}
		for _, ev := range snap.Events[:shown] {
			detail := ev.Detail
			if detail == "" {
				detail = "-"
			}
			maxDetail := w - 30
			if maxDetail < 5 {
				maxDetail = 5
			}
			if len(detail) > maxDetail {
				detail = detail[:maxDetail-3] + "..."
			}
			evType := ev.EventType
			if len(evType) > 16 {
				evType = evType[:16]
			}
			b.WriteString(fmt.Sprintf(" %s %-16s %s",
				labelStyle.Render(ev.Time.Format("15:04:05")),
				detailStyle.Render(evType),
				detailStyle.Render(detail)))
			b.WriteString("\n")
		}
	}

	return lipgloss.NewStyle().Width(w).Height(h).Render(b.String())
}

func renderRitualsPane(snap shogunate.Snapshot, w, h int) string {
	var b strings.Builder
	b.WriteString(sectionStyle.Render(" RITUALS"))
	b.WriteString("\n")

	if len(snap.Rituals) == 0 {
		b.WriteString(labelStyle.Render(" No rituals recorded"))
		b.WriteString("\n")
	} else {
		maxRows := h - 1
		shown := len(snap.Rituals)
		if shown > maxRows {
			shown = maxRows
		}
		for _, r := range snap.Rituals[:shown] {
			stepInfo := fmt.Sprintf("%d/%d", r.CurrentStep+1, r.TotalSteps+1)

			var style lipgloss.Style
			switch {
			case r.State == "pending" || r.State == "running":
				style = activeStyle
			case r.State == "failed" || r.State == "aborted":
				style = failedStyle
			default:
				style = completedStyle
			}

			name := r.RitualName
			maxName := w - 26
			if maxName < 8 {
				maxName = 8
			}
			if len(name) > maxName {
				name = name[:maxName-3] + "..."
			}

			b.WriteString(fmt.Sprintf(" %s %-8s %s %s",
				labelStyle.Render(r.StartedAt.Format("15:04")),
				style.Render(name),
				style.Render(fmt.Sprintf("%-8s", string(r.State))),
				labelStyle.Render(stepInfo)))
			b.WriteString("\n")
		}
	}

	return lipgloss.NewStyle().Width(w).Height(h).Render(b.String())
}

func formatAge(d time.Duration) string {
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm%02ds", m, s)
}

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
