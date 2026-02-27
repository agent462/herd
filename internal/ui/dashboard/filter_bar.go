package dashboard

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// filterBar is a togglable text input for filtering hosts by substring.
type filterBar struct {
	input   textinput.Model
	visible bool
	width   int
}

func newFilterBar(width int) filterBar {
	ti := textinput.New()
	ti.Prompt = "filter> "
	ti.Placeholder = "hostname substring..."
	ti.SetWidth(width - 4)

	return filterBar{
		input: ti,
		width: width,
	}
}

func (f *filterBar) Toggle() tea.Cmd {
	f.visible = !f.visible
	if f.visible {
		return f.input.Focus()
	}
	f.input.Blur()
	f.input.Reset()
	return nil
}

func (f *filterBar) IsVisible() bool {
	return f.visible
}

func (f *filterBar) Update(msg tea.Msg) tea.Cmd {
	if !f.visible {
		return nil
	}
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)
	return cmd
}

func (f *filterBar) View() string {
	if !f.visible {
		return ""
	}
	return f.input.View()
}

func (f *filterBar) Query() string {
	if !f.visible {
		return ""
	}
	return f.input.Value()
}

func (f *filterBar) MatchesHost(name string) bool {
	q := f.Query()
	if q == "" {
		return true
	}
	return strings.Contains(strings.ToLower(name), strings.ToLower(q))
}

func (f *filterBar) Resize(width int) {
	f.width = width
	f.input.SetWidth(width - 4)
}
