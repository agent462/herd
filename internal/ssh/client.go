package ssh

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	sshconfig "github.com/kevinburke/ssh_config"

	"github.com/agent462/herd/internal/pathutil"
)

// PasswordCallback is called when agent and key-based auth both fail.
// It receives the hostname and should return the password.
type PasswordCallback func(host string) (string, error)

// ClientConfig holds options for creating an SSH client.
type ClientConfig struct {
	// User overrides the SSH username. If empty, resolved from
	// ~/.ssh/config or the current OS user.
	User string

	// Port overrides the SSH port. If zero, resolved from
	// ~/.ssh/config or defaults to 22.
	Port int

	// IdentityFiles lists explicit private key paths to try.
	// If empty, resolved from ~/.ssh/config and default key locations.
	IdentityFiles []string

	// PasswordCallback is invoked when agent and key auth fail.
	PasswordCallback PasswordCallback

	// AcceptUnknownHosts controls whether to accept hosts not in known_hosts.
	AcceptUnknownHosts bool

	// HostKeyCallback overrides the default host key verification.
	// If nil, knownhosts is used (with AcceptUnknownHosts controlling unknowns).
	HostKeyCallback ssh.HostKeyCallback

	// ProxyJump specifies one or more comma-separated SSH jump hosts
	// (e.g. "bastion" or "user@jump1:2222,user@jump2").
	// "none" disables proxy jumping (SSH convention).
	ProxyJump string
}

// Client wraps an SSH connection to a single host.
type Client struct {
	host        string
	sshClient   *ssh.Client
	clientConf  ClientConfig
	jumpClients []*Client // intermediate jump-host clients, for cleanup
}

// Dial connects to the given host using the configured auth chain.
// If conf.ProxyJump is set (and not "none"), the connection is tunneled
// through one or more jump hosts.
func Dial(ctx context.Context, host string, conf ClientConfig) (*Client, error) {
	if conf.ProxyJump != "" && conf.ProxyJump != "none" {
		return dialViaProxy(ctx, host, conf)
	}
	return dialDirect(ctx, host, conf)
}

// dialDirect establishes a direct SSH connection (no proxy).
func dialDirect(ctx context.Context, host string, conf ClientConfig) (*Client, error) {
	addr, user, authMethods, err := resolveConnection(host, conf)
	if err != nil {
		return nil, fmt.Errorf("resolve connection for %s: %w", host, err)
	}

	hostKeyCallback, err := resolveHostKeyCallback(conf)
	if err != nil {
		return nil, fmt.Errorf("host key callback: %w", err)
	}

	sshConf := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
	}

	conn, err := dialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	// Perform SSH handshake with context cancellation.
	sshConn, chans, reqs, err := newClientConn(ctx, conn, addr, sshConf)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("ssh handshake with %s: %w", addr, err)
	}

	client := ssh.NewClient(sshConn, chans, reqs)
	return &Client{
		host:       host,
		sshClient:  client,
		clientConf: conf,
	}, nil
}

// dialViaProxy chains through one or more comma-separated jump hosts,
// then dials the final target through the last jump connection.
func dialViaProxy(ctx context.Context, host string, conf ClientConfig) (*Client, error) {
	specs := strings.Split(conf.ProxyJump, ",")
	var jumpClients []*Client

	// buildJumpConf creates a config for a jump host, inheriting auth settings
	// from the original config and applying overrides from the jump spec.
	buildJumpConf := func(spec string) (ClientConfig, string) {
		jumpUser, jumpHostname, jumpPort := parseJumpHost(spec)
		jc := ClientConfig{
			Port:               jumpPort,
			IdentityFiles:      conf.IdentityFiles,
			PasswordCallback:   conf.PasswordCallback,
			AcceptUnknownHosts: conf.AcceptUnknownHosts,
			HostKeyCallback:    conf.HostKeyCallback,
		}
		if jumpUser != "" {
			jc.User = jumpUser
		}
		return jc, jumpHostname
	}

	// Connect to the first jump host directly.
	jumpConf, jumpHostname := buildJumpConf(specs[0])
	prevClient, err := dialDirect(ctx, jumpHostname, jumpConf)
	if err != nil {
		return nil, fmt.Errorf("dial jump host %q: %w", specs[0], err)
	}
	jumpClients = append(jumpClients, prevClient)

	// Chain through remaining jump hosts (if any).
	for _, spec := range specs[1:] {
		jumpConf, jumpHostname = buildJumpConf(spec)
		nextClient, err := dialThrough(ctx, prevClient, jumpHostname, jumpConf)
		if err != nil {
			// Clean up previously established connections.
			for i := len(jumpClients) - 1; i >= 0; i-- {
				jumpClients[i].Close()
			}
			return nil, fmt.Errorf("dial jump host %q: %w", spec, err)
		}
		jumpClients = append(jumpClients, nextClient)
		prevClient = nextClient
	}

	// Dial the final target through the last jump client.
	finalConf := conf
	finalConf.ProxyJump = "" // prevent infinite recursion
	finalClient, err := dialThrough(ctx, prevClient, host, finalConf)
	if err != nil {
		for i := len(jumpClients) - 1; i >= 0; i-- {
			jumpClients[i].Close()
		}
		return nil, fmt.Errorf("dial target %s via proxy: %w", host, err)
	}
	finalClient.jumpClients = jumpClients
	return finalClient, nil
}

// dialThrough tunnels an SSH connection through an existing client.
func dialThrough(ctx context.Context, proxy *Client, host string, conf ClientConfig) (*Client, error) {
	addr, user, authMethods, err := resolveConnection(host, conf)
	if err != nil {
		return nil, fmt.Errorf("resolve connection for %s: %w", host, err)
	}

	hostKeyCallback, err := resolveHostKeyCallback(conf)
	if err != nil {
		return nil, fmt.Errorf("host key callback: %w", err)
	}

	sshConf := &ssh.ClientConfig{
		User:            user,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
	}

	// Open a tunnel through the proxy's SSH connection.
	conn, err := proxy.sshClient.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("tunnel through %s to %s: %w", proxy.host, addr, err)
	}

	sshConn, chans, reqs, err := newClientConn(ctx, conn, addr, sshConf)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("ssh handshake with %s (via %s): %w", addr, proxy.host, err)
	}

	client := ssh.NewClient(sshConn, chans, reqs)
	return &Client{
		host:       host,
		sshClient:  client,
		clientConf: conf,
	}, nil
}

// parseJumpHost parses a jump host spec in the form "user@host:port",
// "host:port", "user@host", or just "host". Returns user, hostname, port.
func parseJumpHost(spec string) (user, hostname string, port int) {
	spec = strings.TrimSpace(spec)

	// Extract user if present.
	if i := strings.Index(spec, "@"); i >= 0 {
		user = spec[:i]
		spec = spec[i+1:]
	}

	// Extract port if present (host:port).
	if host, portStr, err := net.SplitHostPort(spec); err == nil {
		hostname = host
		fmt.Sscanf(portStr, "%d", &port)
	} else {
		hostname = spec
	}

	return user, hostname, port
}

// RunCommand executes a command on the connected host and returns
// stdout, stderr, exit code, and any error.
func (c *Client) RunCommand(ctx context.Context, command string) (stdout, stderr []byte, exitCode int, err error) {
	session, err := c.sshClient.NewSession()
	if err != nil {
		return nil, nil, -1, fmt.Errorf("new session: %w", err)
	}
	defer session.Close()

	// Set up pipes for stdout/stderr.
	var outBuf, errBuf safeBuffer
	session.Stdout = &outBuf
	session.Stderr = &errBuf

	// Run the command, respecting context cancellation.
	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case <-ctx.Done():
		// Signal the session to close, which will cause Run to return.
		session.Signal(ssh.SIGKILL)
		session.Close()
		return nil, nil, -1, ctx.Err()
	case err := <-done:
		if err != nil {
			if exitErr, ok := err.(*ssh.ExitError); ok {
				return outBuf.Bytes(), errBuf.Bytes(), exitErr.ExitStatus(), nil
			}
			return outBuf.Bytes(), errBuf.Bytes(), -1, err
		}
		return outBuf.Bytes(), errBuf.Bytes(), 0, nil
	}
}

// Close closes the underlying SSH connection and any jump-host connections
// in reverse order (innermost first).
func (c *Client) Close() error {
	var firstErr error
	if c.sshClient != nil {
		firstErr = c.sshClient.Close()
	}
	// Close jump clients in reverse order.
	for i := len(c.jumpClients) - 1; i >= 0; i-- {
		if err := c.jumpClients[i].Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Host returns the hostname this client is connected to.
func (c *Client) Host() string {
	return c.host
}

// resolveConnection builds the address, username, and auth methods for a host.
// When values are pre-set in conf (from the config layer's host resolution),
// ssh_config is not re-queried — this avoids double lookups that could use
// the wrong key (resolved hostname vs original alias).
func resolveConnection(host string, conf ClientConfig) (addr, user string, methods []ssh.AuthMethod, err error) {
	// Resolve user: prefer explicit config, fall back to ssh_config, then env.
	user = conf.User
	if user == "" {
		user = sshconfig.Get(host, "User")
	}
	if user == "" {
		user = os.Getenv("USER")
	}
	if user == "" {
		user = "root"
	}

	// Resolve port: prefer explicit config, fall back to ssh_config, then 22.
	port := conf.Port
	if port == 0 {
		portStr := sshconfig.Get(host, "Port")
		if portStr != "" {
			fmt.Sscanf(portStr, "%d", &port)
		}
	}
	if port == 0 {
		port = 22
	}

	// Use the host as-is for the address. The config layer already resolves
	// SSH Hostname directives, so when called via the runner/pool the host
	// parameter is the final hostname to dial.
	addr = net.JoinHostPort(host, fmt.Sprintf("%d", port))

	// Build auth methods in order: agent -> key files -> password.
	methods = buildAuthMethods(host, conf)

	return addr, user, methods, nil
}

// buildAuthMethods constructs the ordered auth chain.
func buildAuthMethods(host string, conf ClientConfig) []ssh.AuthMethod {
	var methods []ssh.AuthMethod

	// 1. SSH agent.
	if agentAuth := agentAuthMethod(); agentAuth != nil {
		methods = append(methods, agentAuth)
	}

	// 2. Key files.
	keyFiles := conf.IdentityFiles
	if len(keyFiles) == 0 {
		keyFiles = resolveKeyFiles(host)
	}
	for _, keyFile := range keyFiles {
		if signer := loadKeySigner(keyFile); signer != nil {
			methods = append(methods, ssh.PublicKeys(signer))
		}
	}

	// 3. Password callback.
	if conf.PasswordCallback != nil {
		methods = append(methods, ssh.PasswordCallback(func() (string, error) {
			return conf.PasswordCallback(host)
		}))
	}

	return methods
}

// sharedAgent holds a lazily-initialized, process-wide SSH agent connection.
// Uses a mutex instead of sync.Once so a failed dial can be retried.
var sharedAgent struct {
	mu     sync.Mutex
	conn   net.Conn
	client agent.ExtendedAgent
}

// CloseAgent closes the shared SSH agent connection, if any.
// This is a no-op if no agent connection has been established.
func CloseAgent() {
	sharedAgent.mu.Lock()
	defer sharedAgent.mu.Unlock()
	if sharedAgent.conn != nil {
		sharedAgent.conn.Close()
		sharedAgent.client = nil
		sharedAgent.conn = nil
	}
}

// agentAuthMethod returns an auth method using the SSH agent, or nil
// if the agent is unavailable or has no keys.
func agentAuthMethod() ssh.AuthMethod {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil
	}

	sharedAgent.mu.Lock()
	defer sharedAgent.mu.Unlock()

	// If we have an existing client, check its health.
	if sharedAgent.client != nil {
		if keys, err := sharedAgent.client.List(); err == nil {
			if len(keys) > 0 {
				return ssh.PublicKeysCallback(sharedAgent.client.Signers)
			}
			return nil
		}
		// Stale connection — close and retry.
		sharedAgent.conn.Close()
		sharedAgent.client = nil
		sharedAgent.conn = nil
	}

	// Attempt a fresh connection.
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil
	}
	sharedAgent.conn = conn
	sharedAgent.client = agent.NewClient(conn)

	keys, err := sharedAgent.client.List()
	if err != nil || len(keys) == 0 {
		return nil
	}
	return ssh.PublicKeysCallback(sharedAgent.client.Signers)
}

// resolveKeyFiles returns key file paths from ssh_config and default locations.
func resolveKeyFiles(host string) []string {
	var files []string

	// Check ssh_config for IdentityFile.
	identity := sshconfig.Get(host, "IdentityFile")
	if identity != "" {
		expanded := pathutil.ExpandHome(identity)
		if _, err := os.Stat(expanded); err == nil {
			files = append(files, expanded)
		}
	}

	// Default key locations.
	home, err := os.UserHomeDir()
	if err != nil {
		return files
	}
	defaults := []string{
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_rsa"),
		filepath.Join(home, ".ssh", "id_ecdsa"),
	}
	for _, f := range defaults {
		if _, err := os.Stat(f); err == nil {
			files = append(files, f)
		}
	}

	return files
}

// loadKeySigner reads a private key file and returns a signer.
func loadKeySigner(path string) ssh.Signer {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		return nil
	}
	return signer
}

// resolveHostKeyCallback builds the host key callback.
func resolveHostKeyCallback(conf ClientConfig) (ssh.HostKeyCallback, error) {
	if conf.HostKeyCallback != nil {
		return conf.HostKeyCallback, nil
	}

	if conf.AcceptUnknownHosts {
		return ssh.InsecureIgnoreHostKey(), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}

	knownHostsPath := filepath.Join(home, ".ssh", "known_hosts")
	if _, err := os.Stat(knownHostsPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no known_hosts file found at %s; use --insecure to skip host key verification", knownHostsPath)
	}

	callback, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("parse known_hosts: %w", err)
	}
	return callback, nil
}

// dialContext dials a network address with context cancellation support.
func dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	d := net.Dialer{}
	return d.DialContext(ctx, network, addr)
}

// newClientConn performs the SSH handshake with context cancellation.
func newClientConn(ctx context.Context, conn net.Conn, addr string, config *ssh.ClientConfig) (ssh.Conn, <-chan ssh.NewChannel, <-chan *ssh.Request, error) {
	type result struct {
		conn  ssh.Conn
		chans <-chan ssh.NewChannel
		reqs  <-chan *ssh.Request
		err   error
	}

	done := make(chan result, 1)
	go func() {
		c, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
		done <- result{c, chans, reqs, err}
	}()

	select {
	case <-ctx.Done():
		conn.Close()
		return nil, nil, nil, ctx.Err()
	case r := <-done:
		return r.conn, r.chans, r.reqs, r.err
	}
}

