package dashboard

import (
	"github.com/agent462/herd/internal/executor"
	"github.com/agent462/herd/internal/grouper"
)

// execResultMsg is sent when a command finishes executing across hosts.
type execResultMsg struct {
	Command string
	Results []*executor.HostResult
	Grouped *grouper.GroupedResults
}

// healthCheckMsg carries the connection status for each host.
type healthCheckMsg struct {
	Status map[string]bool
}

// healthTickMsg triggers a new health check cycle.
type healthTickMsg struct{}
