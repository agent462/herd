package dashboard

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/agent462/herd/internal/ssh"
)

// healthTickCmd returns a tea.Cmd that fires a healthTickMsg after the given interval.
func healthTickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(time.Time) tea.Msg {
		return healthTickMsg{}
	})
}

// healthCheckCmd spawns a goroutine that checks pool connectivity for all hosts.
func healthCheckCmd(pool *ssh.Pool, hosts []string) tea.Cmd {
	return func() tea.Msg {
		status := make(map[string]bool, len(hosts))
		for _, h := range hosts {
			status[h] = pool.IsConnected(h)
		}
		return healthCheckMsg{Status: status}
	}
}
