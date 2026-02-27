package transfer

import (
	"context"
	"sync"
	"time"

	hssh "github.com/agent462/herd/internal/ssh"
)

// ClientProvider returns an SSH client for a given host.
// Implemented by both ssh.Pool and ssh.SSHRunner.
type ClientProvider interface {
	GetClient(ctx context.Context, host string) (*hssh.Client, error)
}

// ClientCloser is optionally implemented by ClientProviders whose GetClient
// returns one-shot connections that the caller must close (e.g. SSHRunner).
// If a ClientProvider does not implement ClientCloser, clients are not closed
// by the executor (appropriate for pooled connections).
type ClientCloser interface {
	CloseClient(client *hssh.Client) error
}

// TransferResult holds the outcome of a file transfer for a single host.
type TransferResult struct {
	Host      string
	BytesSent int64
	Duration  time.Duration
	Checksum  string
	Err       error
}

// Executor runs file transfers in parallel across multiple hosts.
type Executor struct {
	provider    ClientProvider
	concurrency int
	timeout     time.Duration
}

// Option configures an Executor.
type Option func(*Executor)

// WithConcurrency sets the maximum number of parallel transfers.
func WithConcurrency(n int) Option {
	return func(e *Executor) {
		if n > 0 {
			e.concurrency = n
		}
	}
}

// WithTimeout sets the per-host transfer timeout.
func WithTimeout(d time.Duration) Option {
	return func(e *Executor) {
		if d > 0 {
			e.timeout = d
		}
	}
}

// New creates a transfer Executor.
func New(provider ClientProvider, opts ...Option) *Executor {
	e := &Executor{
		provider:    provider,
		concurrency: 20,
		timeout:     5 * time.Minute,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Push uploads a local file to all hosts in parallel.
func (e *Executor) Push(ctx context.Context, hosts []string, localPath, remotePath string, progressFn ProgressFunc) []*TransferResult {
	results := make([]*TransferResult, len(hosts))
	sem := make(chan struct{}, e.concurrency)
	var wg sync.WaitGroup

	for i, host := range hosts {
		wg.Add(1)
		go func(idx int, h string) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = &TransferResult{Host: h, Err: ctx.Err()}
				return
			}

			hostCtx, cancel := context.WithTimeout(ctx, e.timeout)
			defer cancel()

			start := time.Now()
			result := &TransferResult{Host: h}

			client, err := e.provider.GetClient(hostCtx, h)
			if err != nil {
				result.Err = err
				result.Duration = time.Since(start)
				results[idx] = result
				return
			}
			if closer, ok := e.provider.(ClientCloser); ok {
				defer closer.CloseClient(client)
			}

			checksum, bytes, err := PushFile(hostCtx, client.SSHClient(), localPath, remotePath, h, progressFn)
			result.Checksum = checksum
			result.BytesSent = bytes
			result.Err = err
			result.Duration = time.Since(start)
			results[idx] = result
		}(i, host)
	}

	wg.Wait()
	return results
}

// Pull downloads a remote file from all hosts in parallel.
func (e *Executor) Pull(ctx context.Context, hosts []string, remotePath, localDir string, progressFn ProgressFunc) []*TransferResult {
	results := make([]*TransferResult, len(hosts))
	sem := make(chan struct{}, e.concurrency)
	var wg sync.WaitGroup

	for i, host := range hosts {
		wg.Add(1)
		go func(idx int, h string) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = &TransferResult{Host: h, Err: ctx.Err()}
				return
			}

			hostCtx, cancel := context.WithTimeout(ctx, e.timeout)
			defer cancel()

			start := time.Now()
			result := &TransferResult{Host: h}

			client, err := e.provider.GetClient(hostCtx, h)
			if err != nil {
				result.Err = err
				result.Duration = time.Since(start)
				results[idx] = result
				return
			}
			if closer, ok := e.provider.(ClientCloser); ok {
				defer closer.CloseClient(client)
			}

			checksum, bytes, err := PullFile(hostCtx, client.SSHClient(), remotePath, localDir, h, progressFn)
			result.Checksum = checksum
			result.BytesSent = bytes
			result.Err = err
			result.Duration = time.Since(start)
			results[idx] = result
		}(i, host)
	}

	wg.Wait()
	return results
}
