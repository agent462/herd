package selector

import (
	"testing"

	"github.com/agent462/herd/internal/executor"
	"github.com/agent462/herd/internal/grouper"
)

func TestParseInput_NoSelector(t *testing.T) {
	sel, cmd := ParseInput("uptime")
	if sel != "" {
		t.Errorf("sel = %q, want empty", sel)
	}
	if cmd != "uptime" {
		t.Errorf("cmd = %q, want %q", cmd, "uptime")
	}
}

func TestParseInput_WithSelector(t *testing.T) {
	sel, cmd := ParseInput("@differs df -h /")
	if sel != "@differs" {
		t.Errorf("sel = %q, want %q", sel, "@differs")
	}
	if cmd != "df -h /" {
		t.Errorf("cmd = %q, want %q", cmd, "df -h /")
	}
}

func TestParseInput_CombinedSelector(t *testing.T) {
	sel, cmd := ParseInput("@differs,@failed systemctl restart nginx")
	if sel != "@differs,@failed" {
		t.Errorf("sel = %q, want %q", sel, "@differs,@failed")
	}
	if cmd != "systemctl restart nginx" {
		t.Errorf("cmd = %q, want %q", cmd, "systemctl restart nginx")
	}
}

func TestParseInput_CombinedSelectorSpaces(t *testing.T) {
	sel, cmd := ParseInput("@differs, @failed systemctl restart nginx")
	if sel != "@differs, @failed" {
		t.Errorf("sel = %q, want %q", sel, "@differs, @failed")
	}
	if cmd != "systemctl restart nginx" {
		t.Errorf("cmd = %q, want %q", cmd, "systemctl restart nginx")
	}
}

func TestParseInput_SelectorOnly(t *testing.T) {
	sel, cmd := ParseInput("@all")
	if sel != "@all" {
		t.Errorf("sel = %q, want %q", sel, "@all")
	}
	if cmd != "" {
		t.Errorf("cmd = %q, want empty", cmd)
	}
}

func TestParseInput_Whitespace(t *testing.T) {
	sel, cmd := ParseInput("  @ok  ls -la  ")
	if sel != "@ok" {
		t.Errorf("sel = %q, want %q", sel, "@ok")
	}
	if cmd != "ls -la" {
		t.Errorf("cmd = %q, want %q", cmd, "ls -la")
	}
}

func TestResolve_EmptySelector(t *testing.T) {
	state := &State{AllHosts: []string{"a", "b", "c"}}
	hosts, err := Resolve("", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHosts(t, hosts, []string{"a", "b", "c"})
}

func TestResolve_All(t *testing.T) {
	state := &State{AllHosts: []string{"a", "b", "c"}}
	hosts, err := Resolve("@all", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHosts(t, hosts, []string{"a", "b", "c"})
}

func TestResolve_OK(t *testing.T) {
	state := &State{
		AllHosts: []string{"a", "b", "c"},
		Grouped: &grouper.GroupedResults{
			Groups: []grouper.OutputGroup{
				{Hosts: []string{"a", "b"}, IsNorm: true},
				{Hosts: []string{"c"}, IsNorm: false},
			},
		},
	}
	hosts, err := Resolve("@ok", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHosts(t, hosts, []string{"a", "b"})
}

func TestResolve_Differs(t *testing.T) {
	state := &State{
		AllHosts: []string{"a", "b", "c"},
		Grouped: &grouper.GroupedResults{
			Groups: []grouper.OutputGroup{
				{Hosts: []string{"a", "b"}, IsNorm: true},
				{Hosts: []string{"c"}, IsNorm: false},
			},
		},
	}
	hosts, err := Resolve("@differs", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHosts(t, hosts, []string{"c"})
}

func TestResolve_Failed(t *testing.T) {
	state := &State{
		AllHosts: []string{"a", "b", "c", "d"},
		Grouped: &grouper.GroupedResults{
			Failed: []*executor.HostResult{{Host: "a"}},
			Groups: []grouper.OutputGroup{
				{Hosts: []string{"b"}, ExitCode: 1},
			},
			TimedOut: []*executor.HostResult{{Host: "c"}},
		},
	}
	hosts, err := Resolve("@failed", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHosts(t, hosts, []string{"a", "b", "c"})
}

func TestResolve_Timeout(t *testing.T) {
	state := &State{
		AllHosts: []string{"a", "b"},
		Grouped: &grouper.GroupedResults{
			TimedOut: []*executor.HostResult{{Host: "b"}},
		},
	}
	hosts, err := Resolve("@timeout", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHosts(t, hosts, []string{"b"})
}

func TestResolve_HostnameExact(t *testing.T) {
	state := &State{AllHosts: []string{"pi-garage", "pi-livingroom", "pi-workshop"}}
	hosts, err := Resolve("@pi-garage", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHosts(t, hosts, []string{"pi-garage"})
}

func TestResolve_GlobPattern(t *testing.T) {
	state := &State{AllHosts: []string{"pi-garage", "pi-livingroom", "web-01", "web-02"}}
	hosts, err := Resolve("@pi-*", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHosts(t, hosts, []string{"pi-garage", "pi-livingroom"})
}

func TestResolve_CombinedSelectors(t *testing.T) {
	state := &State{
		AllHosts: []string{"a", "b", "c", "d"},
		Grouped: &grouper.GroupedResults{
			Groups: []grouper.OutputGroup{
				{Hosts: []string{"a", "b"}, IsNorm: true},
				{Hosts: []string{"c"}, IsNorm: false},
			},
			Failed: []*executor.HostResult{{Host: "d"}},
		},
	}
	hosts, err := Resolve("@differs,@failed", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHosts(t, hosts, []string{"c", "d"})
}

func TestResolve_CombinedDedup(t *testing.T) {
	state := &State{
		AllHosts: []string{"a", "b", "c"},
		Grouped: &grouper.GroupedResults{
			Groups: []grouper.OutputGroup{
				{Hosts: []string{"a"}, IsNorm: true},
				{Hosts: []string{"b", "c"}, IsNorm: false},
			},
			Failed: []*executor.HostResult{{Host: "b"}},
		},
	}
	hosts, err := Resolve("@differs,@failed", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "b" appears in both @differs and @failed, should be deduplicated.
	assertHosts(t, hosts, []string{"b", "c"})
}

func TestResolve_NoPreviousResults(t *testing.T) {
	state := &State{AllHosts: []string{"a", "b"}}

	for _, sel := range []string{"@ok", "@differs", "@failed", "@timeout"} {
		_, err := Resolve(sel, state)
		if err == nil {
			t.Errorf("%s: expected error for no previous results", sel)
		}
	}
}

func TestResolve_NoMatch(t *testing.T) {
	state := &State{AllHosts: []string{"a", "b"}}
	_, err := Resolve("@nonexistent", state)
	if err == nil {
		t.Error("expected error for non-matching selector")
	}
}

func TestResolve_InvalidSelector(t *testing.T) {
	state := &State{AllHosts: []string{"a"}}
	_, err := Resolve("nope", state)
	if err == nil {
		t.Error("expected error for selector without @")
	}
}

func TestResolve_DiffersEmpty(t *testing.T) {
	// All hosts identical → no differs.
	state := &State{
		AllHosts: []string{"a", "b"},
		Grouped: &grouper.GroupedResults{
			Groups: []grouper.OutputGroup{
				{Hosts: []string{"a", "b"}, IsNorm: true},
			},
		},
	}
	hosts, err := Resolve("@differs", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 0 {
		t.Errorf("expected empty result, got %v", hosts)
	}
}

func TestResolve_GlobBrackets(t *testing.T) {
	state := &State{AllHosts: []string{"web-01", "web-02", "web-03", "db-01"}}
	hosts, err := Resolve("@web-0[12]", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertHosts(t, hosts, []string{"web-01", "web-02"})
}

func assertHosts(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d hosts %v, want %d hosts %v", len(got), got, len(want), want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("host[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
