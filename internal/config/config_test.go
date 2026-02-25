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
	if pis.Hosts[0] != "pi-garage" {
		t.Errorf("pis.Hosts[0] = %q, want \"pi-garage\"", pis.Hosts[0])
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
	cfg.Groups["test"] = Group{Hosts: []string{"host1"}}

	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for invalid output mode")
	}
}

func TestValidateStreamOutputRejected(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Defaults.Output = "stream"
	cfg.Groups["test"] = Group{Hosts: []string{"host1"}}

	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for 'stream' output mode")
	}
}

func TestValidateEmptyGroup(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Groups["empty"] = Group{Hosts: []string{}}

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
