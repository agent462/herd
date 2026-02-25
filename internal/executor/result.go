package executor

import "time"

// HostResult holds the result of executing a command on a single host.
type HostResult struct {
	Host     string
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Duration time.Duration
	Err      error // connection/timeout errors
}
