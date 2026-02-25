// Package sshtest provides an in-process SSH server for testing.
package sshtest

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

// CmdHandler processes a command and returns stdout, stderr, and exit code.
type CmdHandler func(cmd string) (stdout, stderr string, exitCode int)

// ServerConfig holds options for a test SSH server.
type ServerConfig struct {
	ClientPubKey ssh.PublicKey
	PasswordAuth string
	NoAuth       bool
	ForwardTCP   bool
	CmdHandler   CmdHandler
}

// Option configures a test SSH server.
type Option func(*ServerConfig)

// WithPublicKey configures the server to accept the given public key.
func WithPublicKey(pub ssh.PublicKey) Option {
	return func(c *ServerConfig) { c.ClientPubKey = pub }
}

// WithPassword configures the server to accept the given password.
func WithPassword(pw string) Option {
	return func(c *ServerConfig) { c.PasswordAuth = pw }
}

// WithNoAuth configures the server to accept any connection.
func WithNoAuth() Option {
	return func(c *ServerConfig) { c.NoAuth = true }
}

// WithCmdHandler sets the command handler.
func WithCmdHandler(h CmdHandler) Option {
	return func(c *ServerConfig) { c.CmdHandler = h }
}

// WithForwardTCP enables direct-tcpip forwarding.
func WithForwardTCP() Option {
	return func(c *ServerConfig) { c.ForwardTCP = true }
}

// Start launches an in-process SSH server. It returns the listener address
// and a cleanup function that shuts down the server.
func Start(t *testing.T, opts ...Option) (addr string, cleanup func()) {
	t.Helper()

	cfg := &ServerConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}

	serverConf := &ssh.ServerConfig{NoClientAuth: cfg.NoAuth}
	serverConf.AddHostKey(hostSigner)

	if cfg.ClientPubKey != nil {
		expected := cfg.ClientPubKey.Marshal()
		serverConf.PublicKeyCallback = func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) == string(expected) {
				return nil, nil
			}
			return nil, fmt.Errorf("unknown key")
		}
	}

	if cfg.PasswordAuth != "" {
		serverConf.PasswordCallback = func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if string(password) == cfg.PasswordAuth {
				return nil, nil
			}
			return nil, fmt.Errorf("wrong password")
		}
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleConnection(conn, serverConf, cfg)
		}
	}()

	return listener.Addr().String(), func() {
		listener.Close()
		<-done
	}
}

func handleConnection(conn net.Conn, config *ssh.ServerConfig, cfg *ServerConfig) {
	defer conn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go ssh.DiscardRequests(reqs)

	for newChan := range chans {
		switch newChan.ChannelType() {
		case "session":
			ch, requests, err := newChan.Accept()
			if err != nil {
				continue
			}
			go handleSession(ch, requests, cfg)
		case "direct-tcpip":
			if !cfg.ForwardTCP {
				newChan.Reject(ssh.Prohibited, "tcpip forwarding not enabled")
				continue
			}
			ch, _, err := newChan.Accept()
			if err != nil {
				continue
			}
			go handleDirectTCPIP(ch, newChan.ExtraData())
		default:
			newChan.Reject(ssh.UnknownChannelType, "unknown channel type")
		}
	}
}

func handleSession(ch ssh.Channel, reqs <-chan *ssh.Request, cfg *ServerConfig) {
	defer ch.Close()

	for req := range reqs {
		switch req.Type {
		case "exec":
			if len(req.Payload) < 4 {
				req.Reply(false, nil)
				continue
			}
			cmdLen := int(req.Payload[0])<<24 | int(req.Payload[1])<<16 | int(req.Payload[2])<<8 | int(req.Payload[3])
			if len(req.Payload) < 4+cmdLen {
				req.Reply(false, nil)
				continue
			}
			cmd := string(req.Payload[4 : 4+cmdLen])
			req.Reply(true, nil)

			exitCode := 0
			stdoutStr := ""
			stderrStr := ""

			if cfg.CmdHandler != nil {
				stdoutStr, stderrStr, exitCode = cfg.CmdHandler(cmd)
			} else {
				stdoutStr = cmd
			}

			if stdoutStr != "" {
				io.WriteString(ch, stdoutStr)
			}
			if stderrStr != "" {
				io.WriteString(ch.Stderr(), stderrStr)
			}

			exitPayload := []byte{
				byte(exitCode >> 24),
				byte(exitCode >> 16),
				byte(exitCode >> 8),
				byte(exitCode),
			}
			ch.SendRequest("exit-status", false, exitPayload)
			return

		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

func handleDirectTCPIP(ch ssh.Channel, extraData []byte) {
	defer ch.Close()

	if len(extraData) < 4 {
		return
	}
	hostLen := int(extraData[0])<<24 | int(extraData[1])<<16 | int(extraData[2])<<8 | int(extraData[3])
	if len(extraData) < 4+hostLen+4 {
		return
	}
	host := string(extraData[4 : 4+hostLen])
	off := 4 + hostLen
	port := int(extraData[off])<<24 | int(extraData[off+1])<<16 | int(extraData[off+2])<<8 | int(extraData[off+3])

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return
	}
	defer conn.Close()

	done := make(chan struct{}, 2)
	go func() { io.Copy(ch, conn); done <- struct{}{} }()
	go func() { io.Copy(conn, ch); done <- struct{}{} }()
	<-done
}

// GenerateKey creates an ed25519 key pair and writes the private key to a
// temp file. Returns the public key and the path to the private key file.
func GenerateKey(t *testing.T) (ssh.PublicKey, string) {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}

	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privBytes,
	})

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(keyPath, pemBlock, 0600); err != nil {
		t.Fatalf("write key file: %v", err)
	}

	return signer.PublicKey(), keyPath
}

// ParseAddr splits an address into host and port.
func ParseAddr(t *testing.T, addr string) (host string, port int) {
	t.Helper()
	h, portStr, _ := net.SplitHostPort(addr)
	var p int
	fmt.Sscanf(portStr, "%d", &p)
	return h, p
}
