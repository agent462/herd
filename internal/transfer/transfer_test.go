package transfer_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	hssh "github.com/agent462/herd/internal/ssh"
	"github.com/agent462/herd/internal/sshtest"
	"github.com/agent462/herd/internal/transfer"
)

func dialTestServer(t *testing.T, addr, keyPath string) *hssh.Client {
	t.Helper()
	host, port := sshtest.ParseAddr(t, addr)
	client, err := hssh.Dial(context.Background(), host, hssh.ClientConfig{
		Port:               port,
		IdentityFiles:      []string{keyPath},
		AcceptUnknownHosts: true,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return client
}

func TestPushFile(t *testing.T) {
	sftpRoot := t.TempDir()
	pubKey, keyPath := sshtest.GenerateKey(t)

	addr, cleanup := sshtest.Start(t,
		sshtest.WithPublicKey(pubKey),
		sshtest.WithSFTP(sftpRoot),
	)
	defer cleanup()

	client := dialTestServer(t, addr, keyPath)
	defer client.Close()

	// Create a local file to push.
	localDir := t.TempDir()
	localPath := filepath.Join(localDir, "testfile.txt")
	content := []byte("hello world from transfer test\n")
	if err := os.WriteFile(localPath, content, 0644); err != nil {
		t.Fatalf("write local file: %v", err)
	}

	// Track progress.
	var progressCalls int
	progressFn := func(host string, transferred, total int64) {
		progressCalls++
	}

	// Use absolute path within the SFTP root for the remote path.
	remotePath := filepath.Join(sftpRoot, "testfile.txt")
	checksum, bytesWritten, err := transfer.PushFile(
		context.Background(),
		client.SSHClient(),
		localPath,
		remotePath,
		"testhost",
		progressFn,
	)
	if err != nil {
		t.Fatalf("PushFile: %v", err)
	}

	if bytesWritten != int64(len(content)) {
		t.Errorf("bytes written = %d, want %d", bytesWritten, len(content))
	}

	if checksum == "" {
		t.Error("checksum is empty")
	}

	// Verify file was written.
	data, err := os.ReadFile(remotePath)
	if err != nil {
		t.Fatalf("read remote file: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("remote content = %q, want %q", string(data), string(content))
	}

	if progressCalls == 0 {
		t.Error("progress callback was never called")
	}
}

func TestPullFile(t *testing.T) {
	sftpRoot := t.TempDir()
	pubKey, keyPath := sshtest.GenerateKey(t)

	// Write a file to the SFTP root for pulling.
	content := []byte("remote file content for pull test\n")
	remotePath := filepath.Join(sftpRoot, "remote.txt")
	if err := os.WriteFile(remotePath, content, 0644); err != nil {
		t.Fatalf("write remote file: %v", err)
	}

	addr, cleanup := sshtest.Start(t,
		sshtest.WithPublicKey(pubKey),
		sshtest.WithSFTP(sftpRoot),
	)
	defer cleanup()

	client := dialTestServer(t, addr, keyPath)
	defer client.Close()

	localDir := t.TempDir()
	var progressCalls int
	progressFn := func(host string, transferred, total int64) {
		progressCalls++
	}

	checksum, bytesWritten, err := transfer.PullFile(
		context.Background(),
		client.SSHClient(),
		remotePath,
		localDir,
		"testhost",
		progressFn,
	)
	if err != nil {
		t.Fatalf("PullFile: %v", err)
	}

	if bytesWritten != int64(len(content)) {
		t.Errorf("bytes written = %d, want %d", bytesWritten, len(content))
	}

	if checksum == "" {
		t.Error("checksum is empty")
	}

	// Verify file was downloaded.
	localPath := filepath.Join(localDir, "testhost", "remote.txt")
	data, err := os.ReadFile(localPath)
	if err != nil {
		t.Fatalf("read local file: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("local content = %q, want %q", string(data), string(content))
	}

	if progressCalls == 0 {
		t.Error("progress callback was never called")
	}
}

func TestProgressWriter(t *testing.T) {
	var calls []int64
	fn := func(host string, transferred, total int64) {
		calls = append(calls, transferred)
	}

	var buf strings.Builder
	pw := transfer.NewProgressWriterForTest(&buf, "host1", 100, fn)

	pw.Write([]byte("hello"))
	pw.Write([]byte(" world"))

	if buf.String() != "hello world" {
		t.Errorf("written = %q, want %q", buf.String(), "hello world")
	}

	if len(calls) != 2 {
		t.Fatalf("progress calls = %d, want 2", len(calls))
	}
	if calls[0] != 5 {
		t.Errorf("first call = %d, want 5", calls[0])
	}
	if calls[1] != 11 {
		t.Errorf("second call = %d, want 11", calls[1])
	}
}
