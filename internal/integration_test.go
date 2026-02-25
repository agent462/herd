package internal_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/bryanhitc/herd/internal/executor"
	"github.com/bryanhitc/herd/internal/grouper"
	hssh "github.com/bryanhitc/herd/internal/ssh"
	"github.com/bryanhitc/herd/internal/sshtest"
	execui "github.com/bryanhitc/herd/internal/ui/exec"
)

// hostRunner is a test adapter that maps logical host names to 127.0.0.1 connections
// with different ports, so we can test with multiple in-process SSH servers.
type hostRunner struct {
	baseConf  hssh.ClientConfig
	hostPorts map[string]int
	keyPath   string
}

func (r *hostRunner) Run(ctx context.Context, host string, command string) *executor.HostResult {
	result := &executor.HostResult{Host: host}

	port, ok := r.hostPorts[host]
	if !ok {
		result.Err = fmt.Errorf("unknown host: %s", host)
		return result
	}

	conf := r.baseConf
	conf.Port = port
	conf.IdentityFiles = []string{r.keyPath}

	// Always dial 127.0.0.1 regardless of logical host name.
	client, err := hssh.Dial(ctx, "127.0.0.1", conf)
	if err != nil {
		result.Err = fmt.Errorf("connect: %w", err)
		return result
	}
	defer client.Close()

	stdout, stderr, exitCode, err := client.RunCommand(ctx, command)
	result.Stdout = stdout
	result.Stderr = stderr
	result.ExitCode = exitCode
	result.Err = err
	return result
}

// TestFullPipeline_GroupedOutput tests the complete flow:
// SSH servers -> runner -> executor -> grouper -> formatter.
func TestFullPipeline_GroupedOutput(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	pubKey, keyPath := sshtest.GenerateKey(t)

	// 3 servers: 2 identical, 1 different.
	addr1, cleanup1 := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\n", "", 0
	}))
	defer cleanup1()

	addr2, cleanup2 := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\n", "", 0
	}))
	defer cleanup2()

	addr3, cleanup3 := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "PRETTY_NAME=\"Debian GNU/Linux 11 (bullseye)\"\n", "", 0
	}))
	defer cleanup3()

	_, port1 := sshtest.ParseAddr(t, addr1)
	_, port2 := sshtest.ParseAddr(t, addr2)
	_, port3 := sshtest.ParseAddr(t, addr3)

	runner := &hostRunner{
		baseConf: hssh.ClientConfig{
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			User:            "testuser",
		},
		hostPorts: map[string]int{
			"pi-garage":     port1,
			"pi-livingroom": port2,
			"pi-workshop":   port3,
		},
		keyPath: keyPath,
	}

	exec := executor.New(runner, executor.WithConcurrency(5), executor.WithTimeout(10e9))

	ctx := context.Background()
	hosts := []string{"pi-garage", "pi-livingroom", "pi-workshop"}
	results := exec.Execute(ctx, hosts, "cat /etc/os-release | grep PRETTY")

	// Verify all succeeded.
	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("host %s error: %v", r.Host, r.Err)
		}
	}

	// Group results.
	grouped := grouper.Group(results)
	if len(grouped.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(grouped.Groups))
	}

	// Norm group should have 2 hosts (bookworm).
	norm := grouped.Groups[0]
	if !norm.IsNorm {
		t.Fatal("first group should be norm")
	}
	if len(norm.Hosts) != 2 {
		t.Errorf("norm group: expected 2 hosts, got %d", len(norm.Hosts))
	}
	if !strings.Contains(string(norm.Stdout), "bookworm") {
		t.Errorf("norm stdout should contain 'bookworm', got %q", string(norm.Stdout))
	}

	// Outlier group should have 1 host (bullseye).
	outlier := grouped.Groups[1]
	if outlier.IsNorm {
		t.Fatal("second group should not be norm")
	}
	if len(outlier.Hosts) != 1 {
		t.Errorf("outlier group: expected 1 host, got %d", len(outlier.Hosts))
	}
	if outlier.Hosts[0] != "pi-workshop" {
		t.Errorf("outlier host should be pi-workshop, got %s", outlier.Hosts[0])
	}
	if !strings.Contains(string(outlier.Stdout), "bullseye") {
		t.Errorf("outlier stdout should contain 'bullseye', got %q", string(outlier.Stdout))
	}
	if outlier.Diff == "" {
		t.Error("outlier should have a diff")
	}

	// Format output and verify.
	formatter := execui.NewFormatter(false, false, false)
	output := formatter.Format(grouped)

	if !strings.Contains(output, "2 hosts identical") {
		t.Errorf("output should contain '2 hosts identical', got:\n%s", output)
	}
	if !strings.Contains(output, "1 host differs") {
		t.Errorf("output should contain '1 host differs', got:\n%s", output)
	}
	if !strings.Contains(output, "3 succeeded") {
		t.Errorf("output should contain '3 succeeded', got:\n%s", output)
	}

	t.Logf("Formatted output:\n%s", output)
}

// TestFullPipeline_MixedResults tests with success, failure, and different output.
func TestFullPipeline_MixedResults(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	pubKey, keyPath := sshtest.GenerateKey(t)

	addr1, cleanup1 := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "active\n", "", 0
	}))
	defer cleanup1()

	addr2, cleanup2 := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "active\n", "", 0
	}))
	defer cleanup2()

	addr3, cleanup3 := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "inactive\n", "", 3
	}))
	defer cleanup3()

	_, port1 := sshtest.ParseAddr(t, addr1)
	_, port2 := sshtest.ParseAddr(t, addr2)
	_, port3 := sshtest.ParseAddr(t, addr3)

	runner := &hostRunner{
		baseConf: hssh.ClientConfig{
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			User:            "testuser",
		},
		hostPorts: map[string]int{
			"web-01": port1,
			"web-02": port2,
			"web-03": port3,
			"web-04": 1, // unreachable port
		},
		keyPath: keyPath,
	}

	exec := executor.New(runner, executor.WithConcurrency(10), executor.WithTimeout(5e9))

	ctx := context.Background()
	results := exec.Execute(ctx, []string{"web-01", "web-02", "web-03", "web-04"}, "systemctl is-active nginx")

	grouped := grouper.Group(results)

	if len(grouped.Failed) == 0 {
		t.Error("expected at least one failed host (web-04)")
	}

	// Should have groups for the successful hosts (exit code 0).
	if len(grouped.Groups) < 1 {
		t.Error("expected at least one group for successful hosts")
	}

	// web-03 returned exit code 3, should be in NonZero.
	if len(grouped.NonZero) != 1 {
		t.Errorf("expected 1 non-zero host, got %d", len(grouped.NonZero))
	} else if grouped.NonZero[0].Host != "web-03" {
		t.Errorf("expected non-zero host 'web-03', got %q", grouped.NonZero[0].Host)
	}

	formatter := execui.NewFormatter(false, false, false)
	output := formatter.Format(grouped)

	if !strings.Contains(output, "failed") {
		t.Errorf("output should mention failed hosts, got:\n%s", output)
	}
	if !strings.Contains(output, "non-zero exit") {
		t.Errorf("output should mention non-zero exit, got:\n%s", output)
	}

	t.Logf("Mixed results output:\n%s", output)
}

// TestFullPipeline_JSONOutput tests JSON output formatting.
func TestFullPipeline_JSONOutput(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	pubKey, keyPath := sshtest.GenerateKey(t)

	addr1, cleanup1 := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "192.168.1.10\n", "", 0
	}))
	defer cleanup1()

	_, port1 := sshtest.ParseAddr(t, addr1)

	runner := &hostRunner{
		baseConf: hssh.ClientConfig{
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			User:            "testuser",
		},
		hostPorts: map[string]int{
			"server-1": port1,
		},
		keyPath: keyPath,
	}

	exec := executor.New(runner, executor.WithConcurrency(5), executor.WithTimeout(10e9))
	results := exec.Execute(context.Background(), []string{"server-1"}, "hostname -I")

	formatter := execui.NewFormatter(true, false, false)
	data, err := formatter.FormatJSON(results)
	if err != nil {
		t.Fatalf("format JSON: %v", err)
	}

	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"host": "server-1"`) {
		t.Errorf("JSON should contain host, got:\n%s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"stdout": "192.168.1.10\n"`) {
		t.Errorf("JSON should contain stdout, got:\n%s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"exit_code": 0`) {
		t.Errorf("JSON should contain exit_code, got:\n%s", jsonStr)
	}

	t.Logf("JSON output:\n%s", jsonStr)
}

// TestFullPipeline_ErrorsOnly tests the errors-only output mode.
func TestFullPipeline_ErrorsOnly(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	pubKey, keyPath := sshtest.GenerateKey(t)

	addr1, cleanup1 := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "ok\n", "", 0
	}))
	defer cleanup1()

	_, port1 := sshtest.ParseAddr(t, addr1)

	runner := &hostRunner{
		baseConf: hssh.ClientConfig{
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			User:            "testuser",
		},
		hostPorts: map[string]int{
			"good-host": port1,
			"bad-host":  1, // unreachable
		},
		keyPath: keyPath,
	}

	exec := executor.New(runner, executor.WithConcurrency(5), executor.WithTimeout(5e9))
	results := exec.Execute(context.Background(), []string{"good-host", "bad-host"}, "test")
	grouped := grouper.Group(results)

	formatter := execui.NewFormatter(false, true, false)
	output := formatter.Format(grouped)

	// Errors-only: should not show successful output.
	if strings.Contains(output, "identical") {
		t.Errorf("errors-only output should not show identical groups, got:\n%s", output)
	}
	if !strings.Contains(output, "failed") {
		t.Errorf("errors-only output should show failed hosts, got:\n%s", output)
	}

	t.Logf("Errors-only output:\n%s", output)
}

// TestFullPipeline_UserAtHostCollision verifies that specifying the same host
// with different users (e.g. "admin@server" and "deploy@server") produces
// separate executions with the correct per-entry credentials.
func TestFullPipeline_UserAtHostCollision(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	pubKey, keyPath := sshtest.GenerateKey(t)

	addr1, cleanup1 := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "output-a\n", "", 0
	}))
	defer cleanup1()

	addr2, cleanup2 := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "output-b\n", "", 0
	}))
	defer cleanup2()

	_, port1 := sshtest.ParseAddr(t, addr1)
	_, port2 := sshtest.ParseAddr(t, addr2)

	runner := &hostRunner{
		baseConf: hssh.ClientConfig{
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		},
		hostPorts: map[string]int{
			"admin@server":  port1,
			"deploy@server": port2,
		},
		keyPath: keyPath,
	}

	exec := executor.New(runner, executor.WithConcurrency(5), executor.WithTimeout(10e9))
	results := exec.Execute(context.Background(), []string{"admin@server", "deploy@server"}, "whoami")

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Each entry should have its own label in the result.
	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("host %s error: %v", r.Host, r.Err)
		}
	}
	if results[0].Host != "admin@server" {
		t.Errorf("results[0].Host = %q, want admin@server", results[0].Host)
	}
	if results[1].Host != "deploy@server" {
		t.Errorf("results[1].Host = %q, want deploy@server", results[1].Host)
	}

	// Outputs should be distinct (not overwritten by collision).
	if string(results[0].Stdout) != "output-a\n" {
		t.Errorf("results[0].Stdout = %q, want output-a", results[0].Stdout)
	}
	if string(results[1].Stdout) != "output-b\n" {
		t.Errorf("results[1].Stdout = %q, want output-b", results[1].Stdout)
	}

	// Grouping should treat them as separate hosts.
	grouped := grouper.Group(results)
	if len(grouped.Groups) != 2 {
		t.Errorf("expected 2 groups (different output), got %d", len(grouped.Groups))
	}
}

// TestFullPipeline_AllIdentical tests when all hosts return the same output.
func TestFullPipeline_AllIdentical(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	pubKey, keyPath := sshtest.GenerateKey(t)

	handler := func(cmd string) (string, string, int) {
		return " 12:34:56 up 14 days, 3:22, 0 users, load average: 0.02, 0.05, 0.01\n", "", 0
	}

	addr1, cleanup1 := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(handler))
	defer cleanup1()
	addr2, cleanup2 := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(handler))
	defer cleanup2()
	addr3, cleanup3 := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(handler))
	defer cleanup3()

	_, port1 := sshtest.ParseAddr(t, addr1)
	_, port2 := sshtest.ParseAddr(t, addr2)
	_, port3 := sshtest.ParseAddr(t, addr3)

	runner := &hostRunner{
		baseConf: hssh.ClientConfig{
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			User:            "testuser",
		},
		hostPorts: map[string]int{
			"pi-1": port1,
			"pi-2": port2,
			"pi-3": port3,
		},
		keyPath: keyPath,
	}

	exec := executor.New(runner, executor.WithConcurrency(5), executor.WithTimeout(10e9))
	results := exec.Execute(context.Background(), []string{"pi-1", "pi-2", "pi-3"}, "uptime")

	grouped := grouper.Group(results)
	if len(grouped.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(grouped.Groups))
	}
	if !grouped.Groups[0].IsNorm {
		t.Error("single group should be norm")
	}
	if len(grouped.Groups[0].Hosts) != 3 {
		t.Errorf("expected 3 hosts in group, got %d", len(grouped.Groups[0].Hosts))
	}

	formatter := execui.NewFormatter(false, false, false)
	output := formatter.Format(grouped)
	if !strings.Contains(output, "3 hosts identical") {
		t.Errorf("output should contain '3 hosts identical', got:\n%s", output)
	}
	if !strings.Contains(output, "3 succeeded") {
		t.Errorf("output should contain '3 succeeded', got:\n%s", output)
	}

	t.Logf("All identical output:\n%s", output)
}

// TestFullPipeline_ProxyJump tests SSH execution through a bastion/jump host.
func TestFullPipeline_ProxyJump(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	pubKey, keyPath := sshtest.GenerateKey(t)

	// Start a bastion server with TCP forwarding enabled.
	bastionAddr, bastionCleanup := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithForwardTCP())
	defer bastionCleanup()

	// Start the target server.
	targetAddr, targetCleanup := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "behind-bastion\n", "", 0
	}))
	defer targetCleanup()

	_, bastionPort := sshtest.ParseAddr(t, bastionAddr)
	_, targetPort := sshtest.ParseAddr(t, targetAddr)

	jumpSpec := fmt.Sprintf("testuser@127.0.0.1:%d", bastionPort)

	runner := hssh.NewRunner(
		hssh.ClientConfig{
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			User:            "testuser",
		},
		map[string]hssh.HostConfig{
			"target-host": {
				Hostname:     "127.0.0.1",
				Port:         targetPort,
				IdentityFile: keyPath,
				ProxyJump:    jumpSpec,
			},
		},
	)

	exec := executor.New(runner, executor.WithConcurrency(5), executor.WithTimeout(10e9))
	results := exec.Execute(context.Background(), []string{"target-host"}, "test")

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("unexpected error: %v", results[0].Err)
	}
	if string(results[0].Stdout) != "behind-bastion\n" {
		t.Errorf("stdout = %q, want behind-bastion", results[0].Stdout)
	}

	grouped := grouper.Group(results)
	formatter := execui.NewFormatter(false, false, false)
	output := formatter.Format(grouped)
	if !strings.Contains(output, "1 succeeded") {
		t.Errorf("output should contain '1 succeeded', got:\n%s", output)
	}

	t.Logf("ProxyJump output:\n%s", output)
}
