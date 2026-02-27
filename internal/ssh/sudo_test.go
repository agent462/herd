package ssh

import (
	"context"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"github.com/agent462/herd/internal/sshtest"
)

func TestStripSudoPrompt(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "no prompt",
			input:  "hello world\n",
			expect: "hello world\n",
		},
		{
			name:   "sudo prompt with username",
			input:  "[sudo] password for user:\nhello world\n",
			expect: "hello world\n",
		},
		{
			name:   "Password: prompt",
			input:  "Password:\nhello world\n",
			expect: "hello world\n",
		},
		{
			name:   "both prompt styles",
			input:  "[sudo] password for root:\nPassword:\ncommand output\n",
			expect: "command output\n",
		},
		{
			name:   "empty output after stripping",
			input:  "[sudo] password for user:\n",
			expect: "",
		},
		{
			name:   "prompt with leading whitespace",
			input:  "  [sudo] password for user:  \nhello\n",
			expect: "hello\n",
		},
		{
			name:   "multiline output preserves whitespace",
			input:  "[sudo] password for admin:\nline1\nline2\nline3\n",
			expect: "line1\nline2\nline3\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := stripSudoPrompt([]byte(tc.input))
			if string(result) != tc.expect {
				t.Errorf("stripSudoPrompt(%q) = %q, want %q", tc.input, string(result), tc.expect)
			}
		})
	}
}

func TestRunCommandWithSudo(t *testing.T) {
	pubKey, keyPath := sshtest.GenerateKey(t)

	addr, cleanup := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		// The command should be wrapped with sudo -S.
		if !strings.HasPrefix(cmd, "sudo -S ") {
			return "", "expected sudo -S prefix", 1
		}
		actualCmd := strings.TrimPrefix(cmd, "sudo -S ")
		return "[sudo] password for user:\n" + actualCmd + " output\n", "", 0
	}))
	defer cleanup()

	host, port := sshtest.ParseAddr(t, addr)

	t.Setenv("SSH_AUTH_SOCK", "")
	conf := ClientConfig{
		User:            "testuser",
		Port:            port,
		IdentityFiles:   []string{keyPath},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	}

	client, err := Dial(context.Background(), host, conf)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	stdout, stderr, exitCode, err := client.RunCommandWithSudo(context.Background(), "whoami", "testpass")
	if err != nil {
		t.Fatalf("RunCommandWithSudo: %v", err)
	}
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
	if string(stdout) != "whoami output\n" {
		t.Errorf("expected 'whoami output\\n', got %q", string(stdout))
	}
	if len(stderr) != 0 {
		t.Errorf("expected empty stderr (PTY merges streams), got %q", string(stderr))
	}
}

func TestRunCommandWithSudo_NonZeroExit(t *testing.T) {
	pubKey, keyPath := sshtest.GenerateKey(t)

	addr, cleanup := sshtest.Start(t, sshtest.WithPublicKey(pubKey), sshtest.WithCmdHandler(func(cmd string) (string, string, int) {
		return "[sudo] password for user:\npermission denied\n", "", 1
	}))
	defer cleanup()

	host, port := sshtest.ParseAddr(t, addr)

	t.Setenv("SSH_AUTH_SOCK", "")
	conf := ClientConfig{
		User:            "testuser",
		Port:            port,
		IdentityFiles:   []string{keyPath},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	}

	client, err := Dial(context.Background(), host, conf)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	stdout, _, exitCode, err := client.RunCommandWithSudo(context.Background(), "restricted-cmd", "testpass")
	if err != nil {
		t.Fatalf("RunCommandWithSudo: %v", err)
	}
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}
	if string(stdout) != "permission denied\n" {
		t.Errorf("expected 'permission denied\\n', got %q", string(stdout))
	}
}
