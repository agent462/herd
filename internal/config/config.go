package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// Config represents the top-level herd configuration.
type Config struct {
	Groups   map[string]Group   `yaml:"groups"`
	Defaults Defaults           `yaml:"defaults"`
	Recipes  map[string]Recipe  `yaml:"recipes,omitempty"`
	Parsers  map[string]Parser  `yaml:"parsers,omitempty"`
}

// Recipe defines a named multi-step command sequence.
type Recipe struct {
	Description string   `yaml:"description,omitempty"`
	Steps       []string `yaml:"steps"`
}

// Parser defines named field-extraction rules for structured output parsing.
type Parser struct {
	Description string        `yaml:"description,omitempty"`
	Extract     []ExtractRule `yaml:"extract"`
}

// ExtractRule defines how to extract a single field from command output.
type ExtractRule struct {
	Field   string `yaml:"field"`
	Pattern string `yaml:"pattern,omitempty"` // regex with capture group
	Column  int    `yaml:"column,omitempty"`  // extract column by index (1-based)
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

// DefaultConfigPath returns the default config file path.
// Respects $XDG_CONFIG_HOME if set, otherwise falls back to ~/.config.
func DefaultConfigPath() string {
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
	path := DefaultConfigPath()
	if path == "" {
		return DefaultConfig(), nil
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return DefaultConfig(), nil
	}

	return Load(path)
}

// Save writes the config to the given file path as YAML.
// It creates parent directories if they don't exist.
func Save(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
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

	nameRe := regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	for name, recipe := range c.Recipes {
		if !nameRe.MatchString(name) {
			return fmt.Errorf("recipe name %q must match [a-zA-Z0-9_-]+", name)
		}
		if len(recipe.Steps) == 0 {
			return fmt.Errorf("recipe %q has no steps", name)
		}
	}

	for name, parser := range c.Parsers {
		if !nameRe.MatchString(name) {
			return fmt.Errorf("parser name %q must match [a-zA-Z0-9_-]+", name)
		}
		if len(parser.Extract) == 0 {
			return fmt.Errorf("parser %q has no extract rules", name)
		}
		for i, rule := range parser.Extract {
			if rule.Field == "" {
				return fmt.Errorf("parser %q rule %d has empty field name", name, i)
			}
			if rule.Pattern == "" && rule.Column == 0 {
				return fmt.Errorf("parser %q rule %d (%s) must have pattern or column", name, i, rule.Field)
			}
		}
	}

	return nil
}
