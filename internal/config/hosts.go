package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kevinburke/ssh_config"
)

// Host represents a resolved SSH host with connection details.
type Host struct {
	Name         string        // Display/identity label (original input, e.g. "admin@server1")
	Hostname     string        // Actual SSH hostname to connect to (e.g. "server1")
	User         string
	Port         int
	IdentityFile string
	ProxyJump    string
	Timeout      time.Duration
}

// ResolveHosts resolves a list of hosts from a combination of a config group
// and CLI-provided host names. If groupName is specified, hosts are loaded from
// the config group. If cliHosts are provided, they are used. If both are given,
// the results are merged (deduplicated, CLI hosts appended after group hosts).
func ResolveHosts(cfg *Config, groupName string, cliHosts []string) ([]Host, error) {
	if groupName == "" && len(cliHosts) == 0 {
		return nil, fmt.Errorf("no hosts specified: provide a group (-g) or host names as arguments")
	}

	var hostnames []string
	var groupUser string
	var groupTimeout Duration

	if groupName != "" {
		group, ok := cfg.Groups[groupName]
		if !ok {
			available := make([]string, 0, len(cfg.Groups))
			for name := range cfg.Groups {
				available = append(available, name)
			}
			if len(available) == 0 {
				return nil, fmt.Errorf("group %q not found (no groups defined)", groupName)
			}
			return nil, fmt.Errorf("group %q not found (available: %v)", groupName, available)
		}
		hostnames = append(hostnames, group.Hosts...)
		groupUser = group.User
		groupTimeout = group.Timeout
	}

	// Append CLI hosts, deduplicating against group hosts.
	if len(cliHosts) > 0 {
		seen := make(map[string]bool, len(hostnames))
		for _, h := range hostnames {
			seen[h] = true
		}
		for _, h := range cliHosts {
			if !seen[h] {
				hostnames = append(hostnames, h)
				seen[h] = true
			}
		}
	}

	hosts := make([]Host, 0, len(hostnames))
	for _, name := range hostnames {
		host := Host{Name: name, Hostname: name, Port: 22}

		// Parse user@host syntax.
		if user, hostname, ok := parseUserAtHost(name); ok {
			host.Hostname = hostname
			host.User = user
			// Name stays as the original "user@host" for display and dedup.
		}

		// Apply group-level user override.
		if groupUser != "" {
			host.User = groupUser
		}

		// Apply group-level timeout override.
		if groupTimeout.Duration > 0 {
			host.Timeout = groupTimeout.Duration
		}

		// Merge SSH config values (fills in missing fields).
		MergeSSHConfig(&host)

		hosts = append(hosts, host)
	}

	return hosts, nil
}

// MergeSSHConfig reads ~/.ssh/config and fills in User, Port, IdentityFile,
// and ProxyJump for the host if they are not already set. Lookups use
// the Hostname field (the actual SSH target), not the display Name.
func MergeSSHConfig(host *Host) {
	lookup := host.Hostname
	if lookup == "" {
		lookup = host.Name
	}

	if host.User == "" {
		if user := sshConfigGet(lookup, "User"); user != "" {
			host.User = user
		}
	}

	if host.Port == 22 {
		if portStr := sshConfigGet(lookup, "Port"); portStr != "" {
			if port, err := strconv.Atoi(portStr); err == nil && port > 0 {
				host.Port = port
			}
		}
	}

	if host.IdentityFile == "" {
		if identity := sshConfigGet(lookup, "IdentityFile"); identity != "" {
			expanded := expandTilde(identity)
			if _, err := os.Stat(expanded); err == nil {
				host.IdentityFile = expanded
			}
		}
	}

	if host.ProxyJump == "" {
		if proxy := sshConfigGet(lookup, "ProxyJump"); proxy != "" {
			host.ProxyJump = proxy
		}
	}
}

// sshConfigGet looks up a key for a host in the user's SSH config.
func sshConfigGet(hostname, key string) string {
	val, err := ssh_config.GetStrict(hostname, key)
	if err != nil {
		return ""
	}
	return val
}

// parseUserAtHost splits "user@host" into its components.
// Returns ("", "", false) if the input doesn't contain @ or if the user part is empty.
func parseUserAtHost(s string) (user, host string, ok bool) {
	i := strings.Index(s, "@")
	if i <= 0 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// expandTilde expands a leading ~/ to the user's home directory.
// Paths like ~otheruser/... are returned unchanged since we cannot
// reliably resolve other users' home directories.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~/") && path != "~" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}
