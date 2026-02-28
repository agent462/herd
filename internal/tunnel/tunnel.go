// Package tunnel provides SSH port-forwarding (local tunnels).
package tunnel

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"

	gossh "golang.org/x/crypto/ssh"
)

// Forward describes a port forwarding specification.
type Forward struct {
	LocalPort  int
	RemoteHost string
	RemotePort int
}

// Tunnel represents an active SSH tunnel for a single host.
type Tunnel struct {
	Host       string // SSH host the tunnel goes through
	LocalAddr  string // actual bound address "127.0.0.1:8080"
	RemoteAddr string // "localhost:80"
	listener   net.Listener
	sshClient  *gossh.Client
	done       chan struct{}
	closeOnce  sync.Once
}

// Close stops the tunnel and closes the listener.
func (t *Tunnel) Close() error {
	var err error
	t.closeOnce.Do(func() {
		close(t.done)
		err = t.listener.Close()
	})
	return err
}

// Manager manages multiple SSH tunnels.
type Manager struct {
	mu      sync.Mutex
	tunnels []*Tunnel
}

// NewManager returns a new tunnel manager.
func NewManager() *Manager {
	return &Manager{}
}

// Open creates a tunnel through the given SSH client.
// It binds a local listener on 127.0.0.1:localPort (use 0 for ephemeral).
// Each accepted connection is forwarded to remoteHost:remotePort via the SSH client.
func (m *Manager) Open(ctx context.Context, sshClient *gossh.Client, host string, fwd Forward) (*Tunnel, error) {
	listenAddr := fmt.Sprintf("127.0.0.1:%d", fwd.LocalPort)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", listenAddr, err)
	}

	remoteAddr := net.JoinHostPort(fwd.RemoteHost, fmt.Sprintf("%d", fwd.RemotePort))

	tun := &Tunnel{
		Host:       host,
		LocalAddr:  listener.Addr().String(),
		RemoteAddr: remoteAddr,
		listener:   listener,
		sshClient:  sshClient,
		done:       make(chan struct{}),
	}

	// Accept loop: forward each local connection through the SSH client.
	go func() {
		for {
			local, err := listener.Accept()
			if err != nil {
				// listener.Close() causes Accept to return an error;
				// check if we were asked to stop.
				select {
				case <-tun.done:
					return
				default:
				}
				return
			}

			remote, err := sshClient.Dial("tcp", remoteAddr)
			if err != nil {
				local.Close()
				continue
			}

			go relay(local, remote)
		}
	}()

	m.mu.Lock()
	m.tunnels = append(m.tunnels, tun)
	m.mu.Unlock()

	return tun, nil
}

// Tunnels returns a snapshot of all active tunnels.
func (m *Manager) Tunnels() []*Tunnel {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Tunnel, len(m.tunnels))
	copy(out, m.tunnels)
	return out
}

// Close closes all tunnels managed by this manager.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for _, tun := range m.tunnels {
		if err := tun.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	m.tunnels = nil
	return firstErr
}

// relay copies data bidirectionally between local and remote connections.
func relay(local net.Conn, remote net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(remote, local)
	}()
	go func() {
		defer wg.Done()
		io.Copy(local, remote)
	}()
	wg.Wait()
	local.Close()
	remote.Close()
}
