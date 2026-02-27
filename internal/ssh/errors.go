package ssh

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// ConnectError wraps an SSH connection error with a user-friendly hint.
type ConnectError struct {
	Host string
	Err  error
	Hint string
}

func (e *ConnectError) Error() string {
	return fmt.Sprintf("%s: %v\n  hint: %s", e.Host, e.Err, e.Hint)
}

func (e *ConnectError) Unwrap() error {
	return e.Err
}

// WrapConnectError wraps an SSH connection error with a friendly hint.
// If the error doesn't match any known patterns, it's returned as-is.
func WrapConnectError(host string, err error) error {
	if err == nil {
		return nil
	}

	msg := err.Error()

	// Permission denied on SSH key file.
	if strings.Contains(msg, "permission denied") && strings.Contains(msg, "key") {
		return &ConnectError{
			Host: host,
			Err:  err,
			Hint: "check SSH key permissions (chmod 600)",
		}
	}

	// SSH authentication failure.
	if strings.Contains(msg, "unable to authenticate") ||
		strings.Contains(msg, "no supported methods remain") ||
		strings.Contains(msg, "handshake failed") {
		return &ConnectError{
			Host: host,
			Err:  err,
			Hint: fmt.Sprintf("verify your SSH key or agent. Try: ssh -v %s", host),
		}
	}

	// Connection refused.
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if strings.Contains(msg, "connection refused") {
			return &ConnectError{
				Host: host,
				Err:  err,
				Hint: "verify SSH daemon is running on the target host",
			}
		}
	}
	if strings.Contains(msg, "connection refused") {
		return &ConnectError{
			Host: host,
			Err:  err,
			Hint: "verify SSH daemon is running on the target host",
		}
	}

	// DNS resolution failure.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return &ConnectError{
			Host: host,
			Err:  err,
			Hint: "verify hostname is correct",
		}
	}
	if strings.Contains(msg, "no such host") || strings.Contains(msg, "lookup") {
		return &ConnectError{
			Host: host,
			Err:  err,
			Hint: "verify hostname is correct",
		}
	}

	// Known hosts: missing entry.
	if strings.Contains(msg, "no known_hosts") || strings.Contains(msg, "knownhosts") {
		return &ConnectError{
			Host: host,
			Err:  err,
			Hint: fmt.Sprintf("use --insecure or connect once with: ssh %s", host),
		}
	}

	// Known hosts: key mismatch.
	var keyErr *knownhosts.KeyError
	if errors.As(err, &keyErr) {
		return &ConnectError{
			Host: host,
			Err:  err,
			Hint: fmt.Sprintf("remove old key with: ssh-keygen -R %s", host),
		}
	}

	// Generic SSH handshake error.
	if strings.Contains(msg, "ssh:") {
		// Check specifically for auth-related SSH errors.
		var sshErr *ssh.ServerAuthError
		if errors.As(err, &sshErr) {
			return &ConnectError{
				Host: host,
				Err:  err,
				Hint: fmt.Sprintf("verify your SSH key or agent. Try: ssh -v %s", host),
			}
		}
	}

	return err
}
