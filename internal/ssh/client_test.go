package ssh

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/bryanhitc/herd/internal/sshtest"
)

// dialTestClient creates a ClientConfig that won't use the local SSH agent
// or default key files — only the explicitly provided identity file.
func dialTestClient(t *testing.T, host string, port int, keyPath string) *Client {
	t.Helper()

	// Clear SSH_AUTH_SOCK so the agent auth method is skipped.
	t.Setenv("SSH_AUTH_SOCK", "")

	conf := ClientConfig{
		User:            "testuser",
		Port:            port,
		IdentityFiles:   []string{keyPath},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	}

	ctx := context.Background()
	client, err := Dial(ctx, host, conf)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return client
}

func TestSuccessfulConnectionAndCommand(t *testing.T) {
	pubKey, keyPath := sshtest.GenerateKey(t)

	addr, cleanup := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "hello world\n", "", 0
	}))
	defer cleanup()

	host, port := sshtest.ParseAddr(t, addr)
	client := dialTestClient(t, host, port, keyPath)
	defer client.Close()

	stdout, stderr, exitCode, err := client.RunCommand(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("run command: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if string(stdout) != "hello world\n" {
		t.Errorf("expected stdout 'hello world\\n', got %q", string(stdout))
	}
	if len(stderr) != 0 {
		t.Errorf("expected empty stderr, got %q", string(stderr))
	}
}

func TestAuthWithKeyFile(t *testing.T) {
	pubKey, keyPath := sshtest.GenerateKey(t)

	addr, cleanup := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "authenticated\n", "", 0
	}))
	defer cleanup()

	host, port := sshtest.ParseAddr(t, addr)
	client := dialTestClient(t, host, port, keyPath)
	defer client.Close()

	stdout, _, exitCode, err := client.RunCommand(context.Background(), "whoami")
	if err != nil {
		t.Fatalf("run command: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if string(stdout) != "authenticated\n" {
		t.Errorf("expected 'authenticated\\n', got %q", stdout)
	}
}

func TestCommandNonZeroExitCode(t *testing.T) {
	pubKey, keyPath := sshtest.GenerateKey(t)

	addr, cleanup := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "", "command not found\n", 127
	}))
	defer cleanup()

	host, port := sshtest.ParseAddr(t, addr)
	client := dialTestClient(t, host, port, keyPath)
	defer client.Close()

	stdout, stderr, exitCode, err := client.RunCommand(context.Background(), "badcmd")
	if err != nil {
		t.Fatalf("run command: %v", err)
	}
	if exitCode != 127 {
		t.Errorf("expected exit code 127, got %d", exitCode)
	}
	if len(stdout) != 0 {
		t.Errorf("expected empty stdout, got %q", stdout)
	}
	if string(stderr) != "command not found\n" {
		t.Errorf("expected 'command not found\\n', got %q", stderr)
	}
}

func TestConnectionTimeout(t *testing.T) {
	// Create a listener that accepts but never completes the SSH handshake.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 1)
				for {
					if _, err := c.Read(buf); err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	_, port := sshtest.ParseAddr(t, listener.Addr().String())

	t.Setenv("SSH_AUTH_SOCK", "")

	conf := ClientConfig{
		User:            "testuser",
		Port:            port,
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err = Dial(ctx, "127.0.0.1", conf)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("expected context deadline exceeded, got: %v", err)
	}
}

func TestResolveHostKeyCallback_MissingKnownHosts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	conf := ClientConfig{AcceptUnknownHosts: false}
	_, err := resolveHostKeyCallback(conf)
	if err == nil {
		t.Fatal("expected error when known_hosts is missing and AcceptUnknownHosts is false")
	}
	if !strings.Contains(err.Error(), "no known_hosts file") {
		t.Errorf("error should mention missing known_hosts, got: %v", err)
	}
}

func TestResolveHostKeyCallback_Insecure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	conf := ClientConfig{AcceptUnknownHosts: true}
	cb, err := resolveHostKeyCallback(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cb == nil {
		t.Fatal("expected non-nil callback")
	}
}

func TestResolveHostKeyCallback_ExplicitCallback(t *testing.T) {
	explicit := gossh.InsecureIgnoreHostKey()
	conf := ClientConfig{HostKeyCallback: explicit}
	cb, err := resolveHostKeyCallback(conf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cb == nil {
		t.Fatal("expected non-nil callback")
	}
}

func TestRunnerHostnameDialing(t *testing.T) {
	pubKey, keyPath := sshtest.GenerateKey(t)

	addr, cleanup := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "ok\n", "", 0
	}))
	defer cleanup()

	_, port := sshtest.ParseAddr(t, addr)
	t.Setenv("SSH_AUTH_SOCK", "")

	runner := NewRunner(
		ClientConfig{HostKeyCallback: gossh.InsecureIgnoreHostKey()},
		map[string]HostConfig{
			"admin@myhost": {
				Hostname:     "127.0.0.1",
				User:         "testuser",
				Port:         port,
				IdentityFile: keyPath,
			},
		},
	)

	result := runner.Run(context.Background(), "admin@myhost", "test")
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if result.Host != "admin@myhost" {
		t.Errorf("result.Host = %q, want admin@myhost", result.Host)
	}
	if string(result.Stdout) != "ok\n" {
		t.Errorf("result.Stdout = %q, want ok", result.Stdout)
	}
}

func TestRunnerSameHostDifferentUsers(t *testing.T) {
	pubKey, keyPath := sshtest.GenerateKey(t)

	addr1, cleanup1 := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "from-admin\n", "", 0
	}))
	defer cleanup1()

	addr2, cleanup2 := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "from-deploy\n", "", 0
	}))
	defer cleanup2()

	_, port1 := sshtest.ParseAddr(t, addr1)
	_, port2 := sshtest.ParseAddr(t, addr2)
	t.Setenv("SSH_AUTH_SOCK", "")

	runner := NewRunner(
		ClientConfig{HostKeyCallback: gossh.InsecureIgnoreHostKey()},
		map[string]HostConfig{
			"admin@server": {
				Hostname:     "127.0.0.1",
				User:         "admin",
				Port:         port1,
				IdentityFile: keyPath,
			},
			"deploy@server": {
				Hostname:     "127.0.0.1",
				User:         "deploy",
				Port:         port2,
				IdentityFile: keyPath,
			},
		},
	)

	r1 := runner.Run(context.Background(), "admin@server", "whoami")
	r2 := runner.Run(context.Background(), "deploy@server", "whoami")

	if r1.Err != nil {
		t.Fatalf("admin@server error: %v", r1.Err)
	}
	if r2.Err != nil {
		t.Fatalf("deploy@server error: %v", r2.Err)
	}
	if string(r1.Stdout) != "from-admin\n" {
		t.Errorf("admin result = %q, want from-admin", r1.Stdout)
	}
	if string(r2.Stdout) != "from-deploy\n" {
		t.Errorf("deploy result = %q, want from-deploy", r2.Stdout)
	}
}

func TestCommandWithStderrOutput(t *testing.T) {
	pubKey, keyPath := sshtest.GenerateKey(t)

	addr, cleanup := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "stdout output\n", "stderr warning\n", 0
	}))
	defer cleanup()

	host, port := sshtest.ParseAddr(t, addr)
	client := dialTestClient(t, host, port, keyPath)
	defer client.Close()

	stdout, stderr, exitCode, err := client.RunCommand(context.Background(), "mixedoutput")
	if err != nil {
		t.Fatalf("run command: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if string(stdout) != "stdout output\n" {
		t.Errorf("expected stdout 'stdout output\\n', got %q", stdout)
	}
	if string(stderr) != "stderr warning\n" {
		t.Errorf("expected stderr 'stderr warning\\n', got %q", stderr)
	}
}

func TestParseJumpHost(t *testing.T) {
	tests := []struct {
		spec     string
		wantUser string
		wantHost string
		wantPort int
	}{
		{"bastion", "", "bastion", 0},
		{"user@bastion", "user", "bastion", 0},
		{"bastion:2222", "", "bastion", 2222},
		{"user@bastion:2222", "user", "bastion", 2222},
		{"  user@host:22  ", "user", "host", 22},
	}

	for _, tc := range tests {
		t.Run(tc.spec, func(t *testing.T) {
			user, host, port := parseJumpHost(tc.spec)
			if user != tc.wantUser {
				t.Errorf("user = %q, want %q", user, tc.wantUser)
			}
			if host != tc.wantHost {
				t.Errorf("host = %q, want %q", host, tc.wantHost)
			}
			if port != tc.wantPort {
				t.Errorf("port = %d, want %d", port, tc.wantPort)
			}
		})
	}
}

func TestProxyJumpNone(t *testing.T) {
	pubKey, keyPath := sshtest.GenerateKey(t)

	addr, cleanup := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "direct\n", "", 0
	}))
	defer cleanup()

	host, port := sshtest.ParseAddr(t, addr)
	t.Setenv("SSH_AUTH_SOCK", "")

	conf := ClientConfig{
		User:            "testuser",
		Port:            port,
		IdentityFiles:   []string{keyPath},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		ProxyJump:       "none",
	}

	client, err := Dial(context.Background(), host, conf)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	stdout, _, _, err := client.RunCommand(context.Background(), "test")
	if err != nil {
		t.Fatalf("run command: %v", err)
	}
	if string(stdout) != "direct\n" {
		t.Errorf("expected 'direct\\n', got %q", stdout)
	}
}

func TestProxyJumpSingleHop(t *testing.T) {
	pubKey, keyPath := sshtest.GenerateKey(t)

	// Start a bastion server that supports TCP forwarding.
	bastionAddr, bastionCleanup := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithForwardTCP())
	defer bastionCleanup()

	// Start the target server.
	targetAddr, targetCleanup := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "from-target\n", "", 0
	}))
	defer targetCleanup()

	bastionHost, bastionPort := sshtest.ParseAddr(t, bastionAddr)
	targetHost, targetPort := sshtest.ParseAddr(t, targetAddr)
	t.Setenv("SSH_AUTH_SOCK", "")

	jumpSpec := fmt.Sprintf("testuser@%s:%d", bastionHost, bastionPort)

	conf := ClientConfig{
		User:            "testuser",
		Port:            targetPort,
		IdentityFiles:   []string{keyPath},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		ProxyJump:       jumpSpec,
	}

	client, err := Dial(context.Background(), targetHost, conf)
	if err != nil {
		t.Fatalf("dial via proxy: %v", err)
	}
	defer client.Close()

	stdout, _, exitCode, err := client.RunCommand(context.Background(), "hello")
	if err != nil {
		t.Fatalf("run command: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if string(stdout) != "from-target\n" {
		t.Errorf("expected 'from-target\\n', got %q", stdout)
	}

	// Verify jump clients are tracked for cleanup.
	if len(client.jumpClients) != 1 {
		t.Errorf("expected 1 jump client, got %d", len(client.jumpClients))
	}
}
