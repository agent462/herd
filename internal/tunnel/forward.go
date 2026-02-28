package tunnel

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseForwardSpec parses an SSH -L style forward specification.
// Format: localPort:remoteHost:remotePort
// Examples: "8080:localhost:80", "3306:db.internal:3306"
func ParseForwardSpec(spec string) (Forward, error) {
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) != 3 {
		return Forward{}, fmt.Errorf("invalid forward spec %q: expected localPort:remoteHost:remotePort", spec)
	}

	localPort, err := strconv.Atoi(parts[0])
	if err != nil {
		return Forward{}, fmt.Errorf("invalid local port %q: %w", parts[0], err)
	}
	if localPort < 0 || localPort > 65535 {
		return Forward{}, fmt.Errorf("local port %d out of range (0-65535)", localPort)
	}

	remoteHost := parts[1]
	if remoteHost == "" {
		return Forward{}, fmt.Errorf("remote host must not be empty in spec %q", spec)
	}

	remotePort, err := strconv.Atoi(parts[2])
	if err != nil {
		return Forward{}, fmt.Errorf("invalid remote port %q: %w", parts[2], err)
	}
	if remotePort < 1 || remotePort > 65535 {
		return Forward{}, fmt.Errorf("remote port %d out of range (1-65535)", remotePort)
	}

	return Forward{
		LocalPort:  localPort,
		RemoteHost: remoteHost,
		RemotePort: remotePort,
	}, nil
}
