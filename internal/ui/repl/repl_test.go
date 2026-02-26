package repl

import (
	"strings"
	"testing"
)

func TestFormatHistoryEntry(t *testing.T) {
	tests := []struct {
		name   string
		index  int
		entry  HistoryEntry
		want   []string // substrings that must appear
		reject []string // substrings that must not appear
	}{
		{
			name:  "basic ok entry",
			index: 1,
			entry: HistoryEntry{
				Input:     "uptime",
				HostCount: 4,
				OKCount:   3,
				DiffCount: 1,
			},
			want: []string{"1", "uptime", "4 hosts", "3 ok", "1 differs"},
		},
		{
			name:  "with failures",
			index: 2,
			entry: HistoryEntry{
				Input:     "@differs df -h /",
				HostCount: 2,
				OKCount:   1,
				FailCount: 1,
			},
			want: []string{"2", "@differs df -h /", "2 hosts", "1 ok", "1 failed"},
		},
		{
			name:  "single host",
			index: 3,
			entry: HistoryEntry{
				Input:     "@pi-backyard sudo apt autoremove -y",
				HostCount: 1,
				OKCount:   1,
			},
			want:   []string{"3", "1 host,", "1 ok"},
			reject: []string{"hosts,"},
		},
		{
			name:  "long input truncated",
			index: 4,
			entry: HistoryEntry{
				Input:     "some very long command that exceeds the display limit for the history",
				HostCount: 5,
				OKCount:   5,
			},
			want: []string{"4", "...", "5 hosts"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatHistoryEntry(tt.index, tt.entry)
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("output missing %q:\n%s", w, got)
				}
			}
			for _, r := range tt.reject {
				if strings.Contains(got, r) {
					t.Errorf("output should not contain %q:\n%s", r, got)
				}
			}
		})
	}
}

func TestParseColonCommand(t *testing.T) {
	tests := []struct {
		input    string
		wantCmd  string
		wantArgs []string
	}{
		{":quit", ":quit", nil},
		{":q", ":q", nil},
		{":group pis", ":group", []string{"pis"}},
		{":timeout 60s", ":timeout", []string{"60s"}},
		{":export /tmp/out.json", ":export", []string{"/tmp/out.json"}},
		{":history", ":history", nil},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			cmd, args := ParseColonCommand(tt.input)
			if cmd != tt.wantCmd {
				t.Errorf("cmd = %q, want %q", cmd, tt.wantCmd)
			}
			if len(args) != len(tt.wantArgs) {
				t.Fatalf("args = %v, want %v", args, tt.wantArgs)
			}
			for i := range args {
				if args[i] != tt.wantArgs[i] {
					t.Errorf("args[%d] = %q, want %q", i, args[i], tt.wantArgs[i])
				}
			}
		})
	}
}

func TestParseHistoryRef(t *testing.T) {
	tests := []struct {
		input   string
		wantN   int
		wantOK  bool
	}{
		{"!1", 1, true},
		{"!42", 42, true},
		{"!0", 0, false},
		{"!-1", 0, false},
		{"!", 0, false},
		{"hello", 0, false},
		{"!abc", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			n, ok := ParseHistoryRef(tt.input)
			if n != tt.wantN || ok != tt.wantOK {
				t.Errorf("ParseHistoryRef(%q) = (%d, %v), want (%d, %v)",
					tt.input, n, ok, tt.wantN, tt.wantOK)
			}
		})
	}
}

func TestValidCommands(t *testing.T) {
	cmds := ValidCommands()
	if len(cmds) == 0 {
		t.Fatal("expected non-empty command list")
	}

	required := map[string]bool{
		":quit": false, ":q": false, ":history": false, ":h": false,
		":hosts": false, ":group": false, ":timeout": false,
		":diff": false, ":last": false, ":export": false,
	}
	for _, c := range cmds {
		if _, ok := required[c]; ok {
			required[c] = true
		}
	}
	for c, found := range required {
		if !found {
			t.Errorf("missing command %q in ValidCommands()", c)
		}
	}
}

func TestPlural(t *testing.T) {
	if got := plural("host", 1); got != "host" {
		t.Errorf("plural(host, 1) = %q, want %q", got, "host")
	}
	if got := plural("host", 0); got != "hosts" {
		t.Errorf("plural(host, 0) = %q, want %q", got, "hosts")
	}
	if got := plural("host", 5); got != "hosts" {
		t.Errorf("plural(host, 5) = %q, want %q", got, "hosts")
	}
}
