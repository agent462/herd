package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Defaults.Concurrency != 20 {
		t.Errorf("default concurrency = %d, want 20", cfg.Defaults.Concurrency)
	}
	if cfg.Defaults.Timeout.Duration != 30*time.Second {
		t.Errorf("default timeout = %s, want 30s", cfg.Defaults.Timeout)
	}
	if cfg.Defaults.Output != "grouped" {
		t.Errorf("default output = %q, want \"grouped\"", cfg.Defaults.Output)
	}
	if cfg.Groups == nil {
		t.Error("default groups map should not be nil")
	}
}

func TestLoadValidConfig(t *testing.T) {
	content := `
groups:
  pis:
    hosts:
      - pi-garage
      - pi-livingroom
      - pi-workshop
      - pi-backyard
  web:
    hosts:
      - web-01
      - web-02
      - web-03
    user: deploy
    timeout: 10s

defaults:
  concurrency: 10
  timeout: 1m
  output: json
`
	cfg := loadFromString(t, content)

	// Check groups.
	if len(cfg.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(cfg.Groups))
	}

	pis := cfg.Groups["pis"]
	if len(pis.Hosts) != 4 {
		t.Errorf("pis group: expected 4 hosts, got %d", len(pis.Hosts))
	}
	if pis.Hosts[0].Host != "pi-garage" {
		t.Errorf("pis.Hosts[0].Host = %q, want \"pi-garage\"", pis.Hosts[0].Host)
	}

	web := cfg.Groups["web"]
	if len(web.Hosts) != 3 {
		t.Errorf("web group: expected 3 hosts, got %d", len(web.Hosts))
	}
	if web.User != "deploy" {
		t.Errorf("web.User = %q, want \"deploy\"", web.User)
	}
	if web.Timeout.Duration != 10*time.Second {
		t.Errorf("web.Timeout = %s, want 10s", web.Timeout)
	}

	// Check defaults.
	if cfg.Defaults.Concurrency != 10 {
		t.Errorf("concurrency = %d, want 10", cfg.Defaults.Concurrency)
	}
	if cfg.Defaults.Timeout.Duration != time.Minute {
		t.Errorf("timeout = %s, want 1m", cfg.Defaults.Timeout)
	}
	if cfg.Defaults.Output != "json" {
		t.Errorf("output = %q, want \"json\"", cfg.Defaults.Output)
	}
}

func TestDefaultValuesWhenOmitted(t *testing.T) {
	content := `
groups:
  test:
    hosts:
      - host1
`
	cfg := loadFromString(t, content)

	// Defaults should be filled in from DefaultConfig.
	if cfg.Defaults.Concurrency != 20 {
		t.Errorf("concurrency = %d, want 20", cfg.Defaults.Concurrency)
	}
	if cfg.Defaults.Timeout.Duration != 30*time.Second {
		t.Errorf("timeout = %s, want 30s", cfg.Defaults.Timeout)
	}
	if cfg.Defaults.Output != "grouped" {
		t.Errorf("output = %q, want \"grouped\"", cfg.Defaults.Output)
	}
}

func TestDurationParsing(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"10s", 10 * time.Second},
		{"1m", time.Minute},
		{"2m30s", 2*time.Minute + 30*time.Second},
		{"500ms", 500 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			content := `
groups:
  test:
    hosts:
      - host1
    timeout: ` + tt.input + `
`
			cfg := loadFromString(t, content)
			got := cfg.Groups["test"].Timeout.Duration
			if got != tt.want {
				t.Errorf("parsed duration = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestInvalidDuration(t *testing.T) {
	content := `
groups:
  test:
    hosts:
      - host1
    timeout: notaduration
`
	_, err := loadStringRaw(content)
	if err == nil {
		t.Fatal("expected error for invalid duration, got nil")
	}
}

func TestValidateInvalidOutputMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Defaults.Output = "invalid"
	cfg.Groups["test"] = Group{Hosts: strHosts("host1")}

	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for invalid output mode")
	}
}

func TestValidateStreamOutputRejected(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Defaults.Output = "stream"
	cfg.Groups["test"] = Group{Hosts: strHosts("host1")}

	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for 'stream' output mode")
	}
}

func TestValidateEmptyGroup(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Groups["empty"] = Group{Hosts: []HostEntry{}}

	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for empty group")
	}
}

func TestValidateNegativeConcurrency(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Defaults.Concurrency = -1

	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for negative concurrency")
	}
}

func TestLoadNonexistentFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error loading nonexistent file")
	}
}

func TestLoadDefaultNoFile(t *testing.T) {
	// LoadDefault should return defaults when no config file exists.
	// This works because the default path likely doesn't exist in test environments.
	cfg, err := LoadDefault()
	if err != nil {
		t.Fatalf("LoadDefault() error: %v", err)
	}
	if cfg.Defaults.Concurrency != 20 {
		t.Errorf("concurrency = %d, want 20", cfg.Defaults.Concurrency)
	}
}

func TestRecipeConfig(t *testing.T) {
	content := `
groups:
  test:
    hosts:
      - host1

recipes:
  deploy:
    description: "Deploy the app"
    steps:
      - "git pull"
      - "systemctl restart app"
  health-check:
    steps:
      - "curl -s localhost:8080/health"
`
	cfg := loadFromString(t, content)

	if len(cfg.Recipes) != 2 {
		t.Fatalf("expected 2 recipes, got %d", len(cfg.Recipes))
	}

	deploy := cfg.Recipes["deploy"]
	if deploy.Description != "Deploy the app" {
		t.Errorf("deploy.Description = %q, want %q", deploy.Description, "Deploy the app")
	}
	if len(deploy.Steps) != 2 {
		t.Errorf("deploy steps = %d, want 2", len(deploy.Steps))
	}
	if deploy.Steps[0] != "git pull" {
		t.Errorf("deploy.Steps[0] = %q, want %q", deploy.Steps[0], "git pull")
	}
}

func TestRecipeValidation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Groups["test"] = Group{Hosts: strHosts("host1")}

	// Empty steps should fail.
	cfg.Recipes = map[string]Recipe{
		"empty": {Steps: []string{}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for recipe with no steps")
	}

	// Invalid name should fail.
	cfg.Recipes = map[string]Recipe{
		"bad name!": {Steps: []string{"echo hi"}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for invalid recipe name")
	}
}

func TestParserConfig(t *testing.T) {
	content := `
groups:
  test:
    hosts:
      - host1

parsers:
  disk-usage:
    description: "Parse df output"
    extract:
      - field: filesystem
        pattern: '^(\S+)'
      - field: use_pct
        column: 5
`
	cfg := loadFromString(t, content)

	if len(cfg.Parsers) != 1 {
		t.Fatalf("expected 1 parser, got %d", len(cfg.Parsers))
	}

	p := cfg.Parsers["disk-usage"]
	if len(p.Extract) != 2 {
		t.Fatalf("expected 2 extract rules, got %d", len(p.Extract))
	}
	if p.Extract[0].Field != "filesystem" {
		t.Errorf("rule[0].Field = %q, want %q", p.Extract[0].Field, "filesystem")
	}
	if p.Extract[1].Column != 5 {
		t.Errorf("rule[1].Column = %d, want 5", p.Extract[1].Column)
	}
}

func TestParserValidation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Groups["test"] = Group{Hosts: strHosts("host1")}

	// No extract rules should fail.
	cfg.Parsers = map[string]Parser{
		"empty": {Extract: []ExtractRule{}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for parser with no rules")
	}

	// Rule missing both pattern and column.
	cfg.Parsers = map[string]Parser{
		"bad": {Extract: []ExtractRule{{Field: "x"}}},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for rule without pattern or column")
	}
}

func TestSaveConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config.yaml")

	cfg := DefaultConfig()
	cfg.Groups["web"] = Group{Hosts: strHosts("web-01", "web-02")}

	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(loaded.Groups["web"].Hosts) != 2 {
		t.Errorf("loaded group has %d hosts, want 2", len(loaded.Groups["web"].Hosts))
	}
}

// loadFromString is a test helper that writes content to a temp file, loads it,
// and fails the test if loading fails.
func loadFromString(t *testing.T, content string) *Config {
	t.Helper()
	cfg, err := loadStringRaw(content)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	return cfg
}

// strHosts converts bare host name strings to []HostEntry for test convenience.
func strHosts(names ...string) []HostEntry {
	entries := make([]HostEntry, len(names))
	for i, n := range names {
		entries[i] = HostEntry{Host: n}
	}
	return entries
}

func TestHostEntryBareString(t *testing.T) {
	content := `
groups:
  test:
    hosts:
      - host1
      - host2
`
	cfg := loadFromString(t, content)
	hosts := cfg.Groups["test"].Hosts
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}
	if hosts[0].Host != "host1" || len(hosts[0].Tags) != 0 {
		t.Errorf("hosts[0] = %+v, want {Host:host1 Tags:[]}", hosts[0])
	}
}

func TestHostEntryWithTags(t *testing.T) {
	content := `
groups:
  test:
    hosts:
      - host: server1
        tags: [prod, debian12]
      - host: server2
        tags: [staging]
`
	cfg := loadFromString(t, content)
	hosts := cfg.Groups["test"].Hosts
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}
	if hosts[0].Host != "server1" {
		t.Errorf("hosts[0].Host = %q, want %q", hosts[0].Host, "server1")
	}
	if len(hosts[0].Tags) != 2 || hosts[0].Tags[0] != "prod" || hosts[0].Tags[1] != "debian12" {
		t.Errorf("hosts[0].Tags = %v, want [prod debian12]", hosts[0].Tags)
	}
	if hosts[1].Host != "server2" || len(hosts[1].Tags) != 1 || hosts[1].Tags[0] != "staging" {
		t.Errorf("hosts[1] = %+v, want {Host:server2 Tags:[staging]}", hosts[1])
	}
}

func TestHostEntryMixed(t *testing.T) {
	content := `
groups:
  test:
    hosts:
      - simple-host
      - host: tagged-host
        tags: [arm64]
`
	cfg := loadFromString(t, content)
	hosts := cfg.Groups["test"].Hosts
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}
	if hosts[0].Host != "simple-host" || len(hosts[0].Tags) != 0 {
		t.Errorf("hosts[0] = %+v, want bare simple-host", hosts[0])
	}
	if hosts[1].Host != "tagged-host" || len(hosts[1].Tags) != 1 {
		t.Errorf("hosts[1] = %+v, want tagged-host with [arm64]", hosts[1])
	}
}

func TestHostEntryMarshalRoundTrip(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Groups["test"] = Group{
		Hosts: []HostEntry{
			{Host: "bare-host"},
			{Host: "tagged-host", Tags: []string{"prod", "arm64"}},
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := Save(path, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	hosts := loaded.Groups["test"].Hosts
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}
	if hosts[0].Host != "bare-host" || len(hosts[0].Tags) != 0 {
		t.Errorf("hosts[0] = %+v, want bare-host with no tags", hosts[0])
	}
	if hosts[1].Host != "tagged-host" || len(hosts[1].Tags) != 2 {
		t.Errorf("hosts[1] = %+v, want tagged-host with 2 tags", hosts[1])
	}
}

func TestHostEntryMissingHost(t *testing.T) {
	content := `
groups:
  test:
    hosts:
      - tags: [prod]
`
	_, err := loadStringRaw(content)
	if err == nil {
		t.Fatal("expected error for host entry missing 'host' field")
	}
}

func TestValidateInvalidTag(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Groups["test"] = Group{
		Hosts: []HostEntry{
			{Host: "server1", Tags: []string{"valid", "has space"}},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for tag with space")
	}
}

func loadStringRaw(content string) (*Config, error) {
	dir, err := os.MkdirTemp("", "herd-config-test")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return nil, err
	}
	return Load(path)
}
