package parser

import "regexp"

// BuiltinParsers returns all built-in parser names and their OutputParser instances.
func BuiltinParsers() map[string]*OutputParser {
	return map[string]*OutputParser{
		"disk":   BuiltinDisk(),
		"free":   BuiltinFree(),
		"uptime": BuiltinUptime(),
	}
}

// BuiltinDisk parses "df -h" output.
// Fields: filesystem, size, used, avail, use_pct, mount
func BuiltinDisk() *OutputParser {
	return &OutputParser{
		rules: []rule{
			{field: "filesystem", re: regexp.MustCompile(`(?m)^(\S+)\s+\S+\s+\S+\s+\S+\s+\S+\s+/\s*$`)},
			{field: "size", re: regexp.MustCompile(`(?m)^\S+\s+(\S+)\s+\S+\s+\S+\s+\S+\s+/\s*$`)},
			{field: "used", re: regexp.MustCompile(`(?m)^\S+\s+\S+\s+(\S+)\s+\S+\s+\S+\s+/\s*$`)},
			{field: "avail", re: regexp.MustCompile(`(?m)^\S+\s+\S+\s+\S+\s+(\S+)\s+\S+\s+/\s*$`)},
			{field: "use_pct", re: regexp.MustCompile(`(?m)^\S+\s+\S+\s+\S+\s+\S+\s+(\S+)\s+/\s*$`)},
			{field: "mount", re: regexp.MustCompile(`(?m)^\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+(/)\s*$`)},
		},
	}
}

// BuiltinFree parses "free -h" output.
// Fields: total, used, free, available
// Extracts from the "Mem:" line.
func BuiltinFree() *OutputParser {
	return &OutputParser{
		rules: []rule{
			{field: "total", re: regexp.MustCompile(`(?m)^Mem:\s+(\S+)`)},
			{field: "used", re: regexp.MustCompile(`(?m)^Mem:\s+\S+\s+(\S+)`)},
			{field: "free", re: regexp.MustCompile(`(?m)^Mem:\s+\S+\s+\S+\s+(\S+)`)},
			{field: "available", re: regexp.MustCompile(`(?m)^Mem:\s+\S+\s+\S+\s+\S+\s+\S+\s+\S+\s+(\S+)`)},
		},
	}
}

// BuiltinUptime parses "uptime" output.
// Fields: uptime, users, load1, load5, load15
func BuiltinUptime() *OutputParser {
	return &OutputParser{
		rules: []rule{
			{field: "uptime", re: regexp.MustCompile(`up\s+(.+?),\s+\d+\s+user`)},
			{field: "users", re: regexp.MustCompile(`(\d+)\s+users?`)},
			{field: "load1", re: regexp.MustCompile(`load average:\s+(\S+),`)},
			{field: "load5", re: regexp.MustCompile(`load average:\s+\S+,\s+(\S+),`)},
			{field: "load15", re: regexp.MustCompile(`load average:\s+\S+,\s+\S+,\s+(\S+)`)},
		},
	}
}
