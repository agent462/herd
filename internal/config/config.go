package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the top-level herd configuration.
type Config struct {
	Groups   map[string]Group `yaml:"groups"`
	Defaults Defaults         `yaml:"defaults"`
}

// Group defines a named set of hosts with optional overrides.
type Group struct {
	Hosts   []string `yaml:"hosts"`
	User    string   `yaml:"user,omitempty"`
	Timeout Duration `yaml:"timeout,omitempty"`
}

// Defaults holds default settings.
type Defaults struct {
	Concurrency int      `yaml:"concurrency"`
	Timeout     Duration `yaml:"timeout"`
	Output      string   `yaml:"output"` // "grouped" or "json"
}

// Duration wraps time.Duration to support YAML unmarshaling from strings like "30s".
type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	d.Duration = dur
	return nil
}

func (d Duration) MarshalYAML() (interface{}, error) {
	return d.Duration.String(), nil
}

// DefaultConfig returns a Config with sensible default values.
func DefaultConfig() *Config {
	return &Config{
		Groups: make(map[string]Group),
		Defaults: Defaults{
			Concurrency: 20,
			Timeout:     Duration{30 * time.Second},
			Output:      "grouped",
		},
	}
}

// defaultConfigPath returns the default config file path.
// Respects $XDG_CONFIG_HOME if set, otherwise falls back to ~/.config.
func defaultConfigPath() string {
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir != "" {
		return filepath.Join(configDir, "herd", "config.yaml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "herd", "config.yaml")
}

// Load reads and parses a config YAML file from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// LoadDefault loads the config from the default path (~/.config/herd/config.yaml).
// If the file does not exist, it returns the default config.
func LoadDefault() (*Config, error) {
	path := defaultConfigPath()
	if path == "" {
		return DefaultConfig(), nil
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return DefaultConfig(), nil
	}

	return Load(path)
}

// Validate checks the config for logical errors.
func (c *Config) Validate() error {
	if c.Defaults.Concurrency < 0 {
		return fmt.Errorf("concurrency must be non-negative, got %d", c.Defaults.Concurrency)
	}
	if c.Defaults.Timeout.Duration < 0 {
		return fmt.Errorf("default timeout must be non-negative, got %s", c.Defaults.Timeout)
	}

	validOutputModes := map[string]bool{"grouped": true, "json": true}
	if c.Defaults.Output != "" && !validOutputModes[c.Defaults.Output] {
		return fmt.Errorf("invalid output mode %q, must be one of: grouped, json", c.Defaults.Output)
	}

	for name, group := range c.Groups {
		if len(group.Hosts) == 0 {
			return fmt.Errorf("group %q has no hosts", name)
		}
		if group.Timeout.Duration < 0 {
			return fmt.Errorf("group %q has negative timeout: %s", name, group.Timeout)
		}
	}

	return nil
}
