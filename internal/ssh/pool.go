package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"

	"github.com/agent462/herd/internal/executor"
)

// dialResult holds the outcome of a Dial attempt, shared between goroutines
// waiting for the same host connection.
type dialResult struct {
	client *Client
	err    error
}

// Pool manages persistent SSH connections to multiple hosts.
// It implements executor.Runner, reusing cached connections across commands
// and automatically reconnecting on stale connections.
type Pool struct {
	mu        sync.Mutex
	clients   map[string]*Client
	inflight  map[string]chan dialResult // per-host dial coordination
	baseConf  ClientConfig
	hostConfs map[string]HostConfig
}

// NewPool creates a connection pool with the given base config and per-host overrides.
func NewPool(baseConf ClientConfig, hostConfs map[string]HostConfig) *Pool {
	return &Pool{
		clients:   make(map[string]*Client),
		inflight:  make(map[string]chan dialResult),
		baseConf:  baseConf,
		hostConfs: hostConfs,
	}
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
		return nil, nil, -1, fmt.Errorf("connect: %w", err)
	}
	return client.RunCommand(ctx, command)
}

func (p *Pool) getOrDial(ctx context.Context, host string) (*Client, error) {
	p.mu.Lock()

	// Fast path: already connected.
	if client, ok := p.clients[host]; ok {
		p.mu.Unlock()
		return client, nil
	}

	// Check if another goroutine is already dialing this host.
	if ch, ok := p.inflight[host]; ok {
		p.mu.Unlock()
		// Wait for the in-flight dial to complete.
		select {
		case res := <-ch:
			// Put the result back so other waiters can also read it.
			ch <- res
			return res.client, res.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	// We are the first to dial this host. Create a coordination channel.
	ch := make(chan dialResult, 1)
	p.inflight[host] = ch
	p.mu.Unlock()

	conf, dialHost := resolveHostConf(p.baseConf, p.hostConfs, host)
	client, err := Dial(ctx, dialHost, conf)

	p.mu.Lock()
	delete(p.inflight, host)
	if err == nil {
		p.clients[host] = client
	}
	p.mu.Unlock()

	// Broadcast result to any waiters.
	ch <- dialResult{client: client, err: err}

	return client, err
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
