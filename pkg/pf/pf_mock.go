//go:build !freebsd

package pf

import (
	"log"
	"net"
	"sync"
)

// MockDevice provides an in-memory simulation of /dev/pf for non-FreeBSD platforms.
type MockDevice struct {
	mu     sync.Mutex
	tables map[string]map[string]struct{}
	closed bool
}

// Open creates a new mock PF device.
func Open(devPath string) (Device, error) {
	log.Printf("[pf-mock] Initialized mock PF device (platform is not FreeBSD)")
	return &MockDevice{
		tables: make(map[string]map[string]struct{}),
	}, nil
}

func (m *MockDevice) AddAddresses(table string, ips []net.IP) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return 0, ErrDeviceClosed
	}

	tbl, ok := m.tables[table]
	if !ok {
		tbl = make(map[string]struct{})
		m.tables[table] = tbl
	}

	added := 0
	for _, ip := range ips {
		s := ip.String()
		if _, exists := tbl[s]; !exists {
			tbl[s] = struct{}{}
			added++
		}
	}

	log.Printf("[pf-mock] Table <%s>: added %d address(es), total now: %d", table, added, len(tbl))
	return added, nil
}

func (m *MockDevice) DelAddresses(table string, ips []net.IP) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return 0, ErrDeviceClosed
	}

	tbl, ok := m.tables[table]
	if !ok {
		return 0, nil
	}

	deleted := 0
	for _, ip := range ips {
		s := ip.String()
		if _, exists := tbl[s]; exists {
			delete(tbl, s)
			deleted++
		}
	}

	log.Printf("[pf-mock] Table <%s>: deleted %d address(es), remaining: %d", table, deleted, len(tbl))
	return deleted, nil
}

func (m *MockDevice) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	log.Printf("[pf-mock] Mock PF device closed")
	return nil
}
