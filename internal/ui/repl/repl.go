package repl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/agent462/herd/internal/config"
	"github.com/agent462/herd/internal/executor"
	"github.com/agent462/herd/internal/grouper"
	"github.com/agent462/herd/internal/selector"
	hssh "github.com/agent462/herd/internal/ssh"
	execui "github.com/agent462/herd/internal/ui/exec"
)

// HistoryEntry records a single command execution in the REPL.
type HistoryEntry struct {
	Input     string // full input line including selector
	HostCount int
	OKCount   int
	DiffCount int
	FailCount int
}

// Config holds the settings for creating a REPL session.
type Config struct {
	Pool        *hssh.Pool
	AllHosts    []string
	GroupName   string
	HerdConfig  *config.Config
	BaseSSHConf hssh.ClientConfig
	Timeout     time.Duration
	Concurrency int
	Color       bool
}

// REPL is an interactive session that executes commands across SSH hosts.
type REPL struct {
	pool        *hssh.Pool
	exec        *executor.Executor
	formatter   *execui.Formatter
	allHosts    []string
	groupName   string
	cfg         *config.Config
	baseSSHConf hssh.ClientConfig
	timeout     time.Duration
	concurrency int
	color       bool

	// Mutable state from last command.
	lastResults []*executor.HostResult
	lastGrouped *grouper.GroupedResults
	history     []HistoryEntry
}

// New creates a REPL with the given configuration.
func New(c Config) *REPL {
	r := &REPL{
		pool:        c.Pool,
		allHosts:    c.AllHosts,
		groupName:   c.GroupName,
		cfg:         c.HerdConfig,
		baseSSHConf: c.BaseSSHConf,
		timeout:     c.Timeout,
		concurrency: c.Concurrency,
		color:       c.Color,
		formatter:   execui.NewFormatter(false, false, c.Color),
	}
	r.rebuildExecutor()
	return r
}

func (r *REPL) rebuildExecutor() {
	r.exec = executor.New(r.pool,
		executor.WithConcurrency(r.concurrency),
		executor.WithTimeout(r.timeout),
	)
}

// Close closes the REPL's connection pool and any associated resources.
func (r *REPL) Close() error {
	if r.pool != nil {
		return r.pool.Close()
	}
	return nil
}

// Run starts the interactive REPL loop. It returns nil on clean exit (Ctrl-D or :quit).
// Run closes the connection pool on return; callers should not close it separately.
func (r *REPL) Run(ctx context.Context) error {
	defer r.Close()
	// Capture SIGINT so it doesn't kill the process.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	reader := bufio.NewReader(os.Stdin)

	for {
		// Drain any pending signals from previous iteration.
		drainSignals(sigCh)

		fmt.Fprint(os.Stdout, r.prompt())

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Fprintln(os.Stdout)
				return nil
			}
			// Check if a signal arrived during the read.
			if drained := drainSignals(sigCh); drained {
				fmt.Fprintln(os.Stdout)
				continue
			}
			return fmt.Errorf("read input: %w", err)
		}

		// If a signal arrived while we were reading, discard the line.
		if drained := drainSignals(sigCh); drained {
			fmt.Fprintln(os.Stdout)
			continue
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Colon-commands.
		if strings.HasPrefix(line, ":") {
			if quit := r.handleCommand(line); quit {
				return nil
			}
			continue
		}

		// Parse selector and command.
		sel, cmd := selector.ParseInput(line)
		if cmd == "" {
			fmt.Fprintln(os.Stderr, "no command specified")
			continue
		}

		state := &selector.State{
			AllHosts: r.allHosts,
			Grouped:  r.lastGrouped,
		}
		hosts, err := selector.Resolve(sel, state)
		if err != nil {
			fmt.Fprintf(os.Stderr, "selector error: %v\n", err)
			continue
		}
		if len(hosts) == 0 {
			fmt.Fprintln(os.Stderr, "no hosts match selector")
			continue
		}

		// Execute with Ctrl-C cancellation via signal.NotifyContext.
		// Each command gets its own context so Ctrl-C cancels only the
		// current command, not the entire REPL session.
		execCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
		results := r.exec.Execute(execCtx, hosts, cmd)
		stop()

		grouped := grouper.Group(results)
		fmt.Fprint(os.Stdout, r.formatter.Format(grouped))

		r.lastResults = results
		r.lastGrouped = grouped
		r.addHistory(line, grouped)
	}
}

func (r *REPL) prompt() string {
	hostWord := "hosts"
	if len(r.allHosts) == 1 {
		hostWord = "host"
	}
	if r.groupName != "" {
		return fmt.Sprintf("herd [%s: %d %s]> ", r.groupName, len(r.allHosts), hostWord)
	}
	return fmt.Sprintf("herd [%d %s]> ", len(r.allHosts), hostWord)
}

func (r *REPL) addHistory(input string, grouped *grouper.GroupedResults) {
	entry := HistoryEntry{Input: input}

	for _, g := range grouped.Groups {
		entry.HostCount += len(g.Hosts)
		if g.IsNorm {
			entry.OKCount += len(g.Hosts)
		} else {
			entry.DiffCount += len(g.Hosts)
		}
	}
	entry.FailCount += len(grouped.Failed) + len(grouped.NonZero) + len(grouped.TimedOut)
	entry.HostCount += entry.FailCount

	r.history = append(r.history, entry)
}

// handleCommand processes a colon-prefixed REPL command.
// Returns true if the REPL should exit.
func (r *REPL) handleCommand(line string) bool {
	parts := strings.Fields(line)
	cmd := parts[0]
	args := parts[1:]

	switch cmd {
	case ":quit", ":q":
		return true

	case ":history", ":h":
		r.showHistory()

	case ":hosts":
		r.showHosts()

	case ":group":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "usage: :group <name>")
			return false
		}
		if err := r.switchGroup(args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "switch group: %v\n", err)
		}

	case ":timeout":
		if len(args) == 0 {
			fmt.Fprintf(os.Stdout, "current timeout: %s\n", r.timeout)
			return false
		}
		d, err := time.ParseDuration(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid duration: %v\n", err)
			return false
		}
		r.timeout = d
		r.rebuildExecutor()
		fmt.Fprintf(os.Stdout, "timeout set to %s\n", d)

	case ":diff":
		r.showDiff()

	case ":last":
		r.showLast()

	case ":export":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "usage: :export <file>")
			return false
		}
		if err := r.exportJSON(args[0]); err != nil {
			fmt.Fprintf(os.Stderr, "export: %v\n", err)
		} else {
			fmt.Fprintf(os.Stdout, "exported to %s\n", args[0])
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (try :quit, :history, :hosts, :group, :timeout, :diff, :last, :export)\n", cmd)
	}

	return false
}

func (r *REPL) showHistory() {
	if len(r.history) == 0 {
		fmt.Fprintln(os.Stdout, "no history")
		return
	}
	for i, e := range r.history {
		input := e.Input
		if len(input) > 40 {
			input = input[:37] + "..."
		}
		fmt.Fprintf(os.Stdout, " %-4d %-42s (%d %s",
			i+1, input, e.HostCount, plural("host", e.HostCount))

		var parts []string
		if e.OKCount > 0 {
			parts = append(parts, fmt.Sprintf("%d ok", e.OKCount))
		}
		if e.DiffCount > 0 {
			parts = append(parts, fmt.Sprintf("%d differs", e.DiffCount))
		}
		if e.FailCount > 0 {
			parts = append(parts, fmt.Sprintf("%d failed", e.FailCount))
		}
		if len(parts) > 0 {
			fmt.Fprintf(os.Stdout, ", %s", strings.Join(parts, ", "))
		}
		fmt.Fprintln(os.Stdout, ")")
	}
}

func (r *REPL) showHosts() {
	for _, h := range r.allHosts {
		status := "not connected"
		if r.pool.IsConnected(h) {
			status = "connected"
		}
		fmt.Fprintf(os.Stdout, "  %-30s %s\n", h, status)
	}
}

func (r *REPL) switchGroup(name string) error {
	hosts, err := config.ResolveHosts(r.cfg, name, nil)
	if err != nil {
		return err
	}

	r.pool.Close()

	hostConfs := make(map[string]hssh.HostConfig, len(hosts))
	hostNames := make([]string, len(hosts))
	for i, h := range hosts {
		hostNames[i] = h.Name
		hostConfs[h.Name] = hssh.HostConfig{
			Hostname:     h.Hostname,
			User:         h.User,
			Port:         h.Port,
			IdentityFile: h.IdentityFile,
			ProxyJump:    h.ProxyJump,
		}
	}

	r.pool = hssh.NewPool(r.baseSSHConf, hostConfs)
	r.allHosts = hostNames
	r.groupName = name
	r.lastResults = nil
	r.lastGrouped = nil
	r.rebuildExecutor()

	fmt.Fprintf(os.Stdout, "switched to group %q (%d %s)\n",
		name, len(hostNames), plural("host", len(hostNames)))
	return nil
}

func (r *REPL) showDiff() {
	if r.lastGrouped == nil {
		fmt.Fprintln(os.Stderr, "no previous command results")
		return
	}

	hasDiff := false
	for _, g := range r.lastGrouped.Groups {
		if !g.IsNorm && g.Diff != "" {
			hasDiff = true
			fmt.Fprintf(os.Stdout, "--- %s ---\n", strings.Join(g.Hosts, ", "))
			fmt.Fprint(os.Stdout, g.Diff)
			fmt.Fprintln(os.Stdout)
		}
	}

	if !hasDiff {
		fmt.Fprintln(os.Stdout, "no differences in last command output")
	}
}

func (r *REPL) showLast() {
	if r.lastGrouped == nil {
		fmt.Fprintln(os.Stderr, "no previous command results")
		return
	}
	fmt.Fprint(os.Stdout, r.formatter.Format(r.lastGrouped))
}

func (r *REPL) exportJSON(filename string) error {
	if r.lastResults == nil {
		return fmt.Errorf("no results to export")
	}

	data, err := r.formatter.FormatJSON(r.lastResults)
	if err != nil {
		return err
	}
	return os.WriteFile(filename, append(data, '\n'), 0644)
}

func plural(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func drainSignals(ch <-chan os.Signal) bool {
	drained := false
	for {
		select {
		case <-ch:
			drained = true
		default:
			return drained
		}
	}
}

// FormatHistoryEntry formats a single history entry for display.
// Exported for testing.
func FormatHistoryEntry(index int, e HistoryEntry) string {
	input := e.Input
	if len(input) > 40 {
		input = input[:37] + "..."
	}

	var b strings.Builder
	fmt.Fprintf(&b, " %-4d %-42s (%d %s",
		index, input, e.HostCount, plural("host", e.HostCount))

	var parts []string
	if e.OKCount > 0 {
		parts = append(parts, fmt.Sprintf("%d ok", e.OKCount))
	}
	if e.DiffCount > 0 {
		parts = append(parts, fmt.Sprintf("%d differs", e.DiffCount))
	}
	if e.FailCount > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", e.FailCount))
	}
	if len(parts) > 0 {
		fmt.Fprintf(&b, ", %s", strings.Join(parts, ", "))
	}
	b.WriteString(")")
	return b.String()
}

// ParseColonCommand parses a colon-command into its name and arguments.
// Exported for testing.
func ParseColonCommand(line string) (cmd string, args []string) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return "", nil
	}
	return parts[0], parts[1:]
}

// ValidCommands returns the list of valid colon-command names.
func ValidCommands() []string {
	return []string{":quit", ":q", ":history", ":h", ":hosts", ":group", ":timeout", ":diff", ":last", ":export"}
}

// ParseTimeout parses a timeout duration string, exported for testing.
func ParseTimeout(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}

// ParseHistoryRef checks if a string is a history reference like "!3".
// Returns the 1-based index and true if it is, or 0 and false otherwise.
func ParseHistoryRef(s string) (int, bool) {
	if !strings.HasPrefix(s, "!") || len(s) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(s[1:])
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
