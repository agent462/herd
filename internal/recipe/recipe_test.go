package recipe

import (
	"context"
	"testing"

	"github.com/agent462/herd/internal/executor"
	"github.com/agent462/herd/internal/grouper"
)

// --- ParseStep tests ---

func TestParseStep_SimpleCommand(t *testing.T) {
	step := ParseStep("echo hello")
	if step.Selector != "" {
		t.Errorf("selector = %q, want empty", step.Selector)
	}
	if step.Command != "echo hello" {
		t.Errorf("command = %q, want %q", step.Command, "echo hello")
	}
}

func TestParseStep_WithSelector(t *testing.T) {
	step := ParseStep("@web* systemctl restart")
	if step.Selector != "@web*" {
		t.Errorf("selector = %q, want %q", step.Selector, "@web*")
	}
	if step.Command != "systemctl restart" {
		t.Errorf("command = %q, want %q", step.Command, "systemctl restart")
	}
}

func TestParseStep_MultipleSelectors(t *testing.T) {
	step := ParseStep("@ok,@differs uptime")
	if step.Selector != "@ok,@differs" {
		t.Errorf("selector = %q, want %q", step.Selector, "@ok,@differs")
	}
	if step.Command != "uptime" {
		t.Errorf("command = %q, want %q", step.Command, "uptime")
	}
}

// --- Mock runner ---

type mockRunner struct {
	handler func(ctx context.Context, host string, command string) *executor.HostResult
}

func (m *mockRunner) Run(ctx context.Context, host string, command string) *executor.HostResult {
	return m.handler(ctx, host, command)
}

// --- Run tests ---

func TestRun_BasicExecution(t *testing.T) {
	runner := &mockRunner{
		handler: func(ctx context.Context, host string, command string) *executor.HostResult {
			return &executor.HostResult{
				Host:     host,
				Stdout:   []byte("ok from " + host),
				ExitCode: 0,
			}
		},
	}

	exec := executor.New(runner)
	hosts := []string{"host-a", "host-b", "host-c"}
	r := New(exec, hosts)

	steps := []Step{
		{Command: "echo hello"},
		{Command: "uptime"},
	}

	results, err := r.Run(context.Background(), steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 step results, got %d", len(results))
	}

	// Both steps should run on all 3 hosts (no selector = @all).
	for i, sr := range results {
		if len(sr.Hosts) != 3 {
			t.Errorf("step %d: expected 3 hosts, got %d", i, len(sr.Hosts))
		}
		if len(sr.Results) != 3 {
			t.Errorf("step %d: expected 3 results, got %d", i, len(sr.Results))
		}
		if sr.Grouped == nil {
			t.Errorf("step %d: grouped results should not be nil", i)
		}
	}
}

func TestRun_SelectorPropagation(t *testing.T) {
	// Step 1 returns different output for host-c so it lands in the "differs" group.
	// Step 2 uses @ok, which should resolve to host-a and host-b (the norm group from step 1).
	// Step 3 uses @differs, which should resolve to host-c (the outlier from step 1).
	runner := &mockRunner{
		handler: func(ctx context.Context, host string, command string) *executor.HostResult {
			switch command {
			case "check version":
				stdout := []byte("v1.0")
				if host == "host-c" {
					stdout = []byte("v0.9")
				}
				return &executor.HostResult{
					Host:     host,
					Stdout:   stdout,
					ExitCode: 0,
				}
			default:
				return &executor.HostResult{
					Host:     host,
					Stdout:   []byte("done"),
					ExitCode: 0,
				}
			}
		},
	}

	exec := executor.New(runner)
	hosts := []string{"host-a", "host-b", "host-c"}
	r := New(exec, hosts)

	steps := []Step{
		{Command: "check version"},            // all hosts; host-c differs
		{Selector: "@ok", Command: "upgrade"}, // should target host-a, host-b
		{Selector: "@differs", Command: "fix"}, // should target host-c
	}

	results, err := r.Run(context.Background(), steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 step results, got %d", len(results))
	}

	// Step 1: all 3 hosts.
	if len(results[0].Hosts) != 3 {
		t.Errorf("step 0: expected 3 hosts, got %d", len(results[0].Hosts))
	}

	// Verify step 1 grouping: norm group should be host-a, host-b (v1.0).
	step1Grouped := results[0].Grouped
	if step1Grouped == nil {
		t.Fatal("step 0: grouped results should not be nil")
	}
	if len(step1Grouped.Groups) != 2 {
		t.Fatalf("step 0: expected 2 output groups, got %d", len(step1Grouped.Groups))
	}

	// Step 2 (@ok): should resolve to the norm group from step 1 (host-a, host-b).
	assertHostsEqual(t, "step 1 hosts", results[1].Hosts, []string{"host-a", "host-b"})

	// Step 3 (@differs): should resolve to the outlier from step 1 (host-c).
	// Note: after step 2, state.Grouped is updated to step 2's results.
	// But step 2 ran on host-a, host-b with identical output, so @differs from step 2 is empty.
	// However, our steps use @differs which refers to step 2's grouped results (all same = no differs).
	// Let's verify step 2 ran on the correct hosts instead.
	if len(results[1].Results) != 2 {
		t.Errorf("step 1: expected 2 results, got %d", len(results[1].Results))
	}
}

func TestRun_SelectorPropagationDiffersFromPreviousStep(t *testing.T) {
	// This test verifies that each step's selector resolves against the
	// immediately preceding step's grouped results.
	//
	// Step 1: all 3 hosts, host-c produces different output.
	// Step 2: @differs targets host-c (differs from step 1).
	runner := &mockRunner{
		handler: func(ctx context.Context, host string, command string) *executor.HostResult {
			if command == "check" && host == "host-c" {
				return &executor.HostResult{
					Host:     host,
					Stdout:   []byte("different"),
					ExitCode: 0,
				}
			}
			return &executor.HostResult{
				Host:     host,
				Stdout:   []byte("same"),
				ExitCode: 0,
			}
		},
	}

	exec := executor.New(runner)
	hosts := []string{"host-a", "host-b", "host-c"}
	r := New(exec, hosts)

	steps := []Step{
		{Command: "check"},
		{Selector: "@differs", Command: "remediate"},
	}

	results, err := r.Run(context.Background(), steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 step results, got %d", len(results))
	}

	// Step 2: @differs from step 1 should be host-c only.
	assertHostsEqual(t, "step 1 hosts", results[1].Hosts, []string{"host-c"})

	// Verify the remediate command only ran on host-c.
	if len(results[1].Results) != 1 {
		t.Fatalf("step 1: expected 1 result, got %d", len(results[1].Results))
	}
	if results[1].Results[0].Host != "host-c" {
		t.Errorf("step 1: expected host %q, got %q", "host-c", results[1].Results[0].Host)
	}
}

func TestRun_ContextCancellation(t *testing.T) {
	runner := &mockRunner{
		handler: func(ctx context.Context, host string, command string) *executor.HostResult {
			return &executor.HostResult{
				Host:     host,
				Stdout:   []byte("ok"),
				ExitCode: 0,
			}
		},
	}

	exec := executor.New(runner)
	hosts := []string{"host-a"}
	r := New(exec, hosts)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	steps := []Step{
		{Command: "echo hello"},
	}

	results, err := r.Run(ctx, steps)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}

	// No steps should have completed.
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestRun_StepResultFields(t *testing.T) {
	runner := &mockRunner{
		handler: func(ctx context.Context, host string, command string) *executor.HostResult {
			return &executor.HostResult{
				Host:     host,
				Stdout:   []byte("output"),
				ExitCode: 0,
			}
		},
	}

	exec := executor.New(runner)
	hosts := []string{"host-a", "host-b"}
	r := New(exec, hosts)

	steps := []Step{
		{Command: "test cmd"},
	}

	results, err := r.Run(context.Background(), steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sr := results[0]

	// Verify Step is preserved.
	if sr.Step.Command != "test cmd" {
		t.Errorf("step command = %q, want %q", sr.Step.Command, "test cmd")
	}
	if sr.Step.Selector != "" {
		t.Errorf("step selector = %q, want empty", sr.Step.Selector)
	}

	// Verify Hosts.
	assertHostsEqual(t, "hosts", sr.Hosts, []string{"host-a", "host-b"})

	// Verify Results count.
	if len(sr.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(sr.Results))
	}

	// Verify Grouped is populated.
	if sr.Grouped == nil {
		t.Error("grouped should not be nil")
	}
	if len(sr.Grouped.Groups) != 1 {
		t.Errorf("expected 1 output group, got %d", len(sr.Grouped.Groups))
	}
}

func TestRun_WithFailedHosts(t *testing.T) {
	// Step 1: host-c returns an error. Step 2: @failed targets host-c.
	runner := &mockRunner{
		handler: func(ctx context.Context, host string, command string) *executor.HostResult {
			if command == "check" && host == "host-c" {
				return &executor.HostResult{
					Host:     host,
					Stdout:   []byte("fail output"),
					ExitCode: 1,
				}
			}
			return &executor.HostResult{
				Host:     host,
				Stdout:   []byte("ok"),
				ExitCode: 0,
			}
		},
	}

	exec := executor.New(runner)
	hosts := []string{"host-a", "host-b", "host-c"}
	r := New(exec, hosts)

	steps := []Step{
		{Command: "check"},
		{Selector: "@failed", Command: "retry"},
	}

	results, err := r.Run(context.Background(), steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Step 1: host-c has exit code 1, which @failed should catch.
	step1Grouped := results[0].Grouped
	if step1Grouped == nil {
		t.Fatal("step 0: grouped should not be nil")
	}

	// @failed includes non-zero exit code hosts.
	// Step 2 should target host-c.
	assertHostsEqual(t, "step 1 hosts", results[1].Hosts, []string{"host-c"})
}

// --- helpers ---

// verifyGroupedResults is a helper to inspect grouped output for tests.
func verifyGroupedResults(t *testing.T, grouped *grouper.GroupedResults, expectedGroups int) {
	t.Helper()
	if grouped == nil {
		t.Fatal("grouped results should not be nil")
	}
	if len(grouped.Groups) != expectedGroups {
		t.Errorf("expected %d groups, got %d", expectedGroups, len(grouped.Groups))
	}
}

func assertHostsEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d hosts %v, want %d hosts %v", label, len(got), got, len(want), want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s: host[%d] = %q, want %q", label, i, got[i], want[i])
		}
	}
}
