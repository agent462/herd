package dashboard

import (
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/agent462/herd/internal/executor"
	"github.com/agent462/herd/internal/grouper"
)

// diffView is a full-screen overlay showing side-by-side diff of norm vs outlier output.
type diffView struct {
	normVP    viewport.Model
	outlierVP viewport.Model
	visible   bool
	hostName  string
	width     int
	height    int
}

func newDiffView(width, height int) diffView {
	half := width / 2
	return diffView{
		normVP:    viewport.New(viewport.WithWidth(half-2), viewport.WithHeight(height-4)),
		outlierVP: viewport.New(viewport.WithWidth(half-2), viewport.WithHeight(height-4)),
		width:     width,
		height:    height,
	}
}

func (d *diffView) Show(hostName string, grouped *grouper.GroupedResults, results []*executor.HostResult) {
	d.visible = true
	d.hostName = hostName

	var normContent, outlierContent string

	// Find the norm group output.
	for _, g := range grouped.Groups {
		if g.IsNorm {
			normContent = strings.TrimRight(string(g.Stdout), "\n")
			break
		}
	}

	// Find the host's output.
	r := findHostResult(hostName, results)
	if r != nil {
		outlierContent = strings.TrimRight(string(r.Stdout), "\n")
	}

	half := d.width / 2
	d.normVP.SetWidth(half - 4)
	d.normVP.SetHeight(d.height - 6)
	d.outlierVP.SetWidth(half - 4)
	d.outlierVP.SetHeight(d.height - 6)

	d.normVP.SetContent(normContent)
	d.outlierVP.SetContent(outlierContent)
	d.normVP.GotoTop()
	d.outlierVP.GotoTop()
}

func (d *diffView) Hide() {
	d.visible = false
	d.hostName = ""
}

func (d *diffView) IsVisible() bool {
	return d.visible
}

func (d *diffView) Update(msg tea.Msg) tea.Cmd {
	if !d.visible {
		return nil
	}

	var cmd1, cmd2 tea.Cmd
	d.normVP, cmd1 = d.normVP.Update(msg)
	d.outlierVP, cmd2 = d.outlierVP.Update(msg)
	return tea.Batch(cmd1, cmd2)
}

func (d *diffView) View() string {
	if !d.visible {
		return ""
	}

	half := d.width / 2

	normHeader := diffHdrStyle.Render("── norm ──")
	outlierHeader := diffHdrStyle.Render("── " + d.hostName + " ──")

	normPane := lipgloss.NewStyle().
		Width(half - 2).
		Height(d.height - 4).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorGreen).
		Render(normHeader + "\n" + d.normVP.View())

	outlierPane := lipgloss.NewStyle().
		Width(half - 2).
		Height(d.height - 4).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorYellow).
		Render(outlierHeader + "\n" + d.outlierVP.View())

	content := lipgloss.JoinHorizontal(lipgloss.Top, normPane, outlierPane)
	footer := helpDescStyle.Render("  Esc to close  │  j/k to scroll")

	return lipgloss.JoinVertical(lipgloss.Left, content, footer)
}

func (d *diffView) Resize(width, height int) {
	d.width = width
	d.height = height
	half := width / 2
	d.normVP.SetWidth(half - 4)
	d.normVP.SetHeight(height - 6)
	d.outlierVP.SetWidth(half - 4)
	d.outlierVP.SetHeight(height - 6)
}
