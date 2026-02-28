package discover

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

// Host represents a discovered SSH host.
type Host struct {
	Address string // IP address
	Port    int    // SSH port (verified open)
}

// CIDRScan scans a CIDR range for hosts with an open TCP port.
// It skips network and broadcast addresses for IPv4 ranges.
// Concurrency limits the number of parallel TCP dials.
func CIDRScan(ctx context.Context, cidr string, port int, concurrency int, timeout time.Duration) ([]Host, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}

	ips := EnumerateHosts(network)
	if len(ips) == 0 {
		return nil, nil
	}

	var (
		mu      sync.Mutex
		results []Host
		wg      sync.WaitGroup
		sem     = make(chan struct{}, concurrency)
	)

	for _, ip := range ips {
		wg.Add(1)
		go func(addr net.IP) {
			defer wg.Done()

			// Acquire semaphore, respecting context cancellation.
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}

			// Check context again after acquiring semaphore.
			if ctx.Err() != nil {
				return
			}

			target := net.JoinHostPort(addr.String(), fmt.Sprintf("%d", port))
			conn, dialErr := net.DialTimeout("tcp", target, timeout)
			if dialErr != nil {
				return
			}
			conn.Close()

			mu.Lock()
			results = append(results, Host{Address: addr.String(), Port: port})
			mu.Unlock()
		}(ip)
	}

	wg.Wait()

	// Sort results by IP address.
	sort.Slice(results, func(i, j int) bool {
		ipA := net.ParseIP(results[i].Address).To4()
		ipB := net.ParseIP(results[j].Address).To4()
		if ipA != nil && ipB != nil {
			return binary.BigEndian.Uint32(ipA) < binary.BigEndian.Uint32(ipB)
		}
		return results[i].Address < results[j].Address
	})

	return results, nil
}

// EnumerateHosts returns all usable host IPs in the given network.
// For IPv4 networks larger than /31, it skips the network address
// (all host bits 0) and the broadcast address (all host bits 1).
func EnumerateHosts(network *net.IPNet) []net.IP {
	ip := network.IP.To4()
	if ip == nil {
		// IPv6 or invalid; not supported.
		return nil
	}

	mask := network.Mask
	ones, bits := mask.Size()
	if bits != 32 {
		return nil
	}

	// /32 is a single host.
	if ones == 32 {
		result := make(net.IP, 4)
		copy(result, ip)
		return []net.IP{result}
	}

	start := binary.BigEndian.Uint32(ip)
	hostBits := uint(bits - ones)
	size := uint32(1) << hostBits

	var hosts []net.IP

	// /31 is a point-to-point link: both addresses are usable (RFC 3021).
	if ones == 31 {
		for i := uint32(0); i < size; i++ {
			addr := make(net.IP, 4)
			binary.BigEndian.PutUint32(addr, start+i)
			hosts = append(hosts, addr)
		}
		return hosts
	}

	// For /30 and larger: skip network (first) and broadcast (last).
	for i := uint32(1); i < size-1; i++ {
		addr := make(net.IP, 4)
		binary.BigEndian.PutUint32(addr, start+i)
		hosts = append(hosts, addr)
	}

	return hosts
}
