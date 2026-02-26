package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
)

func TestIsReconnectable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context canceled", context.Canceled, false},
		{"context deadline", context.DeadlineExceeded, false},
		{"wrapped context canceled", fmt.Errorf("run: %w", context.Canceled), false},
		{"EOF", io.EOF, true},
		{"unexpected EOF", io.ErrUnexpectedEOF, true},
		{"wrapped EOF", fmt.Errorf("session: %w", io.EOF), true},
		{"net.OpError", &net.OpError{Op: "read", Err: errors.New("reset")}, true},
		{"closed connection", errors.New("use of closed network connection"), true},
		{"connection reset", errors.New("connection reset by peer"), true},
		{"broken pipe", errors.New("write: broken pipe"), true},
		{"auth failure", errors.New("ssh: handshake failed: ssh: unable to authenticate"), false},
		{"generic error", errors.New("something went wrong"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isReconnectable(tc.err)
			if got != tc.want {
				t.Errorf("isReconnectable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
