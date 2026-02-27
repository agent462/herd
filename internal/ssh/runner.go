package ssh

import (
	"context"
	"fmt"

	"github.com/agent462/herd/internal/executor"
)

// HostConfig holds per-host SSH connection details.
type HostConfig struct {
	Hostname     string // actual hostname to dial (may differ from the map key)
	User         string
	Port         int
	IdentityFile string
	ProxyJump    string
}

// SSHRunner implements executor.Runner using real SSH connections.
type SSHRunner struct {
	baseConf     ClientConfig
	hostConfs    map[string]HostConfig
	sudo         bool
	sudoPassword string
}

// NewRunner creates an SSHRunner with a base config and per-host overrides.
func NewRunner(baseConf ClientConfig, hostConfs map[string]HostConfig) *SSHRunner {
	return &SSHRunner{
		baseConf:  baseConf,
		hostConfs: hostConfs,
	}
}

// NewRunnerWithSudo creates an SSHRunner that executes all commands with sudo.
// If sudoPassword is empty, commands are prefixed with "sudo" for passwordless
// (NOPASSWD) execution. If sudoPassword is non-empty, a PTY is used to
// deliver the password.
func NewRunnerWithSudo(baseConf ClientConfig, hostConfs map[string]HostConfig, sudoPassword string) *SSHRunner {
	return &SSHRunner{
		baseConf:     baseConf,
		hostConfs:    hostConfs,
		sudo:         true,
		sudoPassword: sudoPassword,
	}
}

// GetClient dials a one-shot SSH connection to the given host.
// The caller is responsible for closing the returned Client.
func (r *SSHRunner) GetClient(ctx context.Context, host string) (*Client, error) {
	conf, dialHost := resolveHostConf(r.baseConf, r.hostConfs, host)
	return Dial(ctx, dialHost, conf)
}

// CloseClient closes a client returned by GetClient. SSHRunner creates
// one-shot connections, so they must be closed after use.
func (r *SSHRunner) CloseClient(client *Client) error {
	return client.Close()
}

// Run executes a command on a single host via SSH.
func (r *SSHRunner) Run(ctx context.Context, host string, command string) *executor.HostResult {
	result := &executor.HostResult{Host: host}

	conf, dialHost := resolveHostConf(r.baseConf, r.hostConfs, host)

	client, err := Dial(ctx, dialHost, conf)
	if err != nil {
		result.Err = WrapConnectError(host, fmt.Errorf("connect: %w", err))
		return result
	}
	defer client.Close()

	var stdout, stderr []byte
	var exitCode int
	if r.sudo && r.sudoPassword != "" {
		stdout, stderr, exitCode, err = client.RunCommandWithSudo(ctx, command, r.sudoPassword)
	} else if r.sudo {
		stdout, stderr, exitCode, err = client.RunCommand(ctx, "sudo "+command)
	} else {
		stdout, stderr, exitCode, err = client.RunCommand(ctx, command)
	}
	result.Stdout = stdout
	result.Stderr = stderr
	result.ExitCode = exitCode
	result.Err = err
	return result
}
