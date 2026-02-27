package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"

	"golang.org/x/sync/singleflight"

	"github.com/agent462/herd/internal/executor"
)

// Pool manages persistent SSH connections to multiple hosts.
// It implements executor.Runner, reusing cached connections across commands
// and automatically reconnecting on stale connections.
type Pool struct {
	mu           sync.Mutex
	clients      map[string]*Client
	dialGroup    singleflight.Group // deduplicates concurrent dials to the same host
	baseConf     ClientConfig
	hostConfs    map[string]HostConfig
	sudo         bool
	sudoPassword string
}

// NewPool creates a connection pool with the given base config and per-host overrides.
func NewPool(baseConf ClientConfig, hostConfs map[string]HostConfig) *Pool {
	return &Pool{
		clients:   make(map[string]*Client),
		baseConf:  baseConf,
		hostConfs: hostConfs,
	}
}

// SetSudo enables or disables sudo mode. When password is non-empty, a PTY
// is used to deliver it. When password is empty but enable is true, commands
// are prefixed with "sudo" for passwordless (NOPASSWD) execution.
func (p *Pool) SetSudo(enable bool, password string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sudo = enable
	p.sudoPassword = password
}

// Run implements executor.Runner. It reuses a cached connection if available,
// dialing a new one if needed. If a command fails with what looks like a
// connection error, it evicts the cached connection and retries once.
func (p *Pool) Run(ctx context.Context, host string, command string) *executor.HostResult {
	result := &executor.HostResult{Host: host}

	stdout, stderr, exitCode, err := p.exec(ctx, host, command)
	if err != nil && isReconnectable(err) {
		p.evict(host)
		stdout, stderr, exitCode, err = p.exec(ctx, host, command)
	}

	result.Stdout = stdout
	result.Stderr = stderr
	result.ExitCode = exitCode
	result.Err = err
	return result
}

func (p *Pool) exec(ctx context.Context, host string, command string) ([]byte, []byte, int, error) {
	client, err := p.getOrDial(ctx, host)
	if err != nil {
		return nil, nil, -1, WrapConnectError(host, fmt.Errorf("connect: %w", err))
	}

	p.mu.Lock()
	sudo := p.sudo
	sudoPW := p.sudoPassword
	p.mu.Unlock()

	if sudo && sudoPW != "" {
		return client.RunCommandWithSudo(ctx, command, sudoPW)
	}
	if sudo {
		return client.RunCommand(ctx, "sudo "+command)
	}
	return client.RunCommand(ctx, command)
}

func (p *Pool) getOrDial(ctx context.Context, host string) (*Client, error) {
	p.mu.Lock()
	if client, ok := p.clients[host]; ok {
		p.mu.Unlock()
		return client, nil
	}
	p.mu.Unlock()

	// Use singleflight to deduplicate concurrent dials to the same host.
	// DoChan lets each caller respect its own context cancellation.
	ch := p.dialGroup.DoChan(host, func() (interface{}, error) {
		conf, dialHost := resolveHostConf(p.baseConf, p.hostConfs, host)
		client, err := Dial(ctx, dialHost, conf)
		if err != nil {
			return nil, err
		}
		p.mu.Lock()
		p.clients[host] = client
		p.mu.Unlock()
		return client, nil
	})

	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.(*Client), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *Pool) evict(host string) {
	p.mu.Lock()
	client, ok := p.clients[host]
	if ok {
		delete(p.clients, host)
	}
	p.mu.Unlock()

	if ok {
		client.Close()
	}
}

// GetClient returns a connected Client for the given host, reusing a cached
// connection if available. This is used by SFTP and other subsystems that
// need direct access to the SSH connection.
func (p *Pool) GetClient(ctx context.Context, host string) (*Client, error) {
	return p.getOrDial(ctx, host)
}

// IsConnected reports whether a cached connection exists for the given host.
func (p *Pool) IsConnected(host string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, ok := p.clients[host]
	return ok
}

// Close closes all cached connections and resets the pool.
func (p *Pool) Close() error {
	p.mu.Lock()
	clients := p.clients
	p.clients = make(map[string]*Client)
	p.mu.Unlock()

	var firstErr error
	for _, client := range clients {
		if err := client.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// resolveHostConf applies per-host overrides to a base SSH client config.
func resolveHostConf(base ClientConfig, hostConfs map[string]HostConfig, host string) (ClientConfig, string) {
	conf := base
	dialHost := host
	if hc, ok := hostConfs[host]; ok {
		if hc.Hostname != "" {
			dialHost = hc.Hostname
		}
		if hc.User != "" {
			conf.User = hc.User
		}
		if hc.Port > 0 {
			conf.Port = hc.Port
		}
		if hc.IdentityFile != "" {
			conf.IdentityFiles = []string{hc.IdentityFile}
		}
		if hc.ProxyJump != "" {
			conf.ProxyJump = hc.ProxyJump
		}
	}
	return conf, dialHost
}

// isReconnectable returns true if the error suggests a stale/broken connection
// that might succeed on retry with a fresh dial. It returns false for errors
// that are permanent (auth failures, context cancellation) to avoid unnecessary
// retry attempts.
func isReconnectable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	// Detect closed/reset connections.
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return true
	}
	msg := err.Error()
	if strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") {
		return true
	}
	return false
}
