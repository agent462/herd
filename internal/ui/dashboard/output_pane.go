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

// outputPane wraps a bubbles/viewport for displaying grouped command results.
type outputPane struct {
	viewport     viewport.Model
	width        int
	height       int
	expandedHost string // when non-empty, show only this host's output
}

func newOutputPane(width, height int) outputPane {
	contentWidth := width - 2 // account for pane border
	vp := viewport.New(
		viewport.WithWidth(contentWidth),
		viewport.WithHeight(height-2), // account for border
	)
	return outputPane{
		viewport: vp,
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
	// Hard-clip the viewport output to prevent any line from exceeding the
	// content width.  The viewport pads lines but does not truncate, so this
	// catches edge cases where styled/ANSI content is wider than expected.
	if o.width > 0 {
		return lipgloss.NewStyle().MaxWidth(o.width).Render(o.viewport.View())
	}
	return o.viewport.View()
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
	o.viewport.SetHeight(height - 2)
}

func (o *outputPane) SetGroupedResults(grouped *grouper.GroupedResults, results []*executor.HostResult) {
	if grouped == nil {
		o.setContent("No results yet. Type a command below.")
		return
	}

	if o.expandedHost != "" {
		o.renderHostOutput(o.expandedHost, grouped, results)
		return
	}

	o.renderGrouped(grouped)
}

func (o *outputPane) ExpandHost(name string, grouped *grouper.GroupedResults, results []*executor.HostResult) {
	o.expandedHost = name
	o.renderHostOutput(name, grouped, results)
}

func (o *outputPane) CollapseHost(grouped *grouper.GroupedResults, results []*executor.HostResult) {
	o.expandedHost = ""
	if grouped != nil {
		o.renderGrouped(grouped)
	}
}

func (o *outputPane) IsExpanded() bool {
	return o.expandedHost != ""
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
