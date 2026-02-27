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

	right := helpKeyStyle.Render("Tab") + helpDescStyle.Render(" focus") +
		"  " + helpKeyStyle.Render("f") + helpDescStyle.Render(" filter") +
		"  " + helpKeyStyle.Render("d") + helpDescStyle.Render(" diff") +
		"  " + helpKeyStyle.Render("?") + helpDescStyle.Render(" help") +
		"  " + helpKeyStyle.Render("q") + helpDescStyle.Render(" quit") + " "

	// Pad middle.
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
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
  Enter        Host table: expand host output
               Command input: execute command
  Esc          Close overlay / collapse expanded view
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
		Width(width - 4).
		Height(height - 2).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorCyan)

	return style.Render(help)
}
