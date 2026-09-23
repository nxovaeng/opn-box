//go:build freebsd

package pf

import (
	"fmt"
	"net"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	// FreeBSD ioctl definitions for pf table manipulation
	// _IOWR('D', 67, struct pfioc_table) -> 0xc4504443 on 64-bit FreeBSD
	// _IOWR('D', 68, struct pfioc_table) -> 0xc4504444 on 64-bit FreeBSD
	DIOCRADDADDRS = 0xc4504443
	DIOCRDELADDRS = 0xc4504444

	PFR_TABLE_NAME_SIZE = 32
	MAXPATHLEN          = 1024

	AF_INET  = 2
	AF_INET6 = 28
)

// pfr_table matches FreeBSD's struct pfr_table
type pfrTable struct {
	anchor [MAXPATHLEN]byte
	name   [PFR_TABLE_NAME_SIZE]byte
	flags  uint32
	fback  uint8
	_pad   [3]byte
}

// pfr_addr matches FreeBSD's struct pfr_addr
type pfrAddr struct {
	addr  [16]byte // union of in_addr / in6_addr
	af    uint8
	net   uint8
	not   uint8
	fback uint8
	_pad  [4]byte
}

// pfioc_table matches FreeBSD's struct pfioc_table
type pfiocTable struct {
	table   pfrTable
	buffer  uintptr
	esize   int32
	size    int32
	size2   int32
	nadd    int32
	ndel    int32
	nchange int32
	flags   int32
	ticket  uint32
}

// BSDDevice represents a live /dev/pf character device on FreeBSD.
type BSDDevice struct {
	mu sync.Mutex
	fd int
}

// Open opens /dev/pf with O_RDWR. Requires root / wheel permissions.
func Open(devPath string) (Device, error) {
	if devPath == "" {
		devPath = "/dev/pf"
	}

	fd, err := unix.Open(devPath, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open pf device %s: %w", devPath, err)
	}

	return &BSDDevice{fd: fd}, nil
}

// AddAddresses adds a batch of IP addresses to the given table.
func (d *BSDDevice) AddAddresses(table string, ips []net.IP) (int, error) {
	if len(ips) == 0 {
		return 0, nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.fd < 0 {
		return 0, ErrDeviceClosed
	}

	addrs := make([]pfrAddr, len(ips))
	for i, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			copy(addrs[i].addr[:4], ip4)
			addrs[i].af = AF_INET
			addrs[i].net = 32
		} else if ip6 := ip.To16(); ip6 != nil {
			copy(addrs[i].addr[:], ip6)
			addrs[i].af = AF_INET6
			addrs[i].net = 128
		} else {
			return 0, fmt.Errorf("invalid IP: %v", ip)
		}
	}

	var io pfiocTable
	copy(io.table.name[:], table)
	io.buffer = uintptr(unsafe.Pointer(&addrs[0]))
	io.esize = int32(unsafe.Sizeof(addrs[0]))
	io.size = int32(len(addrs))

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(d.fd), uintptr(DIOCRADDADDRS), uintptr(unsafe.Pointer(&io)))
	if errno != 0 {
		return 0, fmt.Errorf("DIOCRADDADDRS ioctl failed: %w", errno)
	}

	return int(io.nadd), nil
}

// DelAddresses removes a batch of IP addresses from the given table.
func (d *BSDDevice) DelAddresses(table string, ips []net.IP) (int, error) {
	if len(ips) == 0 {
		return 0, nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.fd < 0 {
		return 0, ErrDeviceClosed
	}

	addrs := make([]pfrAddr, len(ips))
	for i, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			copy(addrs[i].addr[:4], ip4)
			addrs[i].af = AF_INET
			addrs[i].net = 32
		} else if ip6 := ip.To16(); ip6 != nil {
			copy(addrs[i].addr[:], ip6)
			addrs[i].af = AF_INET6
			addrs[i].net = 128
		} else {
			return 0, fmt.Errorf("invalid IP: %v", ip)
		}
	}

	var io pfiocTable
	copy(io.table.name[:], table)
	io.buffer = uintptr(unsafe.Pointer(&addrs[0]))
	io.esize = int32(unsafe.Sizeof(addrs[0]))
	io.size = int32(len(addrs))

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(d.fd), uintptr(DIOCRDELADDRS), uintptr(unsafe.Pointer(&io)))
	if errno != 0 {
		return 0, fmt.Errorf("DIOCRDELADDRS ioctl failed: %w", errno)
	}

	return int(io.ndel), nil
}

// Close closes the /dev/pf descriptor.
func (d *BSDDevice) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.fd >= 0 {
		err := unix.Close(d.fd)
		d.fd = -1
		return err
	}
	return nil
}
