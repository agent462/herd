package parser

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/agent462/herd/internal/config"
	"github.com/agent462/herd/internal/executor"
)

func TestNewValidRegexRule(t *testing.T) {
	rules := []config.ExtractRule{
		{Field: "version", Pattern: `Version:\s+(\S+)`},
	}
	p, err := New(rules)
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	if len(p.rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(p.rules))
	}
	if p.rules[0].field != "version" {
		t.Errorf("expected field 'version', got %q", p.rules[0].field)
	}
	if p.rules[0].re == nil {
		t.Error("expected compiled regex, got nil")
	}
	if p.rules[0].column != 0 {
		t.Errorf("expected column 0, got %d", p.rules[0].column)
	}
}

func TestNewValidColumnRule(t *testing.T) {
	rules := []config.ExtractRule{
		{Field: "size", Column: 2},
	}
	p, err := New(rules)
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	if len(p.rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(p.rules))
	}
	if p.rules[0].re != nil {
		t.Error("expected nil regex for column rule")
	}
	if p.rules[0].column != 2 {
		t.Errorf("expected column 2, got %d", p.rules[0].column)
	}
}

func TestNewMixedRules(t *testing.T) {
	rules := []config.ExtractRule{
		{Field: "name", Pattern: `Name:\s+(.+)`},
		{Field: "size", Column: 3},
	}
	p, err := New(rules)
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	if len(p.rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(p.rules))
	}
}

func TestNewInvalidRegex(t *testing.T) {
	rules := []config.ExtractRule{
		{Field: "bad", Pattern: `([invalid`},
	}
	_, err := New(rules)
	if err == nil {
		t.Fatal("expected error for invalid regex, got nil")
	}
	if !strings.Contains(err.Error(), "invalid regex") {
		t.Errorf("error should mention invalid regex, got: %v", err)
	}
}

func TestNewNoPatternOrColumn(t *testing.T) {
	rules := []config.ExtractRule{
		{Field: "empty"},
	}
	_, err := New(rules)
	if err == nil {
		t.Fatal("expected error for rule with no pattern or column, got nil")
	}
	if !strings.Contains(err.Error(), "must have pattern or column") {
		t.Errorf("error should mention pattern/column requirement, got: %v", err)
	}
}

func TestParseRegex(t *testing.T) {
	dfOutput := `Filesystem      Size  Used Avail Use% Mounted on
/dev/sda1        50G   20G   28G  42% /
`
	rules := []config.ExtractRule{
		{Field: "size", Pattern: `(?m)^\S+\s+(\S+)\s+\S+\s+\S+\s+\S+\s+/\s*$`},
		{Field: "used", Pattern: `(?m)^\S+\s+\S+\s+(\S+)\s+\S+\s+\S+\s+/\s*$`},
	}
	p, err := New(rules)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	hp := p.Parse("server1", []byte(dfOutput))

	if hp.Host != "server1" {
		t.Errorf("expected host 'server1', got %q", hp.Host)
	}
	if hp.Err != nil {
		t.Errorf("unexpected error: %v", hp.Err)
	}
	if len(hp.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(hp.Fields))
	}
	if hp.Fields[0].Value != "50G" {
		t.Errorf("expected size '50G', got %q", hp.Fields[0].Value)
	}
	if hp.Fields[1].Value != "20G" {
		t.Errorf("expected used '20G', got %q", hp.Fields[1].Value)
	}
}

func TestParseColumn(t *testing.T) {
	dfOutput := `Filesystem      Size  Used Avail Use% Mounted on
/dev/sda1        50G   20G   28G  42% /
`
	rules := []config.ExtractRule{
		{Field: "filesystem", Column: 1},
		{Field: "size", Column: 2},
		{Field: "use_pct", Column: 5},
	}
	p, err := New(rules)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	hp := p.Parse("server1", []byte(dfOutput))

	if len(hp.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(hp.Fields))
	}
	if hp.Fields[0].Value != "/dev/sda1" {
		t.Errorf("expected filesystem '/dev/sda1', got %q", hp.Fields[0].Value)
	}
	if hp.Fields[1].Value != "50G" {
		t.Errorf("expected size '50G', got %q", hp.Fields[1].Value)
	}
	if hp.Fields[2].Value != "42%" {
		t.Errorf("expected use_pct '42%%', got %q", hp.Fields[2].Value)
	}
}

func TestParseNoMatch(t *testing.T) {
	rules := []config.ExtractRule{
		{Field: "missing", Pattern: `NoSuchPattern:\s+(\S+)`},
	}
	p, err := New(rules)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	hp := p.Parse("host1", []byte("some unrelated output\n"))

	if hp.Fields[0].Value != "-" {
		t.Errorf("expected '-' for no match, got %q", hp.Fields[0].Value)
	}
}

func TestParseColumnOutOfRange(t *testing.T) {
	output := `Header1 Header2
val1 val2
`
	rules := []config.ExtractRule{
		{Field: "col99", Column: 99},
	}
	p, err := New(rules)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	hp := p.Parse("host1", []byte(output))

	if hp.Fields[0].Value != "-" {
		t.Errorf("expected '-' for out-of-range column, got %q", hp.Fields[0].Value)
	}
}

func TestParseAll(t *testing.T) {
	rules := []config.ExtractRule{
		{Field: "val", Pattern: `result:\s+(\S+)`},
	}
	p, err := New(rules)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	results := []*executor.HostResult{
		{Host: "host-a", Stdout: []byte("result: 42\n"), Duration: time.Second},
		{Host: "host-b", Stdout: []byte("result: 99\n"), Duration: time.Second},
		{Host: "host-c", Stdout: nil, Err: errors.New("connection refused"), Duration: time.Second},
	}

	parsed := p.ParseAll(results)

	if len(parsed) != 3 {
		t.Fatalf("expected 3 parsed results, got %d", len(parsed))
	}
	if parsed[0].Fields[0].Value != "42" {
		t.Errorf("host-a: expected '42', got %q", parsed[0].Fields[0].Value)
	}
	if parsed[1].Fields[0].Value != "99" {
		t.Errorf("host-b: expected '99', got %q", parsed[1].Fields[0].Value)
	}
	if parsed[2].Err == nil {
		t.Error("host-c: expected error, got nil")
	}
	if parsed[2].Fields[0].Value != "-" {
		t.Errorf("host-c: expected '-' for errored host, got %q", parsed[2].Fields[0].Value)
	}
}

func TestFormatTable(t *testing.T) {
	parsed := []*HostParsed{
		{
			Host: "server1",
			Fields: []FieldValue{
				{Field: "size", Value: "50G"},
				{Field: "used", Value: "20G"},
			},
		},
		{
			Host: "server2",
			Fields: []FieldValue{
				{Field: "size", Value: "100G"},
				{Field: "used", Value: "80G"},
			},
		},
	}

	// Test without color.
	output := FormatTable(parsed, false)

	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines (header + separator + 2 data), got %d:\n%s", len(lines), output)
	}

	// Header should contain column names.
	if !strings.Contains(lines[0], "HOST") {
		t.Errorf("header missing HOST: %q", lines[0])
	}
	if !strings.Contains(lines[0], "SIZE") {
		t.Errorf("header missing SIZE: %q", lines[0])
	}
	if !strings.Contains(lines[0], "USED") {
		t.Errorf("header missing USED: %q", lines[0])
	}

	// Separator should be dashes.
	if !strings.Contains(lines[1], "---") {
		t.Errorf("separator line should contain dashes: %q", lines[1])
	}

	// Data rows should contain host names and values.
	if !strings.Contains(lines[2], "server1") {
		t.Errorf("data row missing server1: %q", lines[2])
	}
	if !strings.Contains(lines[2], "50G") {
		t.Errorf("data row missing 50G: %q", lines[2])
	}
	if !strings.Contains(lines[3], "server2") {
		t.Errorf("data row missing server2: %q", lines[3])
	}
	if !strings.Contains(lines[3], "100G") {
		t.Errorf("data row missing 100G: %q", lines[3])
	}
}

func TestFormatTableWithColor(t *testing.T) {
	parsed := []*HostParsed{
		{
			Host:   "host1",
			Fields: []FieldValue{{Field: "val", Value: "abc"}},
		},
	}

	output := FormatTable(parsed, true)

	if !strings.Contains(output, "\033[1;36m") {
		t.Error("color output should contain bold cyan ANSI code")
	}
	if !strings.Contains(output, "\033[0m") {
		t.Error("color output should contain ANSI reset code")
	}
}

func TestFormatTableEmpty(t *testing.T) {
	output := FormatTable(nil, false)
	if output != "" {
		t.Errorf("expected empty string for nil parsed, got %q", output)
	}
}

func TestFormatTableAlignment(t *testing.T) {
	parsed := []*HostParsed{
		{
			Host:   "a",
			Fields: []FieldValue{{Field: "name", Value: "short"}},
		},
		{
			Host:   "longerhostname",
			Fields: []FieldValue{{Field: "name", Value: "x"}},
		},
	}

	output := FormatTable(parsed, false)
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")

	// All data rows should have the same length (due to padding).
	if len(lines) >= 3 {
		if len(lines[2]) != len(lines[3]) {
			t.Errorf("rows should be aligned: %q vs %q", lines[2], lines[3])
		}
	}
}

// --- Built-in parser tests ---

func TestBuiltinDisk(t *testing.T) {
	dfOutput := `Filesystem      Size  Used Avail Use% Mounted on
/dev/sda1        50G   20G   28G  42% /
`
	p := BuiltinDisk()
	hp := p.Parse("server1", []byte(dfOutput))

	expected := map[string]string{
		"filesystem": "/dev/sda1",
		"size":       "50G",
		"used":       "20G",
		"avail":      "28G",
		"use_pct":    "42%",
		"mount":      "/",
	}

	if len(hp.Fields) != len(expected) {
		t.Fatalf("expected %d fields, got %d", len(expected), len(hp.Fields))
	}

	for _, fv := range hp.Fields {
		want, ok := expected[fv.Field]
		if !ok {
			t.Errorf("unexpected field %q", fv.Field)
			continue
		}
		if fv.Value != want {
			t.Errorf("field %q: got %q, want %q", fv.Field, fv.Value, want)
		}
	}
}

func TestBuiltinFree(t *testing.T) {
	freeOutput := `              total        used        free      shared  buff/cache   available
Mem:           15Gi       4.2Gi       8.1Gi       0.5Gi       3.2Gi        10Gi
Swap:         2.0Gi          0B       2.0Gi
`
	p := BuiltinFree()
	hp := p.Parse("server1", []byte(freeOutput))

	expected := map[string]string{
		"total":     "15Gi",
		"used":      "4.2Gi",
		"free":      "8.1Gi",
		"available": "10Gi",
	}

	if len(hp.Fields) != len(expected) {
		t.Fatalf("expected %d fields, got %d", len(expected), len(hp.Fields))
	}

	for _, fv := range hp.Fields {
		want, ok := expected[fv.Field]
		if !ok {
			t.Errorf("unexpected field %q", fv.Field)
			continue
		}
		if fv.Value != want {
			t.Errorf("field %q: got %q, want %q", fv.Field, fv.Value, want)
		}
	}
}

func TestBuiltinUptime(t *testing.T) {
	uptimeOutput := ` 14:23:15 up 42 days,  3:15,  2 users,  load average: 0.15, 0.10, 0.05
`
	p := BuiltinUptime()
	hp := p.Parse("server1", []byte(uptimeOutput))

	expected := map[string]string{
		"uptime": "42 days,  3:15",
		"users":  "2",
		"load1":  "0.15",
		"load5":  "0.10",
		"load15": "0.05",
	}

	if len(hp.Fields) != len(expected) {
		t.Fatalf("expected %d fields, got %d", len(expected), len(hp.Fields))
	}

	for _, fv := range hp.Fields {
		want, ok := expected[fv.Field]
		if !ok {
			t.Errorf("unexpected field %q", fv.Field)
			continue
		}
		if fv.Value != want {
			t.Errorf("field %q: got %q, want %q", fv.Field, fv.Value, want)
		}
	}
}

func TestBuiltinParsersMap(t *testing.T) {
	parsers := BuiltinParsers()

	expectedNames := []string{"disk", "free", "uptime"}
	for _, name := range expectedNames {
		if _, ok := parsers[name]; !ok {
			t.Errorf("BuiltinParsers() missing %q", name)
		}
	}
	if len(parsers) != len(expectedNames) {
		t.Errorf("expected %d built-in parsers, got %d", len(expectedNames), len(parsers))
	}
}
