package grouper

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/agent462/herd/internal/executor"
)

// OutputGroup represents a set of hosts that produced identical output.
type OutputGroup struct {
	Hosts    []string
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	IsNorm   bool   // true if this is the largest (majority) group
	Diff     string // unified diff vs the norm group; empty for the norm itself
}

// GroupedResults holds the categorized results of a parallel command execution.
type GroupedResults struct {
	Groups   []OutputGroup
	Failed   []*executor.HostResult
	TimedOut []*executor.HostResult
}

// Group categorizes host results by identical output and exit code, identifies
// the majority group as the "norm", and computes unified diffs for outliers.
// Both zero and non-zero exit code results are grouped together so that (e.g.)
// 20 hosts returning exit code 3 with the same output appear as a single group
// rather than 20 individual entries.
func Group(results []*executor.HostResult) *GroupedResults {
	gr := &GroupedResults{}

	// Separate errors from completed results.
	type hashEntry struct {
		hash   string
		result *executor.HostResult
	}

	var completed []hashEntry

	for _, r := range results {
		if r.Err != nil {
			if isTimeout(r.Err) {
				gr.TimedOut = append(gr.TimedOut, r)
			} else {
				gr.Failed = append(gr.Failed, r)
			}
			continue
		}

		// Include exit code in the hash so that hosts with the same output
		// but different exit codes land in separate groups.
		var hashBuf []byte
		hashBuf = append(hashBuf, r.Stdout...)
		hashBuf = append(hashBuf, 0) // NUL separator prevents collisions
		hashBuf = append(hashBuf, r.Stderr...)
		hashBuf = append(hashBuf, 0)
		hashBuf = append(hashBuf, byte(r.ExitCode>>24), byte(r.ExitCode>>16), byte(r.ExitCode>>8), byte(r.ExitCode))
		h := sha256.Sum256(hashBuf)
		completed = append(completed, hashEntry{
			hash:   fmt.Sprintf("%x", h),
			result: r,
		})
	}

	if len(completed) == 0 {
		return gr
	}

	// Group by hash.
	type groupData struct {
		hosts    []string
		stdout   []byte
		stderr   []byte
		exitCode int
	}
	groups := make(map[string]*groupData)
	// Track insertion order for deterministic output.
	var hashOrder []string

	for _, entry := range completed {
		g, ok := groups[entry.hash]
		if !ok {
			g = &groupData{
				stdout:   entry.result.Stdout,
				stderr:   entry.result.Stderr,
				exitCode: entry.result.ExitCode,
			}
			groups[entry.hash] = g
			hashOrder = append(hashOrder, entry.hash)
		}
		g.hosts = append(g.hosts, entry.result.Host)
	}

	// Find the norm (largest group). On tie, use the group that appeared first.
	normHash := hashOrder[0]
	normSize := len(groups[hashOrder[0]].hosts)
	for _, h := range hashOrder[1:] {
		if len(groups[h].hosts) > normSize {
			normHash = h
			normSize = len(groups[h].hosts)
		}
	}

	normStdout := string(groups[normHash].stdout)

	// Build output groups. Norm group first, then outliers in insertion order.
	normGroup := groups[normHash]
	sort.Strings(normGroup.hosts)
	gr.Groups = append(gr.Groups, OutputGroup{
		Hosts:    normGroup.hosts,
		Stdout:   normGroup.stdout,
		Stderr:   normGroup.stderr,
		ExitCode: normGroup.exitCode,
		IsNorm:   true,
	})

	for _, h := range hashOrder {
		if h == normHash {
			continue
		}
		g := groups[h]
		sort.Strings(g.hosts)
		diff := unifiedDiff(normStdout, string(g.stdout))
		gr.Groups = append(gr.Groups, OutputGroup{
			Hosts:    g.hosts,
			Stdout:   g.stdout,
			Stderr:   g.stderr,
			ExitCode: g.exitCode,
			IsNorm:   false,
			Diff:     diff,
		})
	}

	return gr
}

// isTimeout checks if an error represents a timeout.
func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

// maxDiffLines is the maximum number of lines (in either input) before
// the diff engine gives up computing an LCS and falls back to showing
// the full removal/addition. This avoids O(n*m) blowup on very large outputs.
const maxDiffLines = 500

// unifiedDiff computes a simple unified diff between two strings.
func unifiedDiff(a, b string) string {
	aLines := splitLines(a)
	bLines := splitLines(b)

	// For very large outputs, skip LCS and show full removal/addition.
	if len(aLines) > maxDiffLines || len(bLines) > maxDiffLines {
		var out strings.Builder
		out.WriteString("--- norm\n")
		out.WriteString("+++ outlier\n")
		for _, line := range aLines {
			out.WriteString("-")
			out.WriteString(line)
			out.WriteString("\n")
		}
		for _, line := range bLines {
			out.WriteString("+")
			out.WriteString(line)
			out.WriteString("\n")
		}
		return out.String()
	}

	// Compute LCS-based diff.
	lcs := computeLCS(aLines, bLines)

	var out strings.Builder
	out.WriteString("--- norm\n")
	out.WriteString("+++ outlier\n")

	ai, bi, li := 0, 0, 0

	for li < len(lcs) {
		// Lines removed from a (not in b).
		for ai < len(aLines) && aLines[ai] != lcs[li] {
			out.WriteString("-")
			out.WriteString(aLines[ai])
			out.WriteString("\n")
			ai++
		}
		// Lines added in b (not in a).
		for bi < len(bLines) && bLines[bi] != lcs[li] {
			out.WriteString("+")
			out.WriteString(bLines[bi])
			out.WriteString("\n")
			bi++
		}
		// Common line.
		out.WriteString(" ")
		out.WriteString(lcs[li])
		out.WriteString("\n")
		ai++
		bi++
		li++
	}

	// Remaining lines after LCS is exhausted.
	for ai < len(aLines) {
		out.WriteString("-")
		out.WriteString(aLines[ai])
		out.WriteString("\n")
		ai++
	}
	for bi < len(bLines) {
		out.WriteString("+")
		out.WriteString(bLines[bi])
		out.WriteString("\n")
		bi++
	}

	return out.String()
}

// splitLines splits a string into lines, handling the trailing newline gracefully.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// Remove trailing empty element from a trailing newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// computeLCS returns the longest common subsequence of two string slices.
func computeLCS(a, b []string) []string {
	m, n := len(a), len(b)
	// Build DP table.
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack to find the LCS.
	lcs := make([]string, 0, dp[m][n])
	i, j := m, n
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			lcs = append(lcs, a[i-1])
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			i--
		} else {
			j--
		}
	}
	// Reverse.
	for l, r := 0, len(lcs)-1; l < r; l, r = l+1, r-1 {
		lcs[l], lcs[r] = lcs[r], lcs[l]
	}
	return lcs
}
