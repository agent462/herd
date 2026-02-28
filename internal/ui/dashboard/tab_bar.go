package dashboard

import (
	"charm.land/lipgloss/v2"
)

// tab represents a single tab in the tab bar.
type tab struct {
	Label string // display label (truncated if needed)
	ID    string // "diff" or hostname
}

// tabBar manages a horizontal row of tabs with overflow scrolling.
type tabBar struct {
	tabs   []tab
	active int // index of active tab
	offset int // first visible tab index (for overflow scrolling)
	width  int // available width for rendering
}

func newTabBar(width int) tabBar {
	return tabBar{
		tabs:  []tab{{Label: "Diff Output", ID: "diff"}},
		width: width,
	}
}

// SetTabs rebuilds the tab list: ["Diff Output", host1, host2, ...].
// Preserves the current active tab if it still exists; otherwise resets to 0.
func (tb *tabBar) SetTabs(hosts []string) {
	prevID := tb.ActiveID()

	tb.tabs = make([]tab, 0, len(hosts)+1)
	tb.tabs = append(tb.tabs, tab{Label: "Diff Output", ID: "diff"})
	for _, h := range hosts {
		tb.tabs = append(tb.tabs, tab{Label: truncLabel(h, 16), ID: h})
	}

	// Try to preserve active tab.
	tb.active = 0
	for i, t := range tb.tabs {
		if t.ID == prevID {
			tb.active = i
			break
		}
	}
	tb.ensureVisible()
}

// Next moves to the next tab, wrapping around.
func (tb *tabBar) Next() {
	if len(tb.tabs) == 0 {
		return
	}
	tb.active = (tb.active + 1) % len(tb.tabs)
	tb.ensureVisible()
}

// Prev moves to the previous tab, wrapping around.
func (tb *tabBar) Prev() {
	if len(tb.tabs) == 0 {
		return
	}
	tb.active = (tb.active - 1 + len(tb.tabs)) % len(tb.tabs)
	tb.ensureVisible()
}

// SetActive jumps to a tab by index (clamped to valid range).
func (tb *tabBar) SetActive(index int) {
	if len(tb.tabs) == 0 {
		return
	}
	if index < 0 {
		index = 0
	}
	if index >= len(tb.tabs) {
		index = len(tb.tabs) - 1
	}
	tb.active = index
	tb.ensureVisible()
}

// SetActiveByID activates the tab matching the given ID.
// Returns true if found.
func (tb *tabBar) SetActiveByID(id string) bool {
	for i, t := range tb.tabs {
		if t.ID == id {
			tb.active = i
			tb.ensureVisible()
			return true
		}
	}
	return false
}

// ActiveID returns the ID of the currently active tab.
func (tb *tabBar) ActiveID() string {
	if tb.active < 0 || tb.active >= len(tb.tabs) {
		return "diff"
	}
	return tb.tabs[tb.active].ID
}

// ActiveIndex returns the index of the currently active tab.
func (tb *tabBar) ActiveIndex() int {
	return tb.active
}

// Resize updates the available width for rendering.
func (tb *tabBar) Resize(width int) {
	tb.width = width
	tb.ensureVisible()
}

// View renders the tab bar as a single styled line.
func (tb *tabBar) View() string {
	if len(tb.tabs) == 0 {
		return ""
	}

	avail := tb.width
	if avail <= 0 {
		return ""
	}

	// Determine which tabs are visible.
	showLeftArrow := tb.offset > 0
	showRightArrow := false

	// Reserve space for arrows.
	arrowWidth := 2 // "◀ " or " ▶"
	renderWidth := avail
	if showLeftArrow {
		renderWidth -= arrowWidth
	}

	// Build visible tabs from offset, stopping when we run out of width.
	var parts []string
	usedWidth := 0

	for i := tb.offset; i < len(tb.tabs); i++ {
		var rendered string
		if i == tb.active {
			rendered = tabActiveStyle.Render(tb.tabs[i].Label)
		} else {
			rendered = tabInactiveStyle.Render(tb.tabs[i].Label)
		}

		w := lipgloss.Width(rendered)
		isLast := i == len(tb.tabs)-1
		// Only reserve space for a right arrow when there are more tabs after this one.
		rightReserve := arrowWidth
		if isLast {
			rightReserve = 0
		}
		if usedWidth+w+rightReserve > renderWidth && i > tb.offset {
			showRightArrow = true
			break
		}
		parts = append(parts, rendered)
		usedWidth += w
	}

	var result string
	if showLeftArrow {
		result = tabScrollIndicator.Render("◀ ")
	}
	result += lipgloss.JoinHorizontal(lipgloss.Bottom, parts...)
	if showRightArrow {
		result += tabScrollIndicator.Render(" ▶")
	}

	return tabBarStyle.Width(avail).Render(result)
}

// ensureVisible adjusts offset so the active tab is visible.
func (tb *tabBar) ensureVisible() {
	if tb.active < tb.offset {
		tb.offset = tb.active
		return
	}

	// Walk forward from offset, summing widths, to check if active is visible.
	avail := tb.width
	if avail <= 0 {
		return
	}

	arrowWidth := 2
	renderWidth := avail
	if tb.offset > 0 {
		renderWidth -= arrowWidth
	}

	usedWidth := 0
	for i := tb.offset; i < len(tb.tabs); i++ {
		var rendered string
		if i == tb.active {
			rendered = tabActiveStyle.Render(tb.tabs[i].Label)
		} else {
			rendered = tabInactiveStyle.Render(tb.tabs[i].Label)
		}
		w := lipgloss.Width(rendered)

		if i == tb.active {
			// Only reserve right-arrow space when there are more tabs after this one.
			rightReserve := arrowWidth
			if i == len(tb.tabs)-1 {
				rightReserve = 0
			}
			if usedWidth+w+rightReserve > renderWidth {
				// Active tab doesn't fit; increase offset.
				tb.offset++
				tb.ensureVisible()
				return
			}
			return // active tab is visible
		}
		usedWidth += w
	}
}

// truncLabel shortens a label to maxLen characters with an ellipsis.
func truncLabel(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
