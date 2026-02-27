package dashboard

import (
	"charm.land/lipgloss/v2"
)

// Color palette.
var (
	colorGreen   = lipgloss.Color("#04B575")
	colorRed     = lipgloss.Color("#FF4672")
	colorYellow  = lipgloss.Color("#FDFF90")
	colorCyan    = lipgloss.Color("#00E5FF")
	colorSubtle  = lipgloss.Color("#626262")
	colorWhite   = lipgloss.Color("#FFFFFF")
	colorDiffAdd = lipgloss.Color("#04B575")
	colorDiffDel = lipgloss.Color("#FF4672")
	colorDiffHdr = lipgloss.Color("#00E5FF")
)

// Pane border and layout styles.
var (
	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorSubtle)

	focusedPaneStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorCyan)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(colorWhite).
			Background(lipgloss.Color("#333333")).
			Padding(0, 1)

	statusConnected = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)

	statusDisconnected = lipgloss.NewStyle().
				Foreground(colorRed).
				Bold(true)

	commandPromptStyle = lipgloss.NewStyle().
				Foreground(colorCyan).
				Bold(true)

	groupHeaderNorm = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)

	groupHeaderDiffer = lipgloss.NewStyle().
				Foreground(colorYellow).
				Bold(true)

	groupHeaderError = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	hostNameStyle = lipgloss.NewStyle().
			Foreground(colorCyan)

	diffAddStyle = lipgloss.NewStyle().
			Foreground(colorDiffAdd)

	diffDelStyle = lipgloss.NewStyle().
			Foreground(colorDiffDel)

	diffHdrStyle = lipgloss.NewStyle().
			Foreground(colorDiffHdr)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(colorCyan).
			Bold(true)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(colorSubtle)
)
