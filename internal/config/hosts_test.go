package config

import (
	"strings"
	"testing"
	"time"

	"github.com/agent462/herd/internal/pathutil"
)

func TestResolveHostsFromGroup(t *testing.T) {
	cfg := &Config{
		Groups: map[string]Group{
			"pis": {
				Hosts: []string{"pi-garage", "pi-livingroom", "pi-workshop"},
			},
		},
		Defaults: DefaultConfig().Defaults,
	}

	hosts, err := ResolveHosts(cfg, "pis", nil)
	if err != nil {
		t.Fatalf("ResolveHosts error: %v", err)
	}
	if len(hosts) != 3 {
		t.Fatalf("expected 3 hosts, got %d", len(hosts))
	}
	if hosts[0].Name != "pi-garage" {
		t.Errorf("hosts[0].Name = %q, want \"pi-garage\"", hosts[0].Name)
	}
}

func TestResolveHostsFromCLI(t *testing.T) {
	cfg := DefaultConfig()

	hosts, err := ResolveHosts(cfg, "", []string{"web-01", "web-02"})
	if err != nil {
		t.Fatalf("ResolveHosts error: %v", err)
	}
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}
	if hosts[0].Name != "web-01" {
		t.Errorf("hosts[0].Name = %q, want \"web-01\"", hosts[0].Name)
	}
	if hosts[1].Name != "web-02" {
		t.Errorf("hosts[1].Name = %q, want \"web-02\"", hosts[1].Name)
	}
}

func TestResolveHostsMergesGroupAndCLI(t *testing.T) {
	cfg := &Config{
		Groups: map[string]Group{
			"web": {
				Hosts: []string{"web-01", "web-02"},
			},
		},
		Defaults: DefaultConfig().Defaults,
	}

	hosts, err := ResolveHosts(cfg, "web", []string{"web-02", "web-03"})
	if err != nil {
		t.Fatalf("ResolveHosts error: %v", err)
	}
	// web-02 is deduplicated, so we get web-01, web-02, web-03.
	if len(hosts) != 3 {
		t.Fatalf("expected 3 hosts, got %d", len(hosts))
	}
	names := make([]string, len(hosts))
	for i, h := range hosts {
		names[i] = h.Name
	}
	expected := []string{"web-01", "web-02", "web-03"}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("hosts[%d].Name = %q, want %q", i, names[i], want)
		}
	}
}

func TestResolveHostsGroupNotFound(t *testing.T) {
	cfg := &Config{
		Groups: map[string]Group{
			"pis": {Hosts: []string{"pi-1"}},
		},
		Defaults: DefaultConfig().Defaults,
	}

	_, err := ResolveHosts(cfg, "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for nonexistent group")
	}
}

func TestResolveHostsNoHostsSpecified(t *testing.T) {
	cfg := DefaultConfig()

	_, err := ResolveHosts(cfg, "", nil)
	if err == nil {
		t.Fatal("expected error when no hosts specified")
	}
}

func TestResolveHostsGroupUserApplied(t *testing.T) {
	cfg := &Config{
		Groups: map[string]Group{
			"web": {
				Hosts: []string{"web-01", "web-02"},
				User:  "deploy",
			},
		},
		Defaults: DefaultConfig().Defaults,
	}

	hosts, err := ResolveHosts(cfg, "web", nil)
	if err != nil {
		t.Fatalf("ResolveHosts error: %v", err)
	}
	for _, h := range hosts {
		if h.User != "deploy" {
			t.Errorf("host %q user = %q, want \"deploy\"", h.Name, h.User)
		}
	}
}

func TestResolveHostsDefaultPort(t *testing.T) {
	cfg := &Config{
		Groups: map[string]Group{
			"test": {Hosts: []string{"some-unknown-host-for-testing"}},
		},
		Defaults: DefaultConfig().Defaults,
	}

	hosts, err := ResolveHosts(cfg, "test", nil)
	if err != nil {
		t.Fatalf("ResolveHosts error: %v", err)
	}
	// Hosts not in SSH config should keep default port 22.
	if hosts[0].Port != 22 {
		t.Errorf("port = %d, want 22", hosts[0].Port)
	}
}

func TestResolveHostsGroupNotFoundNoGroups(t *testing.T) {
	cfg := DefaultConfig()

	_, err := ResolveHosts(cfg, "missing", nil)
	if err == nil {
		t.Fatal("expected error for missing group with no groups defined")
	}
}

func TestExpandHome(t *testing.T) {
	result := pathutil.ExpandHome("/absolute/path")
	if result != "/absolute/path" {
		t.Errorf("pathutil.ExpandHome should not modify absolute paths, got %q", result)
	}

	result = pathutil.ExpandHome("")
	if result != "" {
		t.Errorf("pathutil.ExpandHome should return empty for empty, got %q", result)
	}

	// ~otheruser paths should be returned unchanged.
	result = pathutil.ExpandHome("~otheruser/.ssh/id_rsa")
	if result != "~otheruser/.ssh/id_rsa" {
		t.Errorf("pathutil.ExpandHome should not expand ~user paths, got %q", result)
	}

	// ~/ should expand to home directory.
	result = pathutil.ExpandHome("~/.ssh/id_rsa")
	if strings.HasPrefix(result, "~") {
		t.Errorf("pathutil.ExpandHome should expand ~/.ssh/id_rsa, got %q", result)
	}
}

func TestHostDefaultValues(t *testing.T) {
	host := Host{Name: "test", Hostname: "test", Port: 22}
	MergeSSHConfig(&host)

	// After merge, port should remain 22 if no SSH config matches.
	if host.Port != 22 {
		t.Errorf("port = %d, want 22", host.Port)
	}
}

func TestResolveHostsUserAtHostSyntax(t *testing.T) {
	cfg := DefaultConfig()

	hosts, err := ResolveHosts(cfg, "", []string{"deploy@web-01", "root@web-02", "web-03"})
	if err != nil {
		t.Fatalf("ResolveHosts error: %v", err)
	}
	if len(hosts) != 3 {
		t.Fatalf("expected 3 hosts, got %d", len(hosts))
	}
	// Name preserves the original input; Hostname is the parsed host.
	if hosts[0].Name != "deploy@web-01" || hosts[0].Hostname != "web-01" || hosts[0].User != "deploy" {
		t.Errorf("hosts[0]: name=%q hostname=%q user=%q, want name=deploy@web-01 hostname=web-01 user=deploy",
			hosts[0].Name, hosts[0].Hostname, hosts[0].User)
	}
	if hosts[1].Name != "root@web-02" || hosts[1].Hostname != "web-02" || hosts[1].User != "root" {
		t.Errorf("hosts[1]: name=%q hostname=%q user=%q, want name=root@web-02 hostname=web-02 user=root",
			hosts[1].Name, hosts[1].Hostname, hosts[1].User)
	}
	if hosts[2].Name != "web-03" || hosts[2].Hostname != "web-03" {
		t.Errorf("hosts[2]: name=%q hostname=%q, want both web-03", hosts[2].Name, hosts[2].Hostname)
	}
}

func TestResolveHostsGroupUserOverridesAtSyntax(t *testing.T) {
	cfg := &Config{
		Groups: map[string]Group{
			"web": {
				Hosts: []string{"deploy@web-01"},
				User:  "admin",
			},
		},
		Defaults: DefaultConfig().Defaults,
	}

	hosts, err := ResolveHosts(cfg, "web", nil)
	if err != nil {
		t.Fatalf("ResolveHosts error: %v", err)
	}
	// Group-level user should override the user@host user.
	if hosts[0].User != "admin" {
		t.Errorf("user = %q, want \"admin\" (group override)", hosts[0].User)
	}
	if hosts[0].Hostname != "web-01" {
		t.Errorf("hostname = %q, want \"web-01\"", hosts[0].Hostname)
	}
}

func TestParseUserAtHost(t *testing.T) {
	tests := []struct {
		input    string
		user     string
		host     string
		ok       bool
	}{
		{"user@host", "user", "host", true},
		{"deploy@192.168.1.1", "deploy", "192.168.1.1", true},
		{"hostname", "", "", false},
		{"192.168.1.1", "", "", false},
		{"@host", "", "", false},
	}
	for _, tt := range tests {
		user, host, ok := parseUserAtHost(tt.input)
		if ok != tt.ok || user != tt.user || host != tt.host {
			t.Errorf("parseUserAtHost(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.input, user, host, ok, tt.user, tt.host, tt.ok)
		}
	}
}

func TestResolveHostsSameHostDifferentUsers(t *testing.T) {
	cfg := DefaultConfig()

	hosts, err := ResolveHosts(cfg, "", []string{"admin@server1", "deploy@server1"})
	if err != nil {
		t.Fatalf("ResolveHosts error: %v", err)
	}
	// Both entries should be preserved (different display names).
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}
	if hosts[0].Name != "admin@server1" || hosts[0].User != "admin" {
		t.Errorf("hosts[0]: name=%q user=%q, want admin@server1/admin", hosts[0].Name, hosts[0].User)
	}
	if hosts[1].Name != "deploy@server1" || hosts[1].User != "deploy" {
		t.Errorf("hosts[1]: name=%q user=%q, want deploy@server1/deploy", hosts[1].Name, hosts[1].User)
	}
	// Both should resolve to the same hostname.
	if hosts[0].Hostname != "server1" || hosts[1].Hostname != "server1" {
		t.Errorf("hostnames should both be server1, got %q and %q", hosts[0].Hostname, hosts[1].Hostname)
	}
}

func TestDurationFieldInGroup(t *testing.T) {
	cfg := &Config{
		Groups: map[string]Group{
			"web": {
				Hosts:   []string{"web-01"},
				User:    "deploy",
				Timeout: Duration{10 * time.Second},
			},
		},
		Defaults: DefaultConfig().Defaults,
	}

	if cfg.Groups["web"].Timeout.Duration != 10*time.Second {
		t.Errorf("group timeout = %s, want 10s", cfg.Groups["web"].Timeout)
	}
}
