package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ListNavigator is the interface that ContentComponent uses for list navigation.
// Any select window that wants to participate in unified list navigation implements this.
type ListNavigator interface {
	GetItemCount() int
	GetVisibleSlots() int
	NavNext(current int) int
	NavPrev(current int) int
	NavFirst() int
	NavLast() int
	NavNearest(index int) int // find nearest selectable to index
}

// SelectWindow is a generic component for displaying a selectable list of items
type SelectWindow[T any] struct {
	Width        int
	Height       int
	Items        []T
	Loading      bool
	Error        error
	MaxVisible   int
	isSelectable func(T) bool // stored filter for ListNavigator methods
}

// NewSelectWindow creates a new generic select window
func NewSelectWindow[T any]() SelectWindow[T] {
	return SelectWindow[T]{
		Width:  70,
		Height: 15,
		Items:  []T{},
	}
}

// SetSize updates the dimensions
func (s *SelectWindow[T]) SetSize(width, height int) {
	s.Width = width
	s.Height = height
	// Account for title line
	s.MaxVisible = height - 1
	if s.MaxVisible < 1 {
		s.MaxVisible = 1
	}
}

// SetItems updates the items list
func (s *SelectWindow[T]) SetItems(items []T) {
	s.Items = items
	s.Loading = false
	s.Error = nil
}

// SetLoading sets loading state
func (s *SelectWindow[T]) SetLoading(loading bool) {
	s.Loading = loading
	if loading {
		s.Error = nil
	}
}

// SetError sets error state
func (s *SelectWindow[T]) SetError(err error) {
	s.Error = err
	s.Loading = false
}

// GetItemCount returns the number of items
func (s *SelectWindow[T]) GetItemCount() int {
	return len(s.Items)
}

// GetVisibleSlots returns how many items can be shown at once
func (s *SelectWindow[T]) GetVisibleSlots() int {
	return s.MaxVisible
}

// GetSelectedItem returns the item at the given index
func (s *SelectWindow[T]) GetSelectedItem(index int) *T {
	if index < 0 || index >= len(s.Items) {
		return nil
	}
	return &s.Items[index]
}

// NextSelectableIndex returns the next selectable index after current, or current if none found
// isSelectable is a function that returns true if an item can be selected
func (s *SelectWindow[T]) NextSelectableIndex(current int, isSelectable func(T) bool) int {
	if isSelectable == nil {
		// All items selectable
		if current < len(s.Items)-1 {
			return current + 1
		}
		return current
	}

	for i := current + 1; i < len(s.Items); i++ {
		if isSelectable(s.Items[i]) {
			return i
		}
	}
	return current // Stay at current if no selectable item found
}

// PrevSelectableIndex returns the previous selectable index before current, or current if none found
func (s *SelectWindow[T]) PrevSelectableIndex(current int, isSelectable func(T) bool) int {
	if isSelectable == nil {
		// All items selectable
		if current > 0 {
			return current - 1
		}
		return current
	}

	for i := current - 1; i >= 0; i-- {
		if isSelectable(s.Items[i]) {
			return i
		}
	}
	return current // Stay at current if no selectable item found
}

// FirstSelectableIndex returns the first selectable index, or 0 if none found
func (s *SelectWindow[T]) FirstSelectableIndex(isSelectable func(T) bool) int {
	if isSelectable == nil || len(s.Items) == 0 {
		return 0
	}

	for i := 0; i < len(s.Items); i++ {
		if isSelectable(s.Items[i]) {
			return i
		}
	}
	return 0
}

// LastSelectableIndex returns the last selectable index, or len-1 if none found
func (s *SelectWindow[T]) LastSelectableIndex(isSelectable func(T) bool) int {
	if len(s.Items) == 0 {
		return 0
	}
	if isSelectable == nil {
		return len(s.Items) - 1
	}

	for i := len(s.Items) - 1; i >= 0; i-- {
		if isSelectable(s.Items[i]) {
			return i
		}
	}
	return len(s.Items) - 1
}

// CountSelectableItems returns the number of selectable items
func (s *SelectWindow[T]) CountSelectableItems(isSelectable func(T) bool) int {
	if isSelectable == nil {
		return len(s.Items)
	}

	count := 0
	for _, item := range s.Items {
		if isSelectable(item) {
			count++
		}
	}
	return count
}

// SetSelectable stores the selectability filter for ListNavigator methods.
func (s *SelectWindow[T]) SetSelectable(fn func(T) bool) {
	s.isSelectable = fn
}

// NavNext implements ListNavigator using the stored isSelectable filter.
func (s *SelectWindow[T]) NavNext(current int) int {
	return s.NextSelectableIndex(current, s.isSelectable)
}

// NavPrev implements ListNavigator using the stored isSelectable filter.
func (s *SelectWindow[T]) NavPrev(current int) int {
	return s.PrevSelectableIndex(current, s.isSelectable)
}

// NavFirst implements ListNavigator using the stored isSelectable filter.
func (s *SelectWindow[T]) NavFirst() int {
	return s.FirstSelectableIndex(s.isSelectable)
}

// NavLast implements ListNavigator using the stored isSelectable filter.
func (s *SelectWindow[T]) NavLast() int {
	return s.LastSelectableIndex(s.isSelectable)
}

// NavNearest finds the nearest selectable index to the given index.
// Searches forward first, then backward.
func (s *SelectWindow[T]) NavNearest(index int) int {
	if index < 0 {
		index = 0
	}
	if index >= len(s.Items) {
		index = len(s.Items) - 1
	}
	if s.isSelectable == nil || len(s.Items) == 0 {
		return index
	}
	if s.isSelectable(s.Items[index]) {
		return index
	}
	// Search forward
	for i := index + 1; i < len(s.Items); i++ {
		if s.isSelectable(s.Items[i]) {
			return i
		}
	}
	// Search backward
	for i := index - 1; i >= 0; i-- {
		if s.isSelectable(s.Items[i]) {
			return i
		}
	}
	return index
}

// RenderConfig holds callbacks for customization
type RenderConfig[T any] struct {
	Title string

	// Optional overrides
	ConstructTitle func(selectedIndex, totalItems int) string
	OnLoading      func(sb *strings.Builder)
	OnError        func(sb *strings.Builder, err error)
	OnEmpty        func(sb *strings.Builder)

	// CustomState allows checking for extra states (e.g. loadingSession)
	// Returns true if it handled the rendering (stopping normal flow)
	CustomState func(sb *strings.Builder) bool

	// RenderItem renders a single item
	// index is the absolute index in the Items slice
	RenderItem func(i int, item T, isSelected bool, sb *strings.Builder)

	// IsSelectable returns whether an item can be selected
	// If nil, all items are selectable
	// Non-selectable items are skipped during navigation
	IsSelectable func(item T) bool
}

// Render renders the list with the given selection and configuration
func (s *SelectWindow[T]) Render(selectedIndex, scrollOffset int, config RenderConfig[T]) string {
	// Title Style (Common)
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#F952F9")).
		Background(lipgloss.Color("#000000")).
		Padding(0, 1)

	totalItems := len(s.Items)

	// Render Title
	var title string
	if config.ConstructTitle != nil {
		title = config.ConstructTitle(selectedIndex, totalItems)
	} else {
		title = titleStyle.Render(fmt.Sprintf("%s [%3d/%3d]:", config.Title, selectedIndex+1, totalItems))
	}

	var sb strings.Builder

	// 1. Check Loading
	if s.Loading {
		if config.OnLoading != nil {
			config.OnLoading(&sb)
		} else {
			sb.WriteString("Loading...\n")
			sb.WriteString("\n")
			sb.WriteString("⏳ Please wait...\n")
		}
		return title + "\n" + sb.String()
	}

	// 2. Check Custom State (e.g. loadingSession)
	if config.CustomState != nil {
		if config.CustomState(&sb) {
			return title + "\n" + sb.String()
		}
	}

	// 3. Check Error
	if s.Error != nil {
		if config.OnError != nil {
			config.OnError(&sb, s.Error)
		} else {
			sb.WriteString("Error:\n")
			sb.WriteString(fmt.Sprintf("%v\n", s.Error))
		}
		return title + "\n" + sb.String()
	}

	// 4. Check Empty
	if totalItems == 0 {
		if config.OnEmpty != nil {
			config.OnEmpty(&sb)
		} else {
			sb.WriteString("No items found.\n")
		}
		return title + "\n" + sb.String()
	}

	// 5. Calculate Scroll
	if scrollOffset < 0 {
		scrollOffset = 0
	}
	maxOffset := totalItems - s.MaxVisible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if scrollOffset > maxOffset {
		scrollOffset = maxOffset
	}
	start := scrollOffset
	end := scrollOffset + s.MaxVisible
	if end > totalItems {
		end = totalItems
	}

	// 6. Loop and Render Items
	for i := start; i < end; i++ {
		isSelected := i == selectedIndex
		if config.RenderItem != nil {
			config.RenderItem(i, s.Items[i], isSelected, &sb)
		} else {
			// Default simple render
			prefix := "  "
			if isSelected {
				prefix = "▶ "
			}
			sb.WriteString(fmt.Sprintf("%s%v\n", prefix, s.Items[i]))
		}
	}
	return title + "\n" + sb.String()
}
