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
	baseConf  ClientConfig
	hostConfs map[string]HostConfig
}

// NewRunner creates an SSHRunner with a base config and per-host overrides.
func NewRunner(baseConf ClientConfig, hostConfs map[string]HostConfig) *SSHRunner {
	return &SSHRunner{
		baseConf:  baseConf,
		hostConfs: hostConfs,
	}
}

// Run executes a command on a single host via SSH.
func (r *SSHRunner) Run(ctx context.Context, host string, command string) *executor.HostResult {
	result := &executor.HostResult{Host: host}

	conf, dialHost := resolveHostConf(r.baseConf, r.hostConfs, host)

	client, err := Dial(ctx, dialHost, conf)
	if err != nil {
		result.Err = fmt.Errorf("connect: %w", err)
		return result
	}
	defer client.Close()

	stdout, stderr, exitCode, err := client.RunCommand(ctx, command)
	result.Stdout = stdout
	result.Stderr = stderr
	result.ExitCode = exitCode
	result.Err = err
	return result
}
