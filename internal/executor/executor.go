package executor

import (
	"context"
	"sync"
	"time"
)

// Runner is the interface that the SSH layer implements to execute a command on a single host.
type Runner interface {
	Run(ctx context.Context, host string, command string) *HostResult
}

// Executor fans out command execution across multiple hosts with bounded concurrency.
type Executor struct {
	runner      Runner
	concurrency int
	timeout     time.Duration
}

// Option configures an Executor.
type Option func(*Executor)

// WithConcurrency sets the maximum number of parallel goroutines.
func WithConcurrency(n int) Option {
	return func(e *Executor) {
		if n > 0 {
			e.concurrency = n
		}
	}
}

// WithTimeout sets the per-host command timeout.
func WithTimeout(d time.Duration) Option {
	return func(e *Executor) {
		if d > 0 {
			e.timeout = d
		}
	}
}

// New creates an Executor with the given Runner and options.
func New(runner Runner, opts ...Option) *Executor {
	e := &Executor{
		runner:      runner,
		concurrency: 20,
		timeout:     30 * time.Second,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Execute runs command on all hosts in parallel, bounded by the concurrency limit.
// Results are returned in the same order as the input hosts slice.
func (e *Executor) Execute(ctx context.Context, hosts []string, command string) []*HostResult {
	results := make([]*HostResult, len(hosts))
	if len(hosts) == 0 {
		return results
	}

	sem := make(chan struct{}, e.concurrency)
	var wg sync.WaitGroup

	for i, host := range hosts {
		wg.Add(1)
		go func(idx int, h string) {
			defer wg.Done()

			// Acquire semaphore, respecting parent context cancellation.
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = &HostResult{
					Host: h,
					Err:  ctx.Err(),
				}
				return
			}

			// Create a per-host timeout context derived from the parent.
			hostCtx, cancel := context.WithTimeout(ctx, e.timeout)
			defer cancel()

			start := time.Now()
			result := e.runner.Run(hostCtx, h, command)
			result.Duration = time.Since(start)
			result.Host = h

			// If the per-host context timed out but the runner didn't set an error, record it.
			if hostCtx.Err() == context.DeadlineExceeded && result.Err == nil {
				result.Err = context.DeadlineExceeded
			}

			results[idx] = result
		}(i, host)
	}

	wg.Wait()
	return results
}
