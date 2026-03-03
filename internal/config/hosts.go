package config

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kevinburke/ssh_config"

	"github.com/agent462/herd/internal/pathutil"
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
	Tags         []string // tags from config HostEntry
}

// ResolveHosts resolves a list of hosts from a combination of a config group
// and CLI-provided host names. If groupName is specified, hosts are loaded from
// the config group. If cliHosts are provided, they are used. If both are given,
// the results are merged (deduplicated, CLI hosts appended after group hosts).
func ResolveHosts(cfg *Config, groupName string, cliHosts []string) ([]Host, error) {
	if groupName == "" && len(cliHosts) == 0 {
		return nil, fmt.Errorf("no hosts specified: provide a group (-g) or host names as arguments")
	}

	var entries []HostEntry
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
		entries = append(entries, group.Hosts...)
		groupUser = group.User
		groupTimeout = group.Timeout
	}

	// Append CLI hosts as tag-less entries, deduplicating against group hosts.
	if len(cliHosts) > 0 {
		seen := make(map[string]bool, len(entries))
		for _, e := range entries {
			seen[e.Host] = true
		}
		for _, h := range cliHosts {
			if !seen[h] {
				entries = append(entries, HostEntry{Host: h})
				seen[h] = true
			}
		}
	}

	hosts := make([]Host, 0, len(entries))
	for _, entry := range entries {
		host := Host{Name: entry.Host, Hostname: entry.Host, Port: 22, Tags: entry.Tags}

		// Parse user@host syntax.
		if user, hostname, ok := parseUserAtHost(entry.Host); ok {
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

// ResolveHostsByTag resolves hosts from ALL groups that match the given tag
// expression. Tags are AND-ed (comma-separated), and a leading "!" negates.
// Returns deduplicated hosts. Group-level User/Timeout overrides are NOT applied
// because a host may appear in multiple groups with different settings.
func ResolveHostsByTag(cfg *Config, tagExpr string) ([]Host, error) {
	required, negated := ParseTagExpr(tagExpr)

	// First pass: collect and merge tags across all groups for each host.
	type hostInfo struct {
		entry HostEntry
		order int // insertion order for stable output
	}
	merged := make(map[string]*hostInfo)
	var order int

	// Sort group names for deterministic iteration order — the insertion
	// order counter drives the final host ordering, so map randomness here
	// would produce non-deterministic output.
	groupNames := make([]string, 0, len(cfg.Groups))
	for name := range cfg.Groups {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)

	for _, gn := range groupNames {
		group := cfg.Groups[gn]
		for _, entry := range group.Hosts {
			if existing, ok := merged[entry.Host]; ok {
				// Merge new tags (union, deduplicated).
				tagSet := make(map[string]bool, len(existing.entry.Tags))
				for _, t := range existing.entry.Tags {
					tagSet[t] = true
				}
				for _, t := range entry.Tags {
					if !tagSet[t] {
						existing.entry.Tags = append(existing.entry.Tags, t)
					}
				}
			} else {
				tags := make([]string, len(entry.Tags))
				copy(tags, entry.Tags)
				merged[entry.Host] = &hostInfo{
					entry: HostEntry{Host: entry.Host, Tags: tags},
					order: order,
				}
				order++
			}
		}
	}

	// Second pass: filter by tag expression, preserving insertion order.
	hosts := make([]Host, 0)
	ordered := make([]*hostInfo, 0, len(merged))
	for _, info := range merged {
		ordered = append(ordered, info)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].order < ordered[j].order })

	for _, info := range ordered {
		if MatchesTags(info.entry.Tags, required, negated) {
			host := Host{Name: info.entry.Host, Hostname: info.entry.Host, Port: 22, Tags: info.entry.Tags}
			if user, hostname, ok := parseUserAtHost(info.entry.Host); ok {
				host.Hostname = hostname
				host.User = user
			}
			MergeSSHConfig(&host)
			hosts = append(hosts, host)
		}
	}

	if len(hosts) == 0 {
		return nil, fmt.Errorf("no hosts match tag expression %q", tagExpr)
	}
	return hosts, nil
}

// ParseTagExpr splits a comma-separated tag expression into required and negated tags.
// Example: "debian12,arm64,!staging" -> (["debian12","arm64"], ["staging"])
func ParseTagExpr(expr string) (required, negated []string) {
	for _, t := range strings.Split(expr, ",") {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "!") {
			if rest := t[1:]; rest != "" {
				negated = append(negated, rest)
			}
		} else {
			required = append(required, t)
		}
	}
	return
}

// AllTags returns a map of tag name to host count across all groups.
// Hosts appearing in multiple groups are counted only once.
func AllTags(cfg *Config) map[string]int {
	counts := make(map[string]int)
	// Collect unique tags per host across all groups, then count.
	hostTags := make(map[string]map[string]bool)
	for _, group := range cfg.Groups {
		for _, entry := range group.Hosts {
			if hostTags[entry.Host] == nil {
				hostTags[entry.Host] = make(map[string]bool)
			}
			for _, tag := range entry.Tags {
				hostTags[entry.Host][tag] = true
			}
		}
	}
	for _, tags := range hostTags {
		for tag := range tags {
			counts[tag]++
		}
	}
	return counts
}

// MatchesTags reports whether a host's tags satisfy the required/negated constraints.
// All required tags must be present and no negated tags may be present.
func MatchesTags(hostTags []string, required, negated []string) bool {
	tagSet := make(map[string]bool, len(hostTags))
	for _, t := range hostTags {
		tagSet[t] = true
	}
	for _, r := range required {
		if !tagSet[r] {
			return false
		}
	}
	for _, n := range negated {
		if tagSet[n] {
			return false
		}
	}
	return true
}

// MergeSSHConfig reads ~/.ssh/config and fills in Hostname, User, Port,
// IdentityFile, and ProxyJump for the host if they are not already set.
// Lookups use the original host Name (the SSH config alias), not the
// resolved Hostname, so that Host directives match correctly.
func MergeSSHConfig(host *Host) {
	// Use the original Name for ssh_config lookups so Host aliases match.
	lookup := host.Name
	// If the name was "user@host", use just the host part for lookup.
	if _, hostname, ok := parseUserAtHost(lookup); ok {
		lookup = hostname
	}

	// Resolve Hostname (ssh_config may map a Host alias to a different address).
	if hn := sshConfigGet(lookup, "Hostname"); hn != "" {
		host.Hostname = hn
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
			expanded := pathutil.ExpandHome(identity)
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

