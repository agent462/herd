package dashboard

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/agent462/herd/internal/executor"
	"github.com/agent462/herd/internal/ssh"
)

// TestDashboardWithLiveHosts exercises the dashboard model against real SSH hosts.
// It verifies health checks, command execution, and message routing work end-to-end.
func TestDashboardWithLiveHosts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live SSH test in short mode")
	}

	rawHosts := []string{"signal@192.168.86.59", "signal@192.168.86.238"}

	baseConf := ssh.ClientConfig{
		AcceptUnknownHosts: true,
	}

	// Parse user@host into HostConfig, mirroring what buildSession does.
	hosts := make([]string, len(rawHosts))
	hostConfs := make(map[string]ssh.HostConfig, len(rawHosts))
	for i, raw := range rawHosts {
		hosts[i] = raw
		if idx := strings.Index(raw, "@"); idx > 0 {
			hostConfs[raw] = ssh.HostConfig{
				Hostname: raw[idx+1:],
				User:     raw[:idx],
			}
		} else {
			hostConfs[raw] = ssh.HostConfig{}
		}
	}

	pool := ssh.NewPool(baseConf, hostConfs)
	defer pool.Close()

	exec := executor.New(pool,
		executor.WithConcurrency(10),
		executor.WithTimeout(10*time.Second),
	)

	model := New(Config{
		Pool:           pool,
		Executor:       exec,
		AllHosts:       hosts,
		GroupName:      "test",
		HealthInterval: 2 * time.Second,
	})

	// Simulate WindowSizeMsg.
	sized, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model = sized.(Model)

	if model.width != 120 || model.height != 40 {
		t.Fatalf("expected 120x40, got %dx%d", model.width, model.height)
	}

	// Verify initial view renders without panic.
	view := model.View()
	if view.Content == "" {
		t.Fatal("expected non-empty view content")
	}

	// Test command execution (runs synchronously via the returned Cmd).
	cmd := model.executeCommand("hostname")
	if cmd == nil {
		t.Fatal("expected a non-nil command")
	}

	// Execute the command function to get the result message.
	msg := cmd()
	result, ok := msg.(execResultMsg)
	if !ok {
		t.Fatalf("expected execResultMsg, got %T", msg)
	}

	if result.Command != "hostname" {
		t.Fatalf("expected command 'hostname', got %q", result.Command)
	}
	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}
	if result.Grouped == nil {
		t.Fatal("expected grouped results")
	}

	// Feed the result back into the model.
	updated, _ := model.Update(result)
	model = updated.(Model)

	if model.lastCommand != "hostname" {
		t.Fatalf("expected lastCommand 'hostname', got %q", model.lastCommand)
	}
	if model.lastGrouped == nil {
		t.Fatal("expected lastGrouped to be set")
	}

	// Verify both results succeeded.
	for _, r := range result.Results {
		if r.Err != nil {
			t.Fatalf("host %s failed: %v", r.Host, r.Err)
		}
		if r.ExitCode != 0 {
			t.Fatalf("host %s exit code %d", r.Host, r.ExitCode)
		}
	}

	// Check that the results grouped as identical (both return "signal").
	if len(result.Grouped.Groups) != 1 {
		t.Fatalf("expected 1 group (identical output), got %d", len(result.Grouped.Groups))
	}
	if !result.Grouped.Groups[0].IsNorm {
		t.Fatal("expected the single group to be the norm")
	}

	// Test health check.
	healthCmd := healthCheckCmd(pool, hosts)
	healthMsg := healthCmd()
	hc, ok := healthMsg.(healthCheckMsg)
	if !ok {
		t.Fatalf("expected healthCheckMsg, got %T", healthMsg)
	}

	// After executing a command, connections should be cached in the pool.
	for _, h := range hosts {
		if !hc.Status[h] {
			t.Errorf("expected host %s to be connected", h)
		}
	}

	// Feed health status into model.
	updated, _ = model.Update(hc)
	model = updated.(Model)

	connCount := model.hostTable.ConnectedCount()
	if connCount != 2 {
		t.Fatalf("expected 2 connected hosts, got %d", connCount)
	}

	// Verify view renders with results.
	view = model.View()
	if view.Content == "" {
		t.Fatal("expected non-empty view after results")
	}

	// Verify durations are populated in host table entries.
	for _, entry := range model.hostTable.entries {
		if entry.Duration == "" {
			t.Fatalf("expected duration to be populated for host %s", entry.Name)
		}
		t.Logf("  %s: status=%s duration=%s", entry.Name, entry.Status, entry.Duration)
	}

	t.Logf("Dashboard integration test passed: %d hosts, %d groups, %d connected",
		len(hosts), len(result.Grouped.Groups), connCount)

	// --- Test focus cycling ---
	if model.focused != paneCommandInput {
		t.Fatalf("expected initial focus on command input, got %d", model.focused)
	}
	model = model.cycleFocus()
	if model.focused != paneHostTable {
		t.Fatalf("expected focus on host table after 1 Tab, got %d", model.focused)
	}
	model = model.cycleFocus()
	if model.focused != paneOutput {
		t.Fatalf("expected focus on output after 2 Tabs, got %d", model.focused)
	}
	model = model.cycleFocus()
	if model.focused != paneCommandInput {
		t.Fatalf("expected focus back on command input after 3 Tabs, got %d", model.focused)
	}

	// --- Test host expand/collapse ---
	selectedHost := hosts[0]
	model.outputPane.ExpandHost(selectedHost, model.lastGrouped, model.lastResults)
	if !model.outputPane.IsExpanded() {
		t.Fatal("expected output pane to be expanded")
	}
	view = model.View()
	if !strings.Contains(view.Content, selectedHost) {
		t.Fatal("expected expanded view to contain host name")
	}
	model.outputPane.CollapseHost(model.lastGrouped, model.lastResults)
	if model.outputPane.IsExpanded() {
		t.Fatal("expected output pane to be collapsed")
	}

	// --- Test diff view ---
	model.diffView.Show(selectedHost, model.lastGrouped, model.lastResults)
	if !model.diffView.IsVisible() {
		t.Fatal("expected diff view to be visible")
	}
	diffContent := model.diffView.View()
	if diffContent == "" {
		t.Fatal("expected non-empty diff view")
	}
	model.diffView.Hide()
	if model.diffView.IsVisible() {
		t.Fatal("expected diff view to be hidden")
	}

	// --- Test filter bar ---
	model.filterBar.Toggle()
	if !model.filterBar.IsVisible() {
		t.Fatal("expected filter bar to be visible")
	}
	if !model.filterBar.MatchesHost("signal@192.168.86.59") {
		t.Fatal("expected empty filter to match all hosts")
	}
	model.filterBar.Toggle()
	if model.filterBar.IsVisible() {
		t.Fatal("expected filter bar to be hidden")
	}

	// --- Test help overlay ---
	model.showHelp = true
	view = model.View()
	if !strings.Contains(view.Content, "Keyboard Shortcuts") {
		t.Fatal("expected help overlay content")
	}
	model.showHelp = false

	// --- Test @ok selector routing ---
	selectorCmd := model.executeCommand("@ok uptime")
	if selectorCmd == nil {
		t.Fatal("expected non-nil command from @ok selector")
	}
	selectorMsg := selectorCmd()
	selectorResult, ok := selectorMsg.(execResultMsg)
	if !ok {
		t.Fatalf("expected execResultMsg from selector command, got %T", selectorMsg)
	}
	if selectorResult.Command != "uptime" {
		t.Fatalf("expected command 'uptime', got %q", selectorResult.Command)
	}
	// @ok should resolve to all hosts since they were all in the norm group.
	if len(selectorResult.Results) != 2 {
		t.Fatalf("expected 2 results from @ok, got %d", len(selectorResult.Results))
	}
	for _, r := range selectorResult.Results {
		if r.Err != nil {
			t.Fatalf("@ok host %s failed: %v", r.Host, r.Err)
		}
	}

	t.Log("Extended integration tests passed: focus cycling, expand/collapse, diff, filter, help, selectors")
}
