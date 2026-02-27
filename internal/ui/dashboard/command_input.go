package dashboard

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// commandInput wraps a bubbles/textinput with the herd> prompt.
type commandInput struct {
	input textinput.Model
	width int
}

func newCommandInput(width int) commandInput {
	ti := textinput.New()
	ti.Prompt = "herd> "
	ti.Placeholder = "type a command..."
	ti.SetWidth(width - 4) // account for border/padding
	ti.Focus()

	return commandInput{
		input: ti,
		width: width,
	}
}

func (c *commandInput) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	return cmd
}

func (c *commandInput) View() string {
	return c.input.View()
}

func (c *commandInput) Value() string {
	return c.input.Value()
}

func (c *commandInput) Reset() {
	c.input.Reset()
}

func (c *commandInput) Focus() tea.Cmd {
	return c.input.Focus()
}

func (c *commandInput) Blur() {
	c.input.Blur()
}

func (c *commandInput) Focused() bool {
	return c.input.Focused()
}

func (c *commandInput) Resize(width int) {
	c.width = width
	c.input.SetWidth(width - 4)
}
