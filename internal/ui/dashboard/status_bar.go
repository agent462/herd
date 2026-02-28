package dashboard

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

// renderStatusBar builds the bottom status bar showing connection counts and keybind hints.
func renderStatusBar(totalHosts, connectedHosts int, width int, groupName string) string {
	left := fmt.Sprintf(" %d hosts", totalHosts)
	if groupName != "" {
		left = fmt.Sprintf(" %s: %d hosts", groupName, totalHosts)
	}

	connStr := statusConnected.Render(fmt.Sprintf("%d connected", connectedHosts))
	disconnected := totalHosts - connectedHosts
	disconnStr := ""
	if disconnected > 0 {
		disconnStr = statusDisconnected.Render(fmt.Sprintf(" %d disconnected", disconnected))
	}

	left += " │ " + connStr + disconnStr

	// Build right-side hints, dropping lowest-priority items (from the end)
	// when they don't fit alongside the left side.
	type hint struct{ key, desc string }
	// Ordered by priority — first items survive at narrow widths.
	hints := []hint{
		{"q", "quit"},
		{"Tab", "focus"},
		{"[ ]", "tabs"},
		{"?", "help"},
		{"f", "filter"},
		{"d", "diff"},
	}

	rightPadding := 1 // trailing space
	stylePadding := statusBarStyle.GetHorizontalPadding()
	avail := width - lipgloss.Width(left) - rightPadding - stylePadding
	right := ""
	for _, h := range hints {
		item := "  " + helpKeyStyle.Render(h.key) + helpDescStyle.Render(" "+h.desc)
		if lipgloss.Width(right)+lipgloss.Width(item) > avail {
			break
		}
		right += item
	}
	right += " "

	// Pad middle to fill the content area (total width minus style padding).
	contentWidth := width - stylePadding
	gap := contentWidth - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	middle := fmt.Sprintf("%*s", gap, "")

	return statusBarStyle.Width(width).Render(left + middle + right)
}

// renderHelpOverlay builds a full-screen help overlay.
func renderHelpOverlay(width, height int) string {
	help := `
  Keyboard Shortcuts
  ──────────────────

  Tab          Cycle focus: hosts → output → input
  q / Ctrl+C   Quit (when not typing)
  j / k        Navigate host table up/down
  Enter        Host table: jump to host tab
               Command input: execute command
  Esc          Close overlay / back to diff tab
  [ / ]        Previous / next output tab
  1-9          Jump to output tab by number
  f            Toggle host filter bar
  d            Show diff for selected divergent host
  ?            Toggle this help

  Selectors (in command input)
  ────────────────────────────
  @all         All hosts (default)
  @ok          Hosts in norm group
  @differs     Hosts that differ from norm
  @failed      Failed hosts (errors + non-zero exit)
  @timeout     Timed out hosts
  @pattern*    Glob match on host names
`

	style := lipgloss.NewStyle().
		Width(width-4).
		Height(height-2).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorCyan)

	return style.Render(help)
}
