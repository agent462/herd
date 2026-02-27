package dashboard

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/agent462/herd/internal/executor"
	"github.com/agent462/herd/internal/grouper"
)

// hostEntry tracks per-host state shown in the table.
type hostEntry struct {
	Name      string
	Connected bool
	LastCmd   string
	ExitCode  int
	Duration  string
	Status    string // "ok", "differs", "failed", "timeout", ""
}

// hostTable wraps a bubbles/table with host state tracking.
type hostTable struct {
	table   table.Model
	entries []hostEntry
	width   int
	height  int
}

func newHostTable(hosts []string, width, height int) hostTable {
	entries := make([]hostEntry, len(hosts))
	for i, h := range hosts {
		entries[i] = hostEntry{Name: h, Status: "pending"}
	}

	// Subtract 2 for the outer pane border so rows fit inside the content area.
	contentWidth := width - 2

	columns := []table.Column{
		{Title: "Host", Width: 20},
		{Title: "Status", Width: 10},
		{Title: "Cmd", Width: 18},
		{Title: "Exit", Width: 5},
		{Title: "Time", Width: 8},
	}

	rows := buildRows(entries)

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(false),
		table.WithWidth(contentWidth),
		table.WithHeight(height-3), // account for border + header border-bottom
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorSubtle).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	// Override the default keymap to avoid conflicts with our global keys.
	// The defaults bind f=PageDown, d=HalfPageDown, g=GotoTop, b=PageUp,
	// which collide with our filter, diff, and other shortcuts.
	km := table.DefaultKeyMap()
	km.PageDown = key.NewBinding(key.WithKeys("pgdown"))
	km.PageUp = key.NewBinding(key.WithKeys("pgup"))
	km.HalfPageDown = key.NewBinding(key.WithKeys("ctrl+d"))
	km.HalfPageUp = key.NewBinding(key.WithKeys("ctrl+u"))
	km.GotoTop = key.NewBinding(key.WithKeys("home"))
	km.GotoBottom = key.NewBinding(key.WithKeys("end"))
	t.KeyMap = km

	ht := hostTable{
		table:   t,
		entries: entries,
		width:   contentWidth,
		height:  height,
	}
	ht.resizeColumns()
	return ht
}

func (h *hostTable) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	h.table, cmd = h.table.Update(msg)
	return cmd
}

func (h *hostTable) View() string {
	return h.table.View()
}

func (h *hostTable) Focus() {
	h.table.Focus()
}

func (h *hostTable) Blur() {
	h.table.Blur()
}

func (h *hostTable) Focused() bool {
	return h.table.Focused()
}

func (h *hostTable) SelectedHost() string {
	row := h.table.SelectedRow()
	if row == nil {
		return ""
	}
	return row[0]
}

func (h *hostTable) Resize(width, height int) {
	h.width = width - 2 // content width inside pane border
	h.height = height
	h.table.SetWidth(h.width)
	h.table.SetHeight(height - 3)
	h.resizeColumns()
}

func (h *hostTable) resizeColumns() {
	// Available width for column content (subtract cell padding: 1 left + 1 right per column × 5 cols).
	w := h.width - 10
	if w < 30 {
		w = 30
	}

	// Fixed-width columns get a share; host name gets the remainder.
	statusW := 8
	exitW := 4
	timeW := 7
	fixed := statusW + exitW + timeW

	// Split remaining space: ~60% host, ~40% cmd.
	remaining := w - fixed
	if remaining < 10 {
		remaining = 10
	}
	hostW := remaining * 60 / 100
	cmdW := remaining - hostW
	if hostW < 8 {
		hostW = 8
	}
	if cmdW < 6 {
		cmdW = 6
	}

	h.table.SetColumns([]table.Column{
		{Title: "Host", Width: hostW},
		{Title: "Status", Width: statusW},
		{Title: "Cmd", Width: cmdW},
		{Title: "Exit", Width: exitW},
		{Title: "Time", Width: timeW},
	})
}

func (h *hostTable) UpdateHealth(status map[string]bool) {
	for i := range h.entries {
		if connected, ok := status[h.entries[i].Name]; ok {
			h.entries[i].Connected = connected
		}
	}
	h.table.SetRows(buildRows(h.entries))
}

func (h *hostTable) UpdateResults(command string, grouped *grouper.GroupedResults, results []*executor.HostResult) {
	// Build lookup maps.
	hostStatus := make(map[string]string)
	hostExit := make(map[string]int)

	for _, g := range grouped.Groups {
		status := "ok"
		if !g.IsNorm {
			status = "differs"
		}
		if g.ExitCode != 0 {
			status = "error"
		}
		for _, host := range g.Hosts {
			hostStatus[host] = status
			hostExit[host] = g.ExitCode
		}
	}
	for _, r := range grouped.Failed {
		hostStatus[r.Host] = "failed"
		hostExit[r.Host] = -1
	}
	for _, r := range grouped.TimedOut {
		hostStatus[r.Host] = "timeout"
		hostExit[r.Host] = -1
	}

	// Build duration map from the raw results (covers all hosts).
	hostDur := make(map[string]string, len(results))
	for _, r := range results {
		hostDur[r.Host] = formatDuration(r.Duration)
	}

	for i := range h.entries {
		name := h.entries[i].Name
		if s, ok := hostStatus[name]; ok {
			h.entries[i].Status = s
			h.entries[i].LastCmd = truncate(command, 18)
			h.entries[i].ExitCode = hostExit[name]
		}
		if d, ok := hostDur[name]; ok {
			h.entries[i].Duration = d
		}
	}

	h.table.SetRows(buildRows(h.entries))
}

// ConnectedCount returns the number of connected hosts.
func (h *hostTable) ConnectedCount() int {
	n := 0
	for _, e := range h.entries {
		if e.Connected {
			n++
		}
	}
	return n
}

func buildRows(entries []hostEntry) []table.Row {
	rows := make([]table.Row, len(entries))
	for i, e := range entries {
		status := e.Status
		exitStr := ""
		if e.LastCmd != "" {
			exitStr = fmt.Sprintf("%d", e.ExitCode)
		}
		rows[i] = table.Row{e.Name, status, e.LastCmd, exitStr, e.Duration}
	}
	return rows
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// findHostResult searches the raw results for a specific host.
func findHostResult(host string, results []*executor.HostResult) *executor.HostResult {
	for _, r := range results {
		if r.Host == host {
			return r
		}
	}
	return nil
}
