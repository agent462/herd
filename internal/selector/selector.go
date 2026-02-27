package selector

import (
	"fmt"
	"path"
	"strings"

	"github.com/agent462/herd/internal/grouper"
)

// State holds the context needed for selector resolution:
// the full host list and (optionally) the results from the last command.
type State struct {
	AllHosts []string
	Grouped  *grouper.GroupedResults // nil if no command has been run yet
}

// ParseInput splits a REPL input line into a selector part and a command part.
// If the input starts with @, the comma-separated list of @-prefixed tokens
// is the selector (spaces around commas are tolerated). The rest is the command.
// Otherwise the selector is empty, implying @all.
func ParseInput(input string) (sel, command string) {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "@") {
		return "", input
	}

	// Consume @-prefixed tokens separated by commas (with optional spaces).
	i := 0
	for {
		// Skip whitespace before token.
		for i < len(input) && input[i] == ' ' {
			i++
		}
		if i >= len(input) || input[i] != '@' {
			break
		}
		// Advance past this selector token.
		for i < len(input) && input[i] != ' ' && input[i] != ',' {
			i++
		}

		// Look ahead past whitespace for a comma.
		j := i
		for j < len(input) && input[j] == ' ' {
			j++
		}
		if j >= len(input) || input[j] != ',' {
			break // no comma → end of selector list
		}
		// Found comma; verify the next non-space char is @.
		j++ // skip comma
		k := j
		for k < len(input) && input[k] == ' ' {
			k++
		}
		if k >= len(input) || input[k] != '@' {
			break // trailing comma, not a combined selector
		}
		i = j // advance past comma; loop will skip whitespace
	}

	sel = strings.TrimSpace(input[:i])
	if i >= len(input) {
		return sel, ""
	}
	return sel, strings.TrimSpace(input[i:])
}

// Resolve maps a selector string to a list of host names.
// An empty selector is equivalent to @all.
func Resolve(sel string, state *State) ([]string, error) {
	if sel == "" || sel == "@all" {
		return state.AllHosts, nil
	}

	parts := strings.Split(sel, ",")
	seen := make(map[string]bool)
	var result []string

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		hosts, err := resolveSingle(part, state)
		if err != nil {
			return nil, err
		}
		for _, h := range hosts {
			if !seen[h] {
				seen[h] = true
				result = append(result, h)
			}
		}
	}

	return result, nil
}

func resolveSingle(sel string, state *State) ([]string, error) {
	if !strings.HasPrefix(sel, "@") {
		return nil, fmt.Errorf("invalid selector %q: must start with @", sel)
	}
	name := sel[1:]

	switch name {
	case "all":
		return state.AllHosts, nil
	case "ok":
		return okHosts(state)
	case "differs":
		return differsHosts(state)
	case "failed":
		return failedHosts(state)
	case "timeout":
		return timeoutHosts(state)
	default:
		return matchHosts(name, state.AllHosts)
	}
}

// okHosts returns hosts in the norm (majority) group.
func okHosts(state *State) ([]string, error) {
	if state.Grouped == nil {
		return nil, fmt.Errorf("@ok: no previous command results")
	}
	for _, g := range state.Grouped.Groups {
		if g.IsNorm {
			return g.Hosts, nil
		}
	}
	return nil, nil
}

// differsHosts returns hosts in non-norm groups.
func differsHosts(state *State) ([]string, error) {
	if state.Grouped == nil {
		return nil, fmt.Errorf("@differs: no previous command results")
	}
	var hosts []string
	for _, g := range state.Grouped.Groups {
		if !g.IsNorm {
			hosts = append(hosts, g.Hosts...)
		}
	}
	return hosts, nil
}

// failedHosts returns hosts that did not succeed: connection errors, non-zero
// exit codes, and timeouts.
func failedHosts(state *State) ([]string, error) {
	if state.Grouped == nil {
		return nil, fmt.Errorf("@failed: no previous command results")
	}
	var hosts []string
	for _, r := range state.Grouped.Failed {
		hosts = append(hosts, r.Host)
	}
	for _, g := range state.Grouped.Groups {
		if g.ExitCode != 0 {
			hosts = append(hosts, g.Hosts...)
		}
	}
	for _, r := range state.Grouped.TimedOut {
		hosts = append(hosts, r.Host)
	}
	return hosts, nil
}

// timeoutHosts returns hosts that timed out.
func timeoutHosts(state *State) ([]string, error) {
	if state.Grouped == nil {
		return nil, fmt.Errorf("@timeout: no previous command results")
	}
	var hosts []string
	for _, r := range state.Grouped.TimedOut {
		hosts = append(hosts, r.Host)
	}
	return hosts, nil
}

// matchHosts returns hosts whose names match the given glob pattern.
func matchHosts(pattern string, allHosts []string) ([]string, error) {
	if _, err := path.Match(pattern, ""); err != nil {
		return nil, fmt.Errorf("invalid pattern %q: %w", pattern, err)
	}

	var matched []string
	for _, h := range allHosts {
		if ok, _ := path.Match(pattern, h); ok {
			matched = append(matched, h)
		}
	}

	if len(matched) == 0 {
		return nil, fmt.Errorf("no hosts match @%s", pattern)
	}

	return matched, nil
}
