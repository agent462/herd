package tunnel_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"

	hssh "github.com/agent462/herd/internal/ssh"
	"github.com/agent462/herd/internal/sshtest"
	"github.com/agent462/herd/internal/tunnel"
)

// ---------------------------------------------------------------------------
// ParseForwardSpec tests
// ---------------------------------------------------------------------------

func TestParseForwardSpec(t *testing.T) {
	tests := []struct {
		spec       string
		wantLocal  int
		wantHost   string
		wantRemote int
		wantErr    bool
	}{
		{"8080:localhost:80", 8080, "localhost", 80, false},
		{"0:db.internal:3306", 0, "db.internal", 3306, false},
		{"443:web.example.com:8443", 443, "web.example.com", 8443, false},
		{"abc:localhost:80", 0, "", 0, true},       // non-numeric local port
		{"8080:localhost", 0, "", 0, true},          // wrong number of parts
		{"8080::80", 0, "", 0, true},                // empty remote host
		{"8080:localhost:99999", 0, "", 0, true},    // remote port out of range
		{"-1:localhost:80", 0, "", 0, true},         // negative local port
		{"70000:localhost:80", 0, "", 0, true},      // local port out of range
		{"8080:localhost:0", 0, "", 0, true},        // remote port 0 not allowed
		{"8080:localhost:abc", 0, "", 0, true},      // non-numeric remote port
		{"", 0, "", 0, true},                        // empty string
		{"8080:localhost:80:extra", 0, "", 0, true}, // too many colons treated as bad remote port
	}

	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			fwd, err := tunnel.ParseForwardSpec(tc.spec)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for spec %q, got nil", tc.spec)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for spec %q: %v", tc.spec, err)
			}
			if fwd.LocalPort != tc.wantLocal {
				t.Errorf("LocalPort = %d, want %d", fwd.LocalPort, tc.wantLocal)
			}
			if fwd.RemoteHost != tc.wantHost {
				t.Errorf("RemoteHost = %q, want %q", fwd.RemoteHost, tc.wantHost)
			}
			if fwd.RemotePort != tc.wantRemote {
				t.Errorf("RemotePort = %d, want %d", fwd.RemotePort, tc.wantRemote)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Manager unit tests (no SSH required)
// ---------------------------------------------------------------------------

func TestNewManager(t *testing.T) {
	m := tunnel.NewManager()
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
}

func TestManagerTunnelsEmpty(t *testing.T) {
	m := tunnel.NewManager()
	tuns := m.Tunnels()
	if len(tuns) != 0 {
		t.Errorf("expected 0 tunnels, got %d", len(tuns))
	}
}

func TestManagerCloseEmpty(t *testing.T) {
	m := tunnel.NewManager()
	if err := m.Close(); err != nil {
		t.Errorf("Close on empty manager: %v", err)
	}
}

// ---------------------------------------------------------------------------
// End-to-end tunnel test using sshtest
// ---------------------------------------------------------------------------

// startEchoServer starts a TCP server that echoes back anything it receives.
// Returns the listener address and a cleanup function.
func startEchoServer(t *testing.T) (string, func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo server: %v", err)
	}

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				io.Copy(c, c)
			}(conn)
		}
	}()

	return ln.Addr().String(), func() { ln.Close() }
}

func TestTunnelEndToEnd(t *testing.T) {
	// Start a local TCP echo server as the "remote" service.
	echoAddr, echoCleanup := startEchoServer(t)
	defer echoCleanup()

	echoHost, echoPort := sshtest.ParseAddr(t, echoAddr)

	// Start an in-process SSH server with TCP forwarding enabled.
	pubKey, keyPath := sshtest.GenerateKey(t)
	sshAddr, sshCleanup := sshtest.Start(t,
		sshtest.WithPublicKey(pubKey),
		sshtest.WithForwardTCP(),
	)
	defer sshCleanup()

	// Dial the test SSH server.
	sshHost, sshPort := sshtest.ParseAddr(t, sshAddr)
	client, err := hssh.Dial(context.Background(), sshHost, hssh.ClientConfig{
		Port:               sshPort,
		IdentityFiles:      []string{keyPath},
		AcceptUnknownHosts: true,
	})
	if err != nil {
		t.Fatalf("dial SSH: %v", err)
	}
	defer client.Close()

	// Open a tunnel: local ephemeral port -> echo server through SSH.
	mgr := tunnel.NewManager()
	defer mgr.Close()

	tun, err := mgr.Open(context.Background(), client.SSHClient(), sshHost, tunnel.Forward{
		LocalPort:  0, // ephemeral
		RemoteHost: echoHost,
		RemotePort: echoPort,
	})
	if err != nil {
		t.Fatalf("Open tunnel: %v", err)
	}

	// Verify tunnel metadata.
	if tun.Host != sshHost {
		t.Errorf("tunnel Host = %q, want %q", tun.Host, sshHost)
	}
	if tun.LocalAddr == "" {
		t.Fatal("tunnel LocalAddr is empty")
	}
	expectedRemote := fmt.Sprintf("%s:%d", echoHost, echoPort)
	if tun.RemoteAddr != expectedRemote {
		t.Errorf("tunnel RemoteAddr = %q, want %q", tun.RemoteAddr, expectedRemote)
	}

	// Verify manager tracks the tunnel.
	tuns := mgr.Tunnels()
	if len(tuns) != 1 {
		t.Fatalf("expected 1 tunnel, got %d", len(tuns))
	}

	// Connect through the tunnel and verify data round-trips.
	conn, err := net.Dial("tcp", tun.LocalAddr)
	if err != nil {
		t.Fatalf("dial tunnel: %v", err)
	}
	defer conn.Close()

	msg := []byte("hello through the tunnel")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	buf := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != string(msg) {
		t.Errorf("echoed = %q, want %q", buf, msg)
	}
}

func TestManagerClose(t *testing.T) {
	// Start an SSH server with TCP forwarding for a real tunnel.
	pubKey, keyPath := sshtest.GenerateKey(t)
	sshAddr, sshCleanup := sshtest.Start(t,
		sshtest.WithPublicKey(pubKey),
		sshtest.WithForwardTCP(),
	)
	defer sshCleanup()

	// Start two echo servers to create two tunnels.
	echo1Addr, echo1Cleanup := startEchoServer(t)
	defer echo1Cleanup()
	echo2Addr, echo2Cleanup := startEchoServer(t)
	defer echo2Cleanup()

	echo1Host, echo1Port := sshtest.ParseAddr(t, echo1Addr)
	echo2Host, echo2Port := sshtest.ParseAddr(t, echo2Addr)

	sshHost, sshPort := sshtest.ParseAddr(t, sshAddr)
	client, err := hssh.Dial(context.Background(), sshHost, hssh.ClientConfig{
		Port:               sshPort,
		IdentityFiles:      []string{keyPath},
		AcceptUnknownHosts: true,
	})
	if err != nil {
		t.Fatalf("dial SSH: %v", err)
	}
	defer client.Close()

	mgr := tunnel.NewManager()

	tun1, err := mgr.Open(context.Background(), client.SSHClient(), sshHost, tunnel.Forward{
		LocalPort:  0,
		RemoteHost: echo1Host,
		RemotePort: echo1Port,
	})
	if err != nil {
		t.Fatalf("Open tunnel 1: %v", err)
	}

	tun2, err := mgr.Open(context.Background(), client.SSHClient(), sshHost, tunnel.Forward{
		LocalPort:  0,
		RemoteHost: echo2Host,
		RemotePort: echo2Port,
	})
	if err != nil {
		t.Fatalf("Open tunnel 2: %v", err)
	}

	if len(mgr.Tunnels()) != 2 {
		t.Fatalf("expected 2 tunnels, got %d", len(mgr.Tunnels()))
	}

	// Close all tunnels via the manager.
	if err := mgr.Close(); err != nil {
		t.Fatalf("Manager.Close: %v", err)
	}

	// Verify the tunnels list is empty.
	if len(mgr.Tunnels()) != 0 {
		t.Errorf("expected 0 tunnels after Close, got %d", len(mgr.Tunnels()))
	}

	// Verify that connecting to the closed tunnel listeners fails.
	if _, err := net.Dial("tcp", tun1.LocalAddr); err == nil {
		t.Error("expected error dialing closed tunnel 1")
	}
	if _, err := net.Dial("tcp", tun2.LocalAddr); err == nil {
		t.Error("expected error dialing closed tunnel 2")
	}
}
