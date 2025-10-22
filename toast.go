package main

import (
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Toast represents a single toast notification
type Toast struct {
	ID      string
	Message string
	Type    string // info, success, warning, error
	Created time.Time
	Timeout time.Duration
}

// ToastManager manages toast notifications
type ToastManager struct {
	Toasts []Toast
	Style  lipgloss.Style
}

// NewToastManager creates a new toast manager
func NewToastManager() ToastManager {
	return ToastManager{
		Toasts: make([]Toast, 0),
		Style: lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("230")).
			Padding(0, 1).
			MaxWidth(50),
	}
}

// AddToast adds a new toast notification
func (tm *ToastManager) AddToast(message, toastType string, timeout time.Duration) {
	toast := Toast{
		ID:      time.Now().String(), // Simple ID generation
		Message: message,
		Type:    toastType,
		Created: time.Now(),
		Timeout: timeout,
	}

	tm.Toasts = append(tm.Toasts, toast)
}

// RemoveToast removes a toast by ID
func (tm *ToastManager) RemoveToast(id string) {
	for i, toast := range tm.Toasts {
		if toast.ID == id {
			// Remove the toast at index i
			tm.Toasts = append(tm.Toasts[:i], tm.Toasts[i+1:]...)
			break
		}
	}
}

// Clear removes all existing toast notifications
func (tm *ToastManager) Clear() {
	tm.Toasts = nil
}

// Update handles updating the toast manager (e.g., removing expired toasts)
func (tm ToastManager) Update() ToastManager {
	now := time.Now()
	activeToasts := make([]Toast, 0)

	for _, toast := range tm.Toasts {
		// Check if toast is still valid
		if now.Sub(toast.Created) < toast.Timeout {
			activeToasts = append(activeToasts, toast)
		}
	}

	tm.Toasts = activeToasts
	return tm
}

// View renders the active toasts
func (tm ToastManager) View() string {
	if len(tm.Toasts) == 0 {
		return ""
	}

	// For now, just show the most recent toast
	// In a full implementation, you might show multiple toasts
	toast := tm.Toasts[len(tm.Toasts)-1]

	// Apply different styles based on toast type
	style := tm.Style
	contentWidth := lipgloss.Width(toast.Message)
	frameWidth, _ := style.GetFrameSize()
	maxWidth := style.GetMaxWidth()
	if maxWidth > 0 && contentWidth+frameWidth > maxWidth {
		style = style.Copy().MaxWidth(contentWidth + frameWidth)
	}
	switch toast.Type {
	case "success":
		style = style.Background(lipgloss.Color("76")) // Green
	case "warning":
		style = style.Background(lipgloss.Color("11")) // Yellow
	case "error":
		style = style.Background(lipgloss.Color("124")) // Red
	}
	return style.Render(toast.Message)
}
