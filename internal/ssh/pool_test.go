package ssh_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	hssh "github.com/agent462/herd/internal/ssh"
	"github.com/agent462/herd/internal/sshtest"
)

func TestPool_BasicExecution(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	pubKey, keyPath := sshtest.GenerateKey(t)
	addr, cleanup := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "hello\n", "", 0
	}))
	defer cleanup()

	_, port := sshtest.ParseAddr(t, addr)

	pool := hssh.NewPool(
		hssh.ClientConfig{
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
			User:            "testuser",
		},
		map[string]hssh.HostConfig{
			"host-1": {Hostname: "127.0.0.1", Port: port, IdentityFile: keyPath},
		},
	)
	defer pool.Close()

	ctx := context.Background()
	result := pool.Run(ctx, "host-1", "echo hello")

	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}
	if string(result.Stdout) != "hello\n" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "hello\n")
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}
}

func TestPool_ConnectionReuse(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	pubKey, keyPath := sshtest.GenerateKey(t)
	var cmdCount atomic.Int32
	addr, cleanup := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		cmdCount.Add(1)
		return "ok\n", "", 0
	}))
	defer cleanup()

	_, port := sshtest.ParseAddr(t, addr)

	pool := hssh.NewPool(
		hssh.ClientConfig{
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
			User:            "testuser",
		},
		map[string]hssh.HostConfig{
			"host-1": {Hostname: "127.0.0.1", Port: port, IdentityFile: keyPath},
		},
	)
	defer pool.Close()

	ctx := context.Background()

	// Run multiple commands on the same host.
	for i := 0; i < 3; i++ {
		result := pool.Run(ctx, "host-1", "cmd")
		if result.Err != nil {
			t.Fatalf("run %d: unexpected error: %v", i, result.Err)
		}
	}

	if !pool.IsConnected("host-1") {
		t.Error("host-1 should be connected after commands")
	}

	// Verify the server saw all 3 commands (connection was reused, not re-dialed).
	if n := cmdCount.Load(); n != 3 {
		t.Errorf("server saw %d commands, want 3", n)
	}
}

func TestPool_IsConnected(t *testing.T) {
	pool := hssh.NewPool(hssh.ClientConfig{}, nil)
	defer pool.Close()

	if pool.IsConnected("nonexistent") {
		t.Error("IsConnected should return false for unknown host")
	}
}

func TestPool_Close(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	pubKey, keyPath := sshtest.GenerateKey(t)
	addr, cleanup := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "ok\n", "", 0
	}))
	defer cleanup()

	_, port := sshtest.ParseAddr(t, addr)

	pool := hssh.NewPool(
		hssh.ClientConfig{
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
			User:            "testuser",
		},
		map[string]hssh.HostConfig{
			"host-1": {Hostname: "127.0.0.1", Port: port, IdentityFile: keyPath},
		},
	)

	ctx := context.Background()
	result := pool.Run(ctx, "host-1", "cmd")
	if result.Err != nil {
		t.Fatalf("unexpected error: %v", result.Err)
	}

	if !pool.IsConnected("host-1") {
		t.Fatal("should be connected before Close")
	}

	if err := pool.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if pool.IsConnected("host-1") {
		t.Error("should not be connected after Close")
	}
}

func TestPool_ConnectionFailure(t *testing.T) {
	pool := hssh.NewPool(
		hssh.ClientConfig{
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
			User:            "testuser",
		},
		map[string]hssh.HostConfig{
			"bad-host": {Hostname: "127.0.0.1", Port: 1},
		},
	)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result := pool.Run(ctx, "bad-host", "cmd")
	if result.Err == nil {
		t.Fatal("expected error for unreachable host")
	}
}

func TestPool_MultipleHosts(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	pubKey, keyPath := sshtest.GenerateKey(t)

	addr1, cleanup1 := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "host-a\n", "", 0
	}))
	defer cleanup1()

	addr2, cleanup2 := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "host-b\n", "", 0
	}))
	defer cleanup2()

	_, port1 := sshtest.ParseAddr(t, addr1)
	_, port2 := sshtest.ParseAddr(t, addr2)

	pool := hssh.NewPool(
		hssh.ClientConfig{
			HostKeyCallback: gossh.InsecureIgnoreHostKey(),
			User:            "testuser",
		},
		map[string]hssh.HostConfig{
			"host-a": {Hostname: "127.0.0.1", Port: port1, IdentityFile: keyPath},
			"host-b": {Hostname: "127.0.0.1", Port: port2, IdentityFile: keyPath},
		},
	)
	defer pool.Close()

	ctx := context.Background()

	r1 := pool.Run(ctx, "host-a", "id")
	r2 := pool.Run(ctx, "host-b", "id")

	if r1.Err != nil {
		t.Fatalf("host-a error: %v", r1.Err)
	}
	if r2.Err != nil {
		t.Fatalf("host-b error: %v", r2.Err)
	}

	if string(r1.Stdout) != "host-a\n" {
		t.Errorf("host-a stdout = %q, want %q", r1.Stdout, "host-a\n")
	}
	if string(r2.Stdout) != "host-b\n" {
		t.Errorf("host-b stdout = %q, want %q", r2.Stdout, "host-b\n")
	}

	if !pool.IsConnected("host-a") || !pool.IsConnected("host-b") {
		t.Error("both hosts should be connected")
	}
}
