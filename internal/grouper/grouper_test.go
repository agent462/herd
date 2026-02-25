package grouper

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/bryanhitc/herd/internal/executor"
)

func TestGroupAllIdentical(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "host-a", Stdout: []byte("hello\n"), ExitCode: 0, Duration: time.Second},
		{Host: "host-b", Stdout: []byte("hello\n"), ExitCode: 0, Duration: time.Second},
		{Host: "host-c", Stdout: []byte("hello\n"), ExitCode: 0, Duration: time.Second},
	}

	gr := Group(results)

	if len(gr.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(gr.Groups))
	}
	if !gr.Groups[0].IsNorm {
		t.Error("single group should be marked as norm")
	}
	if len(gr.Groups[0].Hosts) != 3 {
		t.Errorf("expected 3 hosts in group, got %d", len(gr.Groups[0].Hosts))
	}
	if gr.Groups[0].Diff != "" {
		t.Error("norm group should have empty diff")
	}
	if len(gr.Failed) != 0 {
		t.Errorf("expected 0 failed, got %d", len(gr.Failed))
	}
	if len(gr.TimedOut) != 0 {
		t.Errorf("expected 0 timed out, got %d", len(gr.TimedOut))
	}
}

func TestGroupTwoGroups(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "host-a", Stdout: []byte("Debian 12\n"), ExitCode: 0},
		{Host: "host-b", Stdout: []byte("Debian 12\n"), ExitCode: 0},
		{Host: "host-c", Stdout: []byte("Debian 11\n"), ExitCode: 0},
	}

	gr := Group(results)

	if len(gr.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(gr.Groups))
	}

	// Norm should be the larger group.
	norm := gr.Groups[0]
	if !norm.IsNorm {
		t.Error("first group should be the norm")
	}
	if len(norm.Hosts) != 2 {
		t.Errorf("norm group should have 2 hosts, got %d", len(norm.Hosts))
	}
	if string(norm.Stdout) != "Debian 12\n" {
		t.Errorf("norm stdout = %q, want %q", norm.Stdout, "Debian 12\n")
	}

	outlier := gr.Groups[1]
	if outlier.IsNorm {
		t.Error("second group should not be norm")
	}
	if len(outlier.Hosts) != 1 {
		t.Errorf("outlier group should have 1 host, got %d", len(outlier.Hosts))
	}
	if outlier.Diff == "" {
		t.Error("outlier group should have a non-empty diff")
	}
	// Verify the diff contains the expected change markers.
	if !strings.Contains(outlier.Diff, "-Debian 12") {
		t.Errorf("diff should show removal of 'Debian 12', got:\n%s", outlier.Diff)
	}
	if !strings.Contains(outlier.Diff, "+Debian 11") {
		t.Errorf("diff should show addition of 'Debian 11', got:\n%s", outlier.Diff)
	}
}

func TestGroupSingleHost(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "only-host", Stdout: []byte("output\n"), ExitCode: 0},
	}

	gr := Group(results)

	if len(gr.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(gr.Groups))
	}
	if !gr.Groups[0].IsNorm {
		t.Error("single host group should be norm")
	}
	if gr.Groups[0].Hosts[0] != "only-host" {
		t.Errorf("expected host 'only-host', got %q", gr.Groups[0].Hosts[0])
	}
}

func TestGroupMixedSuccessAndFailure(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "host-a", Stdout: []byte("ok\n"), ExitCode: 0},
		{Host: "host-b", Stdout: []byte("ok\n"), ExitCode: 0},
		{Host: "host-c", Err: errors.New("connection refused")},
		{Host: "host-d", Err: context.DeadlineExceeded},
	}

	gr := Group(results)

	if len(gr.Groups) != 1 {
		t.Fatalf("expected 1 successful group, got %d", len(gr.Groups))
	}
	if len(gr.Groups[0].Hosts) != 2 {
		t.Errorf("expected 2 hosts in successful group, got %d", len(gr.Groups[0].Hosts))
	}
	if len(gr.Failed) != 1 {
		t.Errorf("expected 1 failed host, got %d", len(gr.Failed))
	}
	if gr.Failed[0].Host != "host-c" {
		t.Errorf("expected failed host 'host-c', got %q", gr.Failed[0].Host)
	}
	if len(gr.TimedOut) != 1 {
		t.Errorf("expected 1 timed out host, got %d", len(gr.TimedOut))
	}
	if gr.TimedOut[0].Host != "host-d" {
		t.Errorf("expected timed out host 'host-d', got %q", gr.TimedOut[0].Host)
	}
}

func TestGroupEmptyResults(t *testing.T) {
	gr := Group(nil)

	if len(gr.Groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(gr.Groups))
	}
	if len(gr.Failed) != 0 {
		t.Errorf("expected 0 failed, got %d", len(gr.Failed))
	}
	if len(gr.TimedOut) != 0 {
		t.Errorf("expected 0 timed out, got %d", len(gr.TimedOut))
	}
}

func TestGroupHostsSorted(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "charlie", Stdout: []byte("x\n"), ExitCode: 0},
		{Host: "alpha", Stdout: []byte("x\n"), ExitCode: 0},
		{Host: "bravo", Stdout: []byte("x\n"), ExitCode: 0},
	}

	gr := Group(results)

	if len(gr.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(gr.Groups))
	}
	hosts := gr.Groups[0].Hosts
	if hosts[0] != "alpha" || hosts[1] != "bravo" || hosts[2] != "charlie" {
		t.Errorf("hosts not sorted: %v", hosts)
	}
}

func TestGroupNonZeroExitSeparated(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "host-a", Stdout: []byte("ok\n"), ExitCode: 0},
		{Host: "host-b", Stdout: []byte("ok\n"), ExitCode: 0},
		{Host: "host-c", Stdout: []byte("fail\n"), Stderr: []byte("error\n"), ExitCode: 1},
		{Host: "host-d", Stdout: []byte("nope\n"), ExitCode: 2},
	}

	gr := Group(results)

	if len(gr.Groups) != 1 {
		t.Fatalf("expected 1 successful group, got %d", len(gr.Groups))
	}
	if len(gr.Groups[0].Hosts) != 2 {
		t.Errorf("expected 2 hosts in successful group, got %d", len(gr.Groups[0].Hosts))
	}
	if len(gr.NonZero) != 2 {
		t.Fatalf("expected 2 non-zero hosts, got %d", len(gr.NonZero))
	}
	if gr.NonZero[0].Host != "host-c" {
		t.Errorf("expected non-zero host 'host-c', got %q", gr.NonZero[0].Host)
	}
	if gr.NonZero[0].ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", gr.NonZero[0].ExitCode)
	}
	if gr.NonZero[1].Host != "host-d" {
		t.Errorf("expected non-zero host 'host-d', got %q", gr.NonZero[1].Host)
	}
}

func TestGroupAllFailed(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "host-a", Err: errors.New("connection refused")},
		{Host: "host-b", Err: errors.New("auth failed")},
	}

	gr := Group(results)

	if len(gr.Groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(gr.Groups))
	}
	if len(gr.Failed) != 2 {
		t.Errorf("expected 2 failed, got %d", len(gr.Failed))
	}
}

func TestGroupDifferentStderr(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "host-a", Stdout: []byte("ok\n"), Stderr: []byte("warn1\n"), ExitCode: 0},
		{Host: "host-b", Stdout: []byte("ok\n"), Stderr: []byte("warn2\n"), ExitCode: 0},
	}

	gr := Group(results)

	// Same stdout but different stderr → two separate groups.
	if len(gr.Groups) != 2 {
		t.Fatalf("expected 2 groups (different stderr), got %d", len(gr.Groups))
	}
}

func TestGroupSameStderrGroupedTogether(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "host-a", Stdout: []byte("ok\n"), Stderr: []byte("warn\n"), ExitCode: 0},
		{Host: "host-b", Stdout: []byte("ok\n"), Stderr: []byte("warn\n"), ExitCode: 0},
		{Host: "host-c", Stdout: []byte("ok\n"), Stderr: []byte("warn\n"), ExitCode: 0},
	}

	gr := Group(results)

	if len(gr.Groups) != 1 {
		t.Fatalf("expected 1 group (same stdout+stderr), got %d", len(gr.Groups))
	}
	if len(gr.Groups[0].Hosts) != 3 {
		t.Errorf("expected 3 hosts in group, got %d", len(gr.Groups[0].Hosts))
	}
}

// timeoutError implements net.Error with Timeout() == true.
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }

func TestIsTimeout(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"context.DeadlineExceeded", context.DeadlineExceeded, true},
		{"wrapped DeadlineExceeded", fmt.Errorf("connect: %w", context.DeadlineExceeded), true},
		{"net timeout error", &timeoutError{}, true},
		{"wrapped net timeout", fmt.Errorf("dial: %w", &timeoutError{}), true},
		{"plain error", errors.New("connection refused"), false},
		{"net non-timeout error", &net.OpError{Op: "dial", Err: errors.New("refused")}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isTimeout(tc.err)
			if got != tc.want {
				t.Errorf("isTimeout(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestUnifiedDiff(t *testing.T) {
	a := "line1\nline2\nline3\n"
	b := "line1\nchanged\nline3\n"

	diff := unifiedDiff(a, b)

	if !strings.Contains(diff, "-line2") {
		t.Errorf("diff should contain '-line2', got:\n%s", diff)
	}
	if !strings.Contains(diff, "+changed") {
		t.Errorf("diff should contain '+changed', got:\n%s", diff)
	}
	if !strings.Contains(diff, " line1") {
		t.Errorf("diff should contain ' line1' (context), got:\n%s", diff)
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"", 0},
		{"a\n", 1},
		{"a\nb\n", 2},
		{"a\nb", 2},
	}

	for _, tc := range tests {
		got := splitLines(tc.input)
		if len(got) != tc.want {
			t.Errorf("splitLines(%q) = %d lines, want %d", tc.input, len(got), tc.want)
		}
	}
}
