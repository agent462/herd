package dashboard

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/agent462/herd/internal/executor"
	"github.com/agent462/herd/internal/grouper"
)

// tabBarHeight is the number of rows consumed by the tab bar.
const tabBarHeight = 2 // 1 row for tabs + 1 row for bottom border

// outputPane wraps a bubbles/viewport for displaying grouped command results,
// with a tab bar for switching between the diff view and per-host output.
type outputPane struct {
	viewport viewport.Model
	tabBar   tabBar
	width    int
	height   int

	// Cached data for re-rendering when tabs switch.
	lastGrouped *grouper.GroupedResults
	lastResults []*executor.HostResult
	allHosts    []string
}

func newOutputPane(width, height int) outputPane {
	contentWidth := width - 2 // account for pane border
	vp := viewport.New(
		viewport.WithWidth(contentWidth),
		viewport.WithHeight(height-2-tabBarHeight), // border + tab bar
	)
	return outputPane{
		viewport: vp,
		tabBar:   newTabBar(contentWidth),
		width:    contentWidth,
		height:   height,
	}
}

func (o *outputPane) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	o.viewport, cmd = o.viewport.Update(msg)
	return cmd
}

func (o *outputPane) View() string {
	bar := o.tabBar.View()
	var content string
	if o.width > 0 {
		content = lipgloss.NewStyle().MaxWidth(o.width).Render(o.viewport.View())
	} else {
		content = o.viewport.View()
	}
	return lipgloss.JoinVertical(lipgloss.Left, bar, content)
}

// setContent truncates each line to the viewport width (ANSI-aware) before
// setting it, preventing terminal-level wrapping from inflating the visual height.
func (o *outputPane) setContent(s string) {
	if o.width <= 0 {
		o.viewport.SetContent(s)
		return
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, o.width, "")
	}
	o.viewport.SetContent(strings.Join(lines, "\n"))
}

func (o *outputPane) Resize(width, height int) {
	o.width = width - 2 // content width inside pane border
	o.height = height
	o.viewport.SetWidth(o.width)
	o.viewport.SetHeight(height - 2 - tabBarHeight)
	o.tabBar.Resize(o.width)
}

// SetGroupedResults updates the output pane with new execution results.
// Rebuilds the tab bar and re-renders the active tab's content.
func (o *outputPane) SetGroupedResults(grouped *grouper.GroupedResults, results []*executor.HostResult) {
	o.lastGrouped = grouped
	o.lastResults = results

	if grouped == nil {
		o.setContent("No results yet. Type a command below.")
		return
	}

	// Collect all hosts in order from results.
	hosts := make([]string, 0, len(results))
	for _, r := range results {
		hosts = append(hosts, r.Host)
	}
	o.allHosts = hosts
	o.tabBar.SetTabs(hosts)

	o.renderActiveTab()
}

// renderActiveTab dispatches to the correct renderer based on the active tab.
func (o *outputPane) renderActiveTab() {
	id := o.tabBar.ActiveID()
	if id == "diff" {
		if o.lastGrouped != nil {
			o.renderGrouped(o.lastGrouped)
		}
	} else {
		o.renderHostOutput(id, o.lastGrouped, o.lastResults)
	}
}

// NextTab switches to the next tab and re-renders.
func (o *outputPane) NextTab() {
	o.tabBar.Next()
	o.renderActiveTab()
}

// PrevTab switches to the previous tab and re-renders.
func (o *outputPane) PrevTab() {
	o.tabBar.Prev()
	o.renderActiveTab()
}

// SetTabIndex jumps to a tab by index and re-renders.
func (o *outputPane) SetTabIndex(index int) {
	o.tabBar.SetActive(index)
	o.renderActiveTab()
}

// ActivateHostTab switches to a specific host's tab.
// Returns true if the host was found in the tab list.
func (o *outputPane) ActivateHostTab(hostname string) bool {
	if o.tabBar.SetActiveByID(hostname) {
		o.renderActiveTab()
		return true
	}
	return false
}

// ActivateDiffTab switches back to the diff output tab.
func (o *outputPane) ActivateDiffTab() {
	o.tabBar.SetActive(0)
	o.renderActiveTab()
}

// ExpandHost is a backwards-compatible wrapper that activates the host's tab.
func (o *outputPane) ExpandHost(name string, grouped *grouper.GroupedResults, results []*executor.HostResult) {
	o.lastGrouped = grouped
	o.lastResults = results
	// Ensure tabs are populated.
	if len(o.tabBar.tabs) <= 1 {
		hosts := make([]string, 0, len(results))
		for _, r := range results {
			hosts = append(hosts, r.Host)
		}
		o.allHosts = hosts
		o.tabBar.SetTabs(hosts)
	}
	o.tabBar.SetActiveByID(name)
	o.renderActiveTab()
}

// CollapseHost is a backwards-compatible wrapper that returns to the diff tab.
func (o *outputPane) CollapseHost(grouped *grouper.GroupedResults, results []*executor.HostResult) {
	o.lastGrouped = grouped
	o.lastResults = results
	o.ActivateDiffTab()
}

// IsExpanded returns true if viewing a specific host (not the diff tab).
func (o *outputPane) IsExpanded() bool {
	return o.tabBar.ActiveID() != "diff"
}

func (o *outputPane) renderGrouped(grouped *grouper.GroupedResults) {
	var b strings.Builder

	succeeded := 0
	nonZero := 0

	for _, g := range grouped.Groups {
		if g.ExitCode != 0 {
			nonZero += len(g.Hosts)
		} else {
			succeeded += len(g.Hosts)
		}
		writeGroup(&b, &g, len(grouped.Groups))
		b.WriteString("\n")
	}

	for _, r := range grouped.Failed {
		writeFailed(&b, r)
		b.WriteString("\n")
	}

	for _, r := range grouped.TimedOut {
		writeTimedOut(&b, r)
		b.WriteString("\n")
	}

	// Summary.
	summary := fmt.Sprintf("%d succeeded", succeeded)
	if nonZero > 0 {
		summary += fmt.Sprintf(", %d non-zero exit", nonZero)
	}
	if len(grouped.Failed) > 0 {
		summary += fmt.Sprintf(", %d failed", len(grouped.Failed))
	}
	if len(grouped.TimedOut) > 0 {
		summary += fmt.Sprintf(", %d timeout", len(grouped.TimedOut))
	}
	b.WriteString(summary + "\n")

	o.setContent(b.String())
	o.viewport.GotoTop()
}

func (o *outputPane) renderHostOutput(host string, grouped *grouper.GroupedResults, results []*executor.HostResult) {
	var b strings.Builder

	b.WriteString(hostNameStyle.Render("── "+host+" ──") + "\n\n")

	r := findHostResult(host, results)
	if r == nil {
		b.WriteString("(no result for this host)")
		o.setContent(b.String())
		return
	}

	if r.Err != nil {
		b.WriteString(groupHeaderError.Render("Error: " + r.Err.Error()))
		b.WriteString("\n")
	}

	stdout := strings.TrimRight(string(r.Stdout), "\n")
	if stdout != "" {
		b.WriteString(stdout)
		b.WriteString("\n")
	}

	stderr := strings.TrimRight(string(r.Stderr), "\n")
	if stderr != "" {
		b.WriteString("\n")
		b.WriteString(groupHeaderError.Render("stderr:"))
		b.WriteString("\n")
		b.WriteString(stderr)
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf("\nexit code: %d  duration: %s\n", r.ExitCode, r.Duration))

	o.setContent(b.String())
	o.viewport.GotoTop()
}

func writeGroup(b *strings.Builder, g *grouper.OutputGroup, totalGroups int) {
	hostCount := len(g.Hosts)
	hostWord := "hosts"
	if hostCount == 1 {
		hostWord = "host"
	}

	if g.ExitCode != 0 {
		label := fmt.Sprintf("%d %s exited with code %d:", hostCount, hostWord, g.ExitCode)
		b.WriteString(groupHeaderError.Render(label))
	} else if g.IsNorm {
		var label string
		if totalGroups == 1 && hostCount == 1 {
			label = fmt.Sprintf("%d %s:", hostCount, hostWord)
		} else {
			label = fmt.Sprintf("%d %s identical:", hostCount, hostWord)
		}
		b.WriteString(groupHeaderNorm.Render(label))
	} else {
		verb := "differ"
		if hostCount == 1 {
			verb = "differs"
		}
		label := fmt.Sprintf("%d %s %s:", hostCount, hostWord, verb)
		b.WriteString(groupHeaderDiffer.Render(label))
	}
	b.WriteString("\n")

	// Host list.
	b.WriteString("  " + hostNameStyle.Render(strings.Join(g.Hosts, ", ")))
	b.WriteString("\n")

	// Output.
	stdout := strings.TrimRight(string(g.Stdout), "\n")
	if stdout != "" {
		for _, line := range strings.Split(stdout, "\n") {
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	// Stderr.
	stderr := strings.TrimRight(string(g.Stderr), "\n")
	if stderr != "" {
		for _, line := range strings.Split(stderr, "\n") {
			b.WriteString("  ")
			b.WriteString(groupHeaderError.Render("stderr: " + line))
			b.WriteString("\n")
		}
	}

	// Diff for outliers.
	if !g.IsNorm && g.Diff != "" {
		b.WriteString("\n")
		writeDiff(b, g.Diff)
	}
}

func writeDiff(b *strings.Builder, diff string) {
	for _, line := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		b.WriteString("  ")
		switch {
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
			b.WriteString(diffHdrStyle.Render(line))
		case strings.HasPrefix(line, "+"):
			b.WriteString(diffAddStyle.Render(line))
		case strings.HasPrefix(line, "-"):
			b.WriteString(diffDelStyle.Render(line))
		default:
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
}

func writeFailed(b *strings.Builder, r *executor.HostResult) {
	b.WriteString(groupHeaderError.Render("1 host failed:"))
	b.WriteString("\n")
	errMsg := "unknown error"
	if r.Err != nil {
		errMsg = r.Err.Error()
	}
	b.WriteString("  " + hostNameStyle.Render(r.Host) + " (" + errMsg + ")\n")
}

func writeTimedOut(b *strings.Builder, r *executor.HostResult) {
	b.WriteString(groupHeaderError.Render("1 host timed out:"))
	b.WriteString("\n")
	errMsg := "timeout"
	if r.Err != nil {
		errMsg = r.Err.Error()
	}
	b.WriteString("  " + hostNameStyle.Render(r.Host) + " (" + errMsg + ")\n")
}
