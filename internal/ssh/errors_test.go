package ssh

import (
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestWrapConnectError_ConnectionRefused(t *testing.T) {
	err := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: fmt.Errorf("connection refused"),
	}
	wrapped := WrapConnectError("myhost", err)
	ce, ok := wrapped.(*ConnectError)
	if !ok {
		t.Fatalf("expected *ConnectError, got %T", wrapped)
	}
	if !strings.Contains(ce.Hint, "SSH daemon") {
		t.Errorf("hint = %q, want mention of SSH daemon", ce.Hint)
	}
}

func TestWrapConnectError_DNSFailure(t *testing.T) {
	err := &net.DNSError{
		Err:  "no such host",
		Name: "badhost",
	}
	wrapped := WrapConnectError("badhost", err)
	ce, ok := wrapped.(*ConnectError)
	if !ok {
		t.Fatalf("expected *ConnectError, got %T", wrapped)
	}
	if !strings.Contains(ce.Hint, "hostname") {
		t.Errorf("hint = %q, want mention of hostname", ce.Hint)
	}
}

func TestWrapConnectError_AuthFailure(t *testing.T) {
	err := fmt.Errorf("ssh: unable to authenticate")
	wrapped := WrapConnectError("myhost", err)
	ce, ok := wrapped.(*ConnectError)
	if !ok {
		t.Fatalf("expected *ConnectError, got %T", wrapped)
	}
	if !strings.Contains(ce.Hint, "SSH key") {
		t.Errorf("hint = %q, want mention of SSH key", ce.Hint)
	}
}

func TestWrapConnectError_KnownHostsMissing(t *testing.T) {
	err := fmt.Errorf("no known_hosts file found at /home/user/.ssh/known_hosts")
	wrapped := WrapConnectError("myhost", err)
	ce, ok := wrapped.(*ConnectError)
	if !ok {
		t.Fatalf("expected *ConnectError, got %T", wrapped)
	}
	if !strings.Contains(ce.Hint, "--insecure") {
		t.Errorf("hint = %q, want mention of --insecure", ce.Hint)
	}
}

func TestWrapConnectError_Nil(t *testing.T) {
	if err := WrapConnectError("host", nil); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestWrapConnectError_Unknown(t *testing.T) {
	err := fmt.Errorf("some random error")
	wrapped := WrapConnectError("host", err)
	if _, ok := wrapped.(*ConnectError); ok {
		t.Error("expected unwrapped error for unknown error type")
	}
}
