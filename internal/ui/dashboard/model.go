package dashboard

import (
	"context"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/agent462/herd/internal/executor"
	"github.com/agent462/herd/internal/grouper"
	"github.com/agent462/herd/internal/selector"
	"github.com/agent462/herd/internal/ssh"
)

// pane identifies which sub-model has focus.
type pane int

const (
	paneHostTable pane = iota
	paneOutput
	paneCommandInput
)

// Config holds the parameters needed to create a dashboard Model.
type Config struct {
	Pool           *ssh.Pool
	Executor       *executor.Executor
	AllHosts       []string
	GroupName      string
	HealthInterval time.Duration
}

// Model is the root Bubble Tea model for the dashboard.
type Model struct {
	pool     *ssh.Pool
	executor *executor.Executor
	allHosts []string
	group    string

	hostTable    hostTable
	outputPane   outputPane
	commandInput commandInput
	filterBar    filterBar
	diffView     diffView

	focused      pane
	showHelp     bool
	lastResults  []*executor.HostResult
	lastGrouped  *grouper.GroupedResults
	lastCommand  string
	history      []string
	healthTick   time.Duration

	width  int
	height int
}

// New creates a new dashboard Model from the given config.
func New(cfg Config) Model {
	if cfg.HealthInterval == 0 {
		cfg.HealthInterval = 10 * time.Second
	}

	return Model{
		pool:         cfg.Pool,
		executor:     cfg.Executor,
		allHosts:     cfg.AllHosts,
		group:        cfg.GroupName,
		hostTable:    newHostTable(cfg.AllHosts, 40, 20),
		outputPane:   newOutputPane(40, 20),
		commandInput: newCommandInput(80),
		filterBar:    newFilterBar(80),
		diffView:     newDiffView(80, 24),
		focused:      paneCommandInput,
		healthTick:   cfg.HealthInterval,
	}
}

// Init returns the initial command (health check tick).
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		healthTickCmd(m.healthTick),
		m.commandInput.Focus(),
	)
}

// Update handles all messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case execResultMsg:
		m.lastCommand = msg.Command
		m.lastResults = msg.Results
		m.lastGrouped = msg.Grouped
		m.hostTable.UpdateResults(msg.Command, msg.Grouped, msg.Results)
		m.outputPane.SetGroupedResults(msg.Grouped, msg.Results)
		return m, nil

	case healthTickMsg:
		return m, healthCheckCmd(m.pool, m.allHosts)

	case healthCheckMsg:
		m.hostTable.UpdateHealth(msg.Status)
		cmds = append(cmds, healthTickCmd(m.healthTick))
		return m, tea.Batch(cmds...)
	}

	// Forward to focused pane.
	switch m.focused {
	case paneHostTable:
		cmd := m.hostTable.Update(msg)
		cmds = append(cmds, cmd)
	case paneOutput:
		cmd := m.outputPane.Update(msg)
		cmds = append(cmds, cmd)
	case paneCommandInput:
		cmd := m.commandInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	if len(cmds) > 0 {
		return m, tea.Batch(cmds...)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.Key()

	// Global overlays first.
	if m.diffView.IsVisible() {
		if key.Code == tea.KeyEscape {
			m.diffView.Hide()
			return m, nil
		}
		cmd := m.diffView.Update(msg)
		return m, cmd
	}

	if m.showHelp {
		if key.Code == tea.KeyEscape || msg.String() == "?" {
			m.showHelp = false
			return m, nil
		}
		return m, nil
	}

	// Filter bar gets keys when visible and focused.
	if m.filterBar.IsVisible() {
		if key.Code == tea.KeyEscape {
			m.filterBar.Toggle()
			return m, nil
		}
		if key.Code == tea.KeyEnter {
			// Apply filter and close.
			m.filterBar.Toggle()
			return m, nil
		}
		cmd := m.filterBar.Update(msg)
		return m, cmd
	}

	// Global keys (when not in text input).
	if m.focused != paneCommandInput {
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		case "f":
			cmd := m.filterBar.Toggle()
			return m, cmd
		}
	} else {
		// In command input: ctrl+c always quits, q/? quit or toggle help when empty.
		switch {
		case msg.String() == "ctrl+c":
			return m, tea.Quit
		case msg.String() == "q" && m.commandInput.Value() == "":
			return m, tea.Quit
		case msg.String() == "?" && m.commandInput.Value() == "":
			m.showHelp = !m.showHelp
			return m, nil
		case msg.String() == "f" && m.commandInput.Value() == "":
			cmd := m.filterBar.Toggle()
			return m, cmd
		}
	}

	// Tab cycles focus.
	if key.Code == tea.KeyTab {
		return m.cycleFocus(), nil
	}

	// Pane-specific keys.
	switch m.focused {
	case paneHostTable:
		return m.handleHostTableKey(msg)
	case paneOutput:
		return m.handleOutputKey(msg)
	case paneCommandInput:
		return m.handleCommandInputKey(msg)
	}

	return m, nil
}

func (m Model) handleHostTableKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.Key()

	switch {
	case key.Code == tea.KeyEnter:
		// Switch to the selected host's tab in the output pane.
		host := m.hostTable.SelectedHost()
		if host != "" && m.lastGrouped != nil {
			if m.outputPane.ActivateHostTab(host) {
				// Only switch focus when the host tab actually exists.
				m.hostTable.Blur()
				m.focused = paneOutput
			}
		}
		return m, nil

	case key.Code == tea.KeyEscape:
		if m.outputPane.IsExpanded() {
			m.outputPane.ActivateDiffTab()
			return m, nil
		}

	case msg.String() == "d":
		// Show diff view for selected host.
		host := m.hostTable.SelectedHost()
		if host != "" && m.lastGrouped != nil {
			m.diffView.Show(host, m.lastGrouped, m.lastResults)
			return m, nil
		}

	case msg.String() == "f":
		cmd := m.filterBar.Toggle()
		return m, cmd
	}

	// Forward j/k and other navigation to the table.
	cmd := m.hostTable.Update(msg)
	return m, cmd
}

func (m Model) handleOutputKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.Key()

	if key.Code == tea.KeyEscape {
		if m.outputPane.IsExpanded() {
			m.outputPane.ActivateDiffTab()
			return m, nil
		}
	}

	// Tab switching with [ and ].
	switch msg.String() {
	case "[":
		m.outputPane.PrevTab()
		return m, nil
	case "]":
		m.outputPane.NextTab()
		return m, nil
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(msg.String()[0]-'0') - 1 // 1-based to 0-based
		m.outputPane.SetTabIndex(idx)
		return m, nil
	}

	// Forward to viewport for scrolling.
	cmd := m.outputPane.Update(msg)
	return m, cmd
}

func (m Model) handleCommandInputKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.Key()

	if key.Code == tea.KeyEnter {
		input := m.commandInput.Value()
		if input == "" {
			return m, nil
		}
		m.commandInput.Reset()
		m.history = append(m.history, input)
		return m, m.executeCommand(input)
	}

	// Forward all other keys to the text input.
	cmd := m.commandInput.Update(msg)
	return m, cmd
}

func (m Model) cycleFocus() Model {
	// Blur current.
	switch m.focused {
	case paneHostTable:
		m.hostTable.Blur()
	case paneCommandInput:
		m.commandInput.Blur()
	}

	// Advance.
	switch m.focused {
	case paneHostTable:
		m.focused = paneOutput
	case paneOutput:
		m.focused = paneCommandInput
		m.commandInput.Focus()
	case paneCommandInput:
		m.focused = paneHostTable
		m.hostTable.Focus()
	}
	return m
}

func (m Model) executeCommand(input string) tea.Cmd {
	sel, command := selector.ParseInput(input)
	if command == "" {
		return nil
	}

	state := &selector.State{
		AllHosts: m.allHosts,
		Grouped:  m.lastGrouped,
	}
	hosts, err := selector.Resolve(sel, state)
	if err != nil {
		// Return error as a result message.
		return func() tea.Msg {
			return execResultMsg{
				Command: command,
			}
		}
	}

	exec := m.executor
	return func() tea.Msg {
		ctx := context.Background()
		results := exec.Execute(ctx, hosts, command)
		grouped := grouper.Group(results)
		return execResultMsg{
			Command: command,
			Results: results,
			Grouped: grouped,
		}
	}
}

func (m *Model) resize() {
	tableWidth := m.width * 35 / 100
	outputWidth := m.width - tableWidth

	// Vertical layout: main panes, filter bar (optional), command input, status bar.
	filterHeight := 0
	if m.filterBar.IsVisible() {
		filterHeight = 1
	}
	statusHeight := 1
	inputHeight := 3
	mainHeight := m.height - statusHeight - inputHeight - filterHeight

	if mainHeight < 5 {
		mainHeight = 5
	}

	m.hostTable.Resize(tableWidth, mainHeight)
	m.outputPane.Resize(outputWidth, mainHeight)
	m.commandInput.Resize(m.width)
	m.filterBar.Resize(m.width)
	m.diffView.Resize(m.width, m.height)
}

// View renders the full dashboard.
func (m Model) View() tea.View {
	if m.width == 0 || m.height == 0 {
		return tea.NewView("Loading...")
	}

	v := tea.NewView(m.renderContent())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) renderContent() string {
	// Help overlay takes over everything.
	if m.showHelp {
		return renderHelpOverlay(m.width, m.height)
	}

	// Diff overlay takes over everything.
	if m.diffView.IsVisible() {
		return m.diffView.View()
	}

	// Main layout.
	tableWidth := m.width * 35 / 100
	outputWidth := m.width - tableWidth

	filterHeight := 0
	if m.filterBar.IsVisible() {
		filterHeight = 1
	}
	statusHeight := 1
	inputHeight := 3
	mainHeight := m.height - statusHeight - inputHeight - filterHeight
	if mainHeight < 5 {
		mainHeight = 5
	}

	// In lipgloss v2, Width(w)/Height(h) set the TOTAL rendered size including
	// borders. Content area = w - GetHorizontalFrameSize(). So we pass the full
	// pane width (not width-2) and the border is included in that total.
	var tableStyle, outputStyle lipgloss.Style
	if m.focused == paneHostTable {
		tableStyle = focusedPaneStyle.Width(tableWidth).Height(mainHeight)
	} else {
		tableStyle = paneStyle.Width(tableWidth).Height(mainHeight)
	}
	if m.focused == paneOutput {
		outputStyle = focusedPaneStyle.Width(outputWidth).Height(mainHeight)
	} else {
		outputStyle = paneStyle.Width(outputWidth).Height(mainHeight)
	}

	tableView := tableStyle.Render(m.hostTable.View())
	outputView := outputStyle.Render(m.outputPane.View())
	mainRow := lipgloss.JoinHorizontal(lipgloss.Top, tableView, outputView)

	// Build vertical stack.
	parts := []string{mainRow}

	if m.filterBar.IsVisible() {
		parts = append(parts, m.filterBar.View())
	}

	inputStyle := paneStyle.Width(m.width)
	if m.focused == paneCommandInput {
		inputStyle = focusedPaneStyle.Width(m.width)
	}
	parts = append(parts, inputStyle.Render(m.commandInput.View()))

	connCount := m.hostTable.ConnectedCount()
	parts = append(parts, renderStatusBar(len(m.allHosts), connCount, m.width, m.group))

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
