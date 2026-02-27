package exec

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/agent462/herd/internal/executor"
	"github.com/agent462/herd/internal/grouper"
)

// ANSI color codes.
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
)

// Formatter formats grouped execution results for terminal display.
type Formatter struct {
	JSON       bool
	ErrorsOnly bool
	Color      bool
}

// NewFormatter creates a Formatter with the given options.
func NewFormatter(jsonOutput, errorsOnly, color bool) *Formatter {
	return &Formatter{
		JSON:       jsonOutput,
		ErrorsOnly: errorsOnly,
		Color:      color,
	}
}

// Format renders grouped results as a human-readable string.
func (f *Formatter) Format(grouped *grouper.GroupedResults) string {
	var b strings.Builder

	succeeded := 0
	nonZero := 0
	failed := len(grouped.Failed)
	timedOut := len(grouped.TimedOut)

	// Show groups (unless errors-only mode skips successful ones).
	for _, g := range grouped.Groups {
		if g.ExitCode != 0 {
			nonZero += len(g.Hosts)
		} else {
			succeeded += len(g.Hosts)
		}
		if !f.ErrorsOnly || g.ExitCode != 0 {
			f.writeGroup(&b, &g, len(grouped.Groups))
			b.WriteString("\n")
		}
	}

	// Show failed hosts.
	for _, r := range grouped.Failed {
		f.writeFailed(&b, r)
		b.WriteString("\n")
	}

	// Show timed out hosts.
	for _, r := range grouped.TimedOut {
		f.writeTimedOut(&b, r)
		b.WriteString("\n")
	}

	// Summary line.
	b.WriteString(f.summaryLine(succeeded, nonZero, failed, timedOut))
	b.WriteString("\n")

	return b.String()
}

// FormatJSON serializes results as a JSON array.
func (f *Formatter) FormatJSON(results []*executor.HostResult) ([]byte, error) {
	type jsonResult struct {
		Host     string `json:"host"`
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
		Duration string `json:"duration"`
		Error    string `json:"error,omitempty"`
	}

	out := make([]jsonResult, len(results))
	for i, r := range results {
		out[i] = jsonResult{
			Host:     r.Host,
			Stdout:   string(r.Stdout),
			Stderr:   string(r.Stderr),
			ExitCode: r.ExitCode,
			Duration: r.Duration.String(),
		}
		if r.Err != nil {
			out[i].Error = r.Err.Error()
		}
	}

	return json.MarshalIndent(out, "", "  ")
}

func (f *Formatter) writeGroup(b *strings.Builder, g *grouper.OutputGroup, totalGroups int) {
	hostCount := len(g.Hosts)
	hostWord := "hosts"
	if hostCount == 1 {
		hostWord = "host"
	}

	if g.ExitCode != 0 {
		label := fmt.Sprintf(" %d %s exited with code %d:", hostCount, hostWord, g.ExitCode)
		b.WriteString(f.colorize(label, colorRed))
	} else if g.IsNorm {
		var label string
		if totalGroups == 1 && hostCount == 1 {
			// "1 host identical" doesn't make sense for a single host.
			label = fmt.Sprintf(" %d %s:", hostCount, hostWord)
		} else {
			label = fmt.Sprintf(" %d %s identical:", hostCount, hostWord)
		}
		b.WriteString(f.colorize(label, colorGreen))
	} else {
		verb := "differ"
		if hostCount == 1 {
			verb = "differs"
		}
		label := fmt.Sprintf(" %d %s %s:", hostCount, hostWord, verb)
		b.WriteString(f.colorize(label, colorYellow))
	}
	b.WriteString("\n")

	// Host list.
	hostList := "   " + f.colorize(strings.Join(g.Hosts, ", "), colorCyan)
	b.WriteString(hostList)
	b.WriteString("\n")

	// Output (indented).
	stdout := strings.TrimRight(string(g.Stdout), "\n")
	if stdout != "" {
		for _, line := range strings.Split(stdout, "\n") {
			b.WriteString("   ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	// Stderr (if any).
	stderr := strings.TrimRight(string(g.Stderr), "\n")
	if stderr != "" {
		for _, line := range strings.Split(stderr, "\n") {
			b.WriteString("   ")
			b.WriteString(f.colorize("stderr: "+line, colorRed))
			b.WriteString("\n")
		}
	}

	// Diff for outlier groups.
	if !g.IsNorm && g.Diff != "" {
		b.WriteString("\n")
		f.writeDiff(b, g.Diff)
	}
}

func (f *Formatter) writeDiff(b *strings.Builder, diff string) {
	for _, line := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		b.WriteString("   ")
		switch {
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
			b.WriteString(f.colorize(line, colorCyan))
		case strings.HasPrefix(line, "+"):
			b.WriteString(f.colorize(line, colorGreen))
		case strings.HasPrefix(line, "-"):
			b.WriteString(f.colorize(line, colorRed))
		default:
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
}

func (f *Formatter) writeFailed(b *strings.Builder, r *executor.HostResult) {
	label := " 1 host failed:"
	b.WriteString(f.colorize(label, colorRed))
	b.WriteString("\n")

	errMsg := "unknown error"
	if r.Err != nil {
		errMsg = r.Err.Error()
	}
	b.WriteString("   ")
	b.WriteString(f.colorize(r.Host, colorCyan))
	b.WriteString(fmt.Sprintf(" (%s)", errMsg))
	b.WriteString("\n")
}

func (f *Formatter) writeTimedOut(b *strings.Builder, r *executor.HostResult) {
	label := " 1 host timed out:"
	b.WriteString(f.colorize(label, colorRed))
	b.WriteString("\n")

	errMsg := "timeout"
	if r.Err != nil {
		errMsg = r.Err.Error()
	}
	b.WriteString("   ")
	b.WriteString(f.colorize(r.Host, colorCyan))
	b.WriteString(fmt.Sprintf(" (%s)", errMsg))
	b.WriteString("\n")
}

func (f *Formatter) summaryLine(succeeded, nonZero, failed, timedOut int) string {
	parts := []string{
		fmt.Sprintf("%d succeeded", succeeded),
	}
	if nonZero > 0 {
		parts = append(parts, fmt.Sprintf("%d non-zero exit", nonZero))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if timedOut > 0 {
		parts = append(parts, fmt.Sprintf("%d timeout", timedOut))
	}
	return strings.Join(parts, ", ")
}

func (f *Formatter) colorize(text, color string) string {
	if !f.Color {
		return text
	}
	return color + text + colorReset
}
