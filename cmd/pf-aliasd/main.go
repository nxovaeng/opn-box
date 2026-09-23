package main

import (
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"opn-box/pkg/pf"
	"opn-box/pkg/protocol"
	"opn-box/pkg/ttlcleaner"
)

var (
	socketPath = flag.String("socket", "/var/run/pf-aliasd.sock", "Path to Unix SOCK_SEQPACKET socket")
	pfDevPath  = flag.String("dev", "/dev/pf", "Path to PF character device")
	gcInterval = flag.Duration("gc", 2*time.Second, "Interval for TTL expiration GC sweeps")
	gcBatch    = flag.Int("batch", 256, "Max expired IPs to evict in a single GC batch")
	socketMode = flag.String("mode", "0660", "Octal file mode permissions for Unix socket")
)

type Daemon struct {
	pfDev    pf.Device
	tracker  *ttlcleaner.Tracker
	listener net.Listener
	wg       sync.WaitGroup
	quit     chan struct{}
}

func main() {
	flag.Parse()

	log.Println("[pf-aliasd] Starting Packet Filter Alias Daemon...")

	// 1. Open /dev/pf and hold open for reuse
	dev, err := pf.Open(*pfDevPath)
	if err != nil {
		log.Fatalf("[pf-aliasd] Failed to open PF device: %v", err)
	}
	defer dev.Close()

	// 2. Prepare Unix Domain Socket
	_ = os.Remove(*socketPath) // Clean up stale socket file if any
	if err := os.MkdirAll(filepath.Dir(*socketPath), 0755); err != nil {
		log.Fatalf("[pf-aliasd] Failed to create socket directory: %v", err)
	}

	listener, err := net.Listen("unixpacket", *socketPath)
	if err != nil {
		log.Fatalf("[pf-aliasd] Failed to listen on unixpacket %s: %v", *socketPath, err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(*socketPath)
	}()

	// Set permissions on socket file
	if mode, err := strconv.ParseUint(*socketMode, 8, 32); err == nil {
		_ = os.Chmod(*socketPath, os.FileMode(mode))
	}

	log.Printf("[pf-aliasd] Listening on %s (SEQPACKET, mode %s)", *socketPath, *socketMode)

	d := &Daemon{
		pfDev:    dev,
		tracker:  ttlcleaner.NewTracker(),
		listener: listener,
		quit:     make(chan struct{}),
	}

	// 3. Start background GC loop for TTL expiration
	d.wg.Add(1)
	go d.runGCLoop(*gcInterval, *gcBatch)

	// 4. Start connection accepting loop
	d.wg.Add(1)
	go d.acceptLoop()

	// 5. Signal handling for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	log.Printf("[pf-aliasd] Received signal %v, shutting down gracefully...", sig)

	close(d.quit)
	_ = listener.Close()
	d.wg.Wait()

	log.Println("[pf-aliasd] Daemon exited cleanly.")
}

func (d *Daemon) acceptLoop() {
	defer d.wg.Done()

	for {
		conn, err := d.listener.Accept()
		if err != nil {
			select {
			case <-d.quit:
				return
			default:
				log.Printf("[pf-aliasd] Accept error: %v", err)
				continue
			}
		}

		go d.handleConnection(conn)
	}
}

func (d *Daemon) handleConnection(conn net.Conn) {
	defer conn.Close()

	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			if err != io.EOF {
				select {
				case <-d.quit:
					return
				default:
				}
			}
			return
		}

		msg, err := protocol.Decode(buf[:n])
		if err != nil {
			log.Printf("[pf-aliasd] Decode message error: %v", err)
			continue
		}

		d.dispatchMessage(conn, msg)
	}
}

func (d *Daemon) dispatchMessage(conn net.Conn, msg *protocol.Message) {
	switch msg.Header.Command {
	case protocol.CmdAdd:
		ttl := time.Duration(msg.TTL) * time.Second
		var newIPs []net.IP

		// Check and update TTL in memory tracker
		for _, ip := range msg.IPs {
			isNew := d.tracker.AddOrRenew(msg.Table, ip, ttl)
			if isNew {
				newIPs = append(newIPs, ip)
			}
		}

		// Inject new IPs into /dev/pf via batch ioctl
		var (
			status uint8 = protocol.StatusOK
			errMsg string
		)

		if len(newIPs) > 0 {
			nadd, err := d.pfDev.AddAddresses(msg.Table, newIPs)
			if err != nil {
				log.Printf("[pf-aliasd] Error adding addresses to table <%s>: %v", msg.Table, err)
				status = protocol.StatusErr
				errMsg = err.Error()
			} else {
				log.Printf("[pf-aliasd] Table <%s>: added %d new IP(s) (batch requested %d, ttl %s)",
					msg.Table, nadd, len(newIPs), ttl)
			}
		}

		// Send ACK
		ack := protocol.NewAckMessage(msg.Header.Seq, status, errMsg)
		if data, err := ack.Encode(); err == nil {
			_, _ = conn.Write(data)
		}

	case protocol.CmdDel:
		for _, ip := range msg.IPs {
			d.tracker.Delete(msg.Table, ip)
		}

		ndel, err := d.pfDev.DelAddresses(msg.Table, msg.IPs)
		status := protocol.StatusOK
		var errMsg string
		if err != nil {
			status = protocol.StatusErr
			errMsg = err.Error()
		} else {
			log.Printf("[pf-aliasd] Table <%s>: explicitly deleted %d IP(s)", msg.Table, ndel)
		}

		ack := protocol.NewAckMessage(msg.Header.Seq, status, errMsg)
		if data, err := ack.Encode(); err == nil {
			_, _ = conn.Write(data)
		}

	case protocol.CmdFlush:
		ips := d.tracker.Flush(msg.Table)
		if len(ips) > 0 {
			_, _ = d.pfDev.DelAddresses(msg.Table, ips)
			log.Printf("[pf-aliasd] Table <%s>: flushed %d IP(s)", msg.Table, len(ips))
		}

		ack := protocol.NewAckMessage(msg.Header.Seq, protocol.StatusOK, "")
		if data, err := ack.Encode(); err == nil {
			_, _ = conn.Write(data)
		}
	}
}

// runGCLoop periodically sweeps expired IPs from the tracker and deletes them from PF
func (d *Daemon) runGCLoop(interval time.Duration, maxBatch int) {
	defer d.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-d.quit:
			return
		case now := <-ticker.C:
			expiredMap := d.tracker.PopExpired(now, maxBatch)
			if len(expiredMap) == 0 {
				continue
			}

			// Perform batch deletions per table in PF
			for table, ips := range expiredMap {
				ndel, err := d.pfDev.DelAddresses(table, ips)
				if err != nil {
					log.Printf("[pf-aliasd][GC] Error deleting %d expired IP(s) from table <%s>: %v",
						len(ips), table, err)
				} else {
					log.Printf("[pf-aliasd][GC] Successfully evicted %d expired IP(s) from table <%s>",
						ndel, table)
				}
			}

			stats := d.tracker.Stats()
			log.Printf("[pf-aliasd][GC] Stats: Active=%d, TotalAdded=%d, Renewed=%d, Expired=%d",
				stats.ActiveEntries, stats.TotalAdded, stats.TotalRenewed, stats.TotalExpired)
		}
	}
}
