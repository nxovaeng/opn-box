package pf

import (
	"errors"
	"net"
)

var (
	ErrDeviceClosed = errors.New("pf device closed")
	ErrInvalidTable = errors.New("invalid table name")
	ErrEmptyIPList  = errors.New("empty IP list")
)

// Device represents an interface for Packet Filter table operations.
type Device interface {
	// AddAddresses inserts a batch of IP addresses into the specified table.
	// Returns the number of newly added addresses.
	AddAddresses(table string, ips []net.IP) (int, error)

	// DelAddresses removes a batch of IP addresses from the specified table.
	// Returns the number of addresses removed.
	DelAddresses(table string, ips []net.IP) (int, error)

	// Close releases the /dev/pf file descriptor.
	Close() error
}
