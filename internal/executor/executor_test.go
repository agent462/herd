package executor

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// mockRunner is a configurable mock for testing the executor.
type mockRunner struct {
	handler func(ctx context.Context, host string, command string) *HostResult
}

func (m *mockRunner) Run(ctx context.Context, host string, command string) *HostResult {
	return m.handler(ctx, host, command)
}

func TestExecute_Success(t *testing.T) {
	runner := &mockRunner{
		handler: func(ctx context.Context, host string, command string) *HostResult {
			return &HostResult{
				Host:     host,
				Stdout:   []byte("hello from " + host),
				ExitCode: 0,
			}
		},
	}

	e := New(runner)
	hosts := []string{"host-a", "host-b", "host-c"}
	results := e.Execute(context.Background(), hosts, "echo hello")

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	for i, r := range results {
		if r.Host != hosts[i] {
			t.Errorf("result[%d]: expected host %q, got %q", i, hosts[i], r.Host)
		}
		if r.Err != nil {
			t.Errorf("result[%d]: unexpected error: %v", i, r.Err)
		}
		expected := "hello from " + hosts[i]
		if string(r.Stdout) != expected {
			t.Errorf("result[%d]: expected stdout %q, got %q", i, expected, string(r.Stdout))
		}
		if r.Duration == 0 {
			t.Errorf("result[%d]: duration should be non-zero", i)
		}
	}
}

func TestExecute_PreservesHostOrder(t *testing.T) {
	// Hosts complete in reverse order, but results should match input order.
	runner := &mockRunner{
		handler: func(ctx context.Context, host string, command string) *HostResult {
			switch host {
			case "slow":
				time.Sleep(50 * time.Millisecond)
			case "medium":
				time.Sleep(25 * time.Millisecond)
			case "fast":
				// no delay
			}
			return &HostResult{
				Host:   host,
				Stdout: []byte(host),
			}
		},
	}

	e := New(runner)
	hosts := []string{"slow", "medium", "fast"}
	results := e.Execute(context.Background(), hosts, "test")

	for i, r := range results {
		if r.Host != hosts[i] {
			t.Errorf("result[%d]: expected host %q, got %q", i, hosts[i], r.Host)
		}
	}
}

func TestExecute_ConcurrencyLimiting(t *testing.T) {
	var running atomic.Int32
	var maxRunning atomic.Int32

	runner := &mockRunner{
		handler: func(ctx context.Context, host string, command string) *HostResult {
			cur := running.Add(1)
			// Track the maximum number of concurrently running tasks.
			for {
				prev := maxRunning.Load()
				if cur <= prev || maxRunning.CompareAndSwap(prev, cur) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			running.Add(-1)
			return &HostResult{Host: host}
		},
	}

	e := New(runner, WithConcurrency(2))
	hosts := []string{"a", "b", "c", "d"}
	results := e.Execute(context.Background(), hosts, "test")

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	peak := maxRunning.Load()
	if peak > 2 {
		t.Errorf("expected max concurrency of 2, but %d were running simultaneously", peak)
	}
	if peak < 2 {
		t.Errorf("expected concurrency to reach 2, but peak was %d", peak)
	}
}

func TestExecute_PerHostTimeout(t *testing.T) {
	runner := &mockRunner{
		handler: func(ctx context.Context, host string, command string) *HostResult {
			select {
			case <-time.After(5 * time.Second):
				return &HostResult{Host: host, Stdout: []byte("done")}
			case <-ctx.Done():
				return &HostResult{Host: host, Err: ctx.Err()}
			}
		},
	}

	e := New(runner, WithTimeout(50*time.Millisecond))
	results := e.Execute(context.Background(), []string{"slow-host"}, "sleep 100")

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if results[0].Err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", results[0].Err)
	}
}

func TestExecute_ContextCancellation(t *testing.T) {
	var started atomic.Int32
	runner := &mockRunner{
		handler: func(ctx context.Context, host string, command string) *HostResult {
			started.Add(1)
			select {
			case <-time.After(10 * time.Second):
				return &HostResult{Host: host}
			case <-ctx.Done():
				return &HostResult{Host: host, Err: ctx.Err()}
			}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	e := New(runner)

	done := make(chan []*HostResult, 1)
	go func() {
		done <- e.Execute(ctx, []string{"host-1", "host-2"}, "long-command")
	}()

	// Wait for at least one goroutine to start, then cancel.
	for started.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()

	results := <-done
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, r := range results {
		if r.Err == nil {
			t.Errorf("host %q: expected cancellation error, got nil", r.Host)
		}
	}
}

func TestExecute_MixedResults(t *testing.T) {
	runner := &mockRunner{
		handler: func(ctx context.Context, host string, command string) *HostResult {
			switch host {
			case "ok-host":
				return &HostResult{Host: host, Stdout: []byte("ok"), ExitCode: 0}
			case "fail-host":
				return &HostResult{Host: host, Stderr: []byte("error"), ExitCode: 1}
			case "timeout-host":
				select {
				case <-time.After(10 * time.Second):
					return &HostResult{Host: host}
				case <-ctx.Done():
					return &HostResult{Host: host, Err: ctx.Err()}
				}
			case "error-host":
				return &HostResult{Host: host, Err: fmt.Errorf("connection refused")}
			default:
				return &HostResult{Host: host}
			}
		},
	}

	e := New(runner, WithTimeout(50*time.Millisecond))
	hosts := []string{"ok-host", "fail-host", "timeout-host", "error-host"}
	results := e.Execute(context.Background(), hosts, "check")

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	// ok-host: success
	if results[0].ExitCode != 0 || results[0].Err != nil {
		t.Errorf("ok-host: expected success, got exit=%d err=%v", results[0].ExitCode, results[0].Err)
	}

	// fail-host: non-zero exit
	if results[1].ExitCode != 1 {
		t.Errorf("fail-host: expected exit code 1, got %d", results[1].ExitCode)
	}

	// timeout-host: deadline exceeded
	if results[2].Err != context.DeadlineExceeded {
		t.Errorf("timeout-host: expected DeadlineExceeded, got %v", results[2].Err)
	}

	// error-host: connection error
	if results[3].Err == nil || results[3].Err.Error() != "connection refused" {
		t.Errorf("error-host: expected 'connection refused' error, got %v", results[3].Err)
	}
}

func TestExecute_ZeroHosts(t *testing.T) {
	runner := &mockRunner{
		handler: func(ctx context.Context, host string, command string) *HostResult {
			t.Fatal("runner should not be called with zero hosts")
			return nil
		},
	}

	e := New(runner)
	results := e.Execute(context.Background(), nil, "test")

	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestNew_Defaults(t *testing.T) {
	runner := &mockRunner{}
	e := New(runner)

	if e.concurrency != 20 {
		t.Errorf("expected default concurrency 20, got %d", e.concurrency)
	}
	if e.timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", e.timeout)
	}
}

func TestNew_WithOptions(t *testing.T) {
	runner := &mockRunner{}
	e := New(runner, WithConcurrency(5), WithTimeout(10*time.Second))

	if e.concurrency != 5 {
		t.Errorf("expected concurrency 5, got %d", e.concurrency)
	}
	if e.timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", e.timeout)
	}
}

func TestWithConcurrency_IgnoresInvalid(t *testing.T) {
	runner := &mockRunner{}
	e := New(runner, WithConcurrency(0), WithConcurrency(-1))

	if e.concurrency != 20 {
		t.Errorf("expected default concurrency 20, got %d", e.concurrency)
	}
}

func TestWithTimeout_IgnoresInvalid(t *testing.T) {
	runner := &mockRunner{}
	e := New(runner, WithTimeout(0), WithTimeout(-1*time.Second))

	if e.timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", e.timeout)
	}
}
