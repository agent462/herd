package exec

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/agent462/herd/internal/executor"
	"github.com/agent462/herd/internal/grouper"
)

func TestFormatGroupedIdentical(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "host-a", Stdout: []byte("hello\n"), ExitCode: 0},
		{Host: "host-b", Stdout: []byte("hello\n"), ExitCode: 0},
		{Host: "host-c", Stdout: []byte("hello\n"), ExitCode: 0},
	}

	grouped := grouper.Group(results)
	f := NewFormatter(false, false, false)
	output := f.Format(grouped)

	if !strings.Contains(output, "3 hosts identical:") {
		t.Errorf("expected '3 hosts identical:', got:\n%s", output)
	}
	if !strings.Contains(output, "host-a, host-b, host-c") {
		t.Errorf("expected host list, got:\n%s", output)
	}
	if !strings.Contains(output, "hello") {
		t.Errorf("expected output content, got:\n%s", output)
	}
	if !strings.Contains(output, "3 succeeded") {
		t.Errorf("expected summary line, got:\n%s", output)
	}
}

func TestFormatWithDiffs(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "host-a", Stdout: []byte("Debian 12\n"), ExitCode: 0},
		{Host: "host-b", Stdout: []byte("Debian 12\n"), ExitCode: 0},
		{Host: "host-c", Stdout: []byte("Debian 11\n"), ExitCode: 0},
	}

	grouped := grouper.Group(results)
	f := NewFormatter(false, false, false)
	output := f.Format(grouped)

	if !strings.Contains(output, "2 hosts identical:") {
		t.Errorf("expected '2 hosts identical:', got:\n%s", output)
	}
	if !strings.Contains(output, "1 host differs:") {
		t.Errorf("expected '1 host differs:', got:\n%s", output)
	}
	if !strings.Contains(output, "-Debian 12") {
		t.Errorf("expected diff removal line, got:\n%s", output)
	}
	if !strings.Contains(output, "+Debian 11") {
		t.Errorf("expected diff addition line, got:\n%s", output)
	}
	if !strings.Contains(output, "3 succeeded") {
		t.Errorf("expected summary, got:\n%s", output)
	}
}

func TestFormatJSON(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "host-a", Stdout: []byte("ok\n"), ExitCode: 0, Duration: 2 * time.Second},
		{Host: "host-b", Stdout: []byte("ok\n"), ExitCode: 0, Duration: time.Second},
		{Host: "host-c", Err: errors.New("connection refused"), Duration: 0},
	}

	f := NewFormatter(true, false, false)
	data, err := f.FormatJSON(results)
	if err != nil {
		t.Fatalf("FormatJSON error: %v", err)
	}

	var parsed []map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if len(parsed) != 3 {
		t.Fatalf("expected 3 results, got %d", len(parsed))
	}

	// Check that error field is present for the failed host.
	if parsed[2]["error"] != "connection refused" {
		t.Errorf("expected error 'connection refused', got %v", parsed[2]["error"])
	}
	// Check that error field is absent for successful hosts.
	if _, ok := parsed[0]["error"]; ok {
		t.Errorf("expected no error field for successful host, got %v", parsed[0]["error"])
	}
}

func TestFormatErrorsOnly(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "host-a", Stdout: []byte("ok\n"), ExitCode: 0},
		{Host: "host-b", Stdout: []byte("ok\n"), ExitCode: 0},
		{Host: "host-c", Err: errors.New("connection refused")},
	}

	grouped := grouper.Group(results)
	f := NewFormatter(false, true, false)
	output := f.Format(grouped)

	// Should NOT show the successful group.
	if strings.Contains(output, "identical") {
		t.Errorf("errors-only mode should not show identical group, got:\n%s", output)
	}
	// Should show the failed host.
	if !strings.Contains(output, "host-c") {
		t.Errorf("expected failed host 'host-c', got:\n%s", output)
	}
	if !strings.Contains(output, "connection refused") {
		t.Errorf("expected error message, got:\n%s", output)
	}
	// Summary should still appear.
	if !strings.Contains(output, "2 succeeded") {
		t.Errorf("expected summary with 2 succeeded, got:\n%s", output)
	}
	if !strings.Contains(output, "1 failed") {
		t.Errorf("expected summary with 1 failed, got:\n%s", output)
	}
}

func TestFormatSummaryLine(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "host-a", Stdout: []byte("ok\n"), ExitCode: 0},
		{Host: "host-b", Stdout: []byte("ok\n"), ExitCode: 0},
		{Host: "host-c", Err: errors.New("connection refused")},
		{Host: "host-d", Err: context.DeadlineExceeded},
	}

	grouped := grouper.Group(results)
	f := NewFormatter(false, false, false)
	output := f.Format(grouped)

	if !strings.Contains(output, "2 succeeded") {
		t.Errorf("expected '2 succeeded' in summary, got:\n%s", output)
	}
	if !strings.Contains(output, "1 failed") {
		t.Errorf("expected '1 failed' in summary, got:\n%s", output)
	}
	if !strings.Contains(output, "1 timeout") {
		t.Errorf("expected '1 timeout' in summary, got:\n%s", output)
	}
}

func TestFormatWithColor(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "host-a", Stdout: []byte("ok\n"), ExitCode: 0},
		{Host: "host-b", Stdout: []byte("ok\n"), ExitCode: 0},
	}

	grouped := grouper.Group(results)
	f := NewFormatter(false, false, true)
	output := f.Format(grouped)

	// Should contain ANSI escape codes.
	if !strings.Contains(output, "\033[") {
		t.Errorf("expected ANSI color codes in output, got:\n%s", output)
	}
}

func TestFormatWithoutColor(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "host-a", Stdout: []byte("ok\n"), ExitCode: 0},
	}

	grouped := grouper.Group(results)
	f := NewFormatter(false, false, false)
	output := f.Format(grouped)

	// Should NOT contain ANSI escape codes.
	if strings.Contains(output, "\033[") {
		t.Errorf("expected no ANSI color codes, got:\n%s", output)
	}
}

func TestFormatGroupedWithStderr(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "host-a", Stdout: []byte("ok\n"), Stderr: []byte("deprecation warning\n"), ExitCode: 0},
		{Host: "host-b", Stdout: []byte("ok\n"), Stderr: []byte("deprecation warning\n"), ExitCode: 0},
	}

	grouped := grouper.Group(results)
	f := NewFormatter(false, false, false)
	output := f.Format(grouped)

	if !strings.Contains(output, "stderr: deprecation warning") {
		t.Errorf("expected stderr line in output, got:\n%s", output)
	}
	if !strings.Contains(output, "2 hosts identical:") {
		t.Errorf("expected '2 hosts identical:', got:\n%s", output)
	}
}

func TestFormatSingleHost(t *testing.T) {
	results := []*executor.HostResult{
		{Host: "only-host", Stdout: []byte("output\n"), ExitCode: 0},
	}

	grouped := grouper.Group(results)
	f := NewFormatter(false, false, false)
	output := f.Format(grouped)

	if !strings.Contains(output, "1 host:") {
		t.Errorf("expected '1 host:', got:\n%s", output)
	}
	if strings.Contains(output, "identical") {
		t.Errorf("single host should not say 'identical', got:\n%s", output)
	}
	if !strings.Contains(output, "1 succeeded") {
		t.Errorf("expected '1 succeeded', got:\n%s", output)
	}
}
