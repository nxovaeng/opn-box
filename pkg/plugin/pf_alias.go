package plugin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/IrineSistiana/mosdns/v5/coremain"
	"github.com/IrineSistiana/mosdns/v5/pkg/query_context"
	"github.com/IrineSistiana/mosdns/v5/plugin/executable/sequence"
	"github.com/miekg/dns"
	"go.uber.org/zap"

	"opn-box/pkg/protocol"
)

const PluginType = "pf_alias"

func init() {
	coremain.RegNewPluginFunc(PluginType, NewPlugin, func() any { return new(Args) })
	sequence.MustRegExecQuickSetup(PluginType, QuickSetup)
}

// Args configures the pf_alias plugin
type Args struct {
	SocketPath string `json:"socket_path"` // Unix domain socket path (default: /var/run/pf-aliasd.sock)
	Table      string `json:"table"`       // Target PF Table name (OPNsense External Alias)
	MinTTL     uint32 `json:"min_ttl"`     // Minimum clamped TTL in seconds (default: 60)
	MaxTTL     uint32 `json:"max_ttl"`     // Maximum clamped TTL in seconds (default: 86400)
	Timeout    string `json:"timeout"`     // Wait timeout for ACK (default: "100ms")
	Sync       bool   `json:"sync"`        // Wait synchronously for ACK (default: true)
}

// QuickSetup enables shorthand configuration in MosDNS sequence (e.g. `exec: pf_alias gfw_proxy`)
func QuickSetup(_ sequence.BQ, s string) (any, error) {
	if len(s) == 0 {
		return nil, errors.New("pf_alias quick setup requires at least table name (e.g. 'pf_alias gfw_proxy')")
	}
	args := &Args{
		Table:      s,
		SocketPath: "/var/run/pf-aliasd.sock",
		MinTTL:     60,
		MaxTTL:     86400,
		Timeout:    "100ms",
		Sync:       true,
	}
	return newPluginFromArgs(args, zap.L())
}

// NewPlugin creates a new pf_alias plugin instance for MosDNS v5
func NewPlugin(bp *coremain.BP, args any) (any, error) {
	cfg, ok := args.(*Args)
	if !ok {
		return nil, errors.New("invalid plugin configuration type")
	}
	return newPluginFromArgs(cfg, bp.L())
}

func newPluginFromArgs(cfg *Args, logger *zap.Logger) (*PFAliasPlugin, error) {
	if cfg.Table == "" {
		return nil, errors.New("target table name cannot be empty")
	}
	if len(cfg.Table) > protocol.MaxTableNameLen {
		return nil, fmt.Errorf("table name exceeds %d characters", protocol.MaxTableNameLen)
	}
	if cfg.SocketPath == "" {
		cfg.SocketPath = "/var/run/pf-aliasd.sock"
	}
	if cfg.MinTTL == 0 {
		cfg.MinTTL = 60
	}
	if cfg.MaxTTL == 0 {
		cfg.MaxTTL = 86400
	}
	if cfg.MinTTL > cfg.MaxTTL {
		cfg.MinTTL = cfg.MaxTTL
	}

	timeout := 100 * time.Millisecond
	if cfg.Timeout != "" {
		if t, err := time.ParseDuration(cfg.Timeout); err == nil && t > 0 {
			timeout = t
		}
	}

	p := &PFAliasPlugin{
		cfg:        cfg,
		logger:     logger.Named("pf_alias").With(zap.String("table", cfg.Table)),
		timeout:    timeout,
		localCache: make(map[string]time.Time),
		connPool:   make(chan net.Conn, 16),
	}

	return p, nil
}

// PFAliasPlugin implements sequence.Executable
type PFAliasPlugin struct {
	cfg     *Args
	logger  *zap.Logger
	timeout time.Duration

	// Local cache to deduplicate frequent queries and avoid redundant IPC
	cacheMu    sync.RWMutex
	localCache map[string]time.Time

	// Connection pool for SOCK_SEQPACKET sockets
	connPool chan net.Conn
	seq      atomic.Uint32
	closed   atomic.Bool
}

// Exec processes the DNS response and transmits A/AAAA records to pf-aliasd
func (p *PFAliasPlugin) Exec(ctx context.Context, qCtx *query_context.Context) error {
	resp := qCtx.R()
	if resp == nil || len(resp.Answer) == 0 {
		return nil
	}

	// 1. Extract IPs and determine minimum TTL
	var (
		ips     []net.IP
		minTTL  uint32 = 0xFFFFFFFF
		now            = time.Now()
	)

	for _, rr := range resp.Answer {
		header := rr.Header()
		if header.Ttl < minTTL {
			minTTL = header.Ttl
		}

		switch v := rr.(type) {
		case *dns.A:
			if ip4 := v.A.To4(); ip4 != nil {
				ips = append(ips, ip4)
			}
		case *dns.AAAA:
			if ip6 := v.AAAA.To16(); ip6 != nil {
				ips = append(ips, ip6)
			}
		}
	}

	if len(ips) == 0 {
		return nil
	}

	// 2. Clamp TTL
	if minTTL < p.cfg.MinTTL {
		minTTL = p.cfg.MinTTL
	} else if minTTL > p.cfg.MaxTTL {
		minTTL = p.cfg.MaxTTL
	}
	ttlDuration := time.Duration(minTTL) * time.Second

	// 3. Local Cache Check
	// If all IPs are fresh (TTL remaining > 20%), skip socket dispatch
	var uncachedIPs []net.IP
	p.cacheMu.RLock()
	for _, ip := range ips {
		s := ip.String()
		exp, ok := p.localCache[s]
		// If not cached or close to expiration (last 20% of TTL window), dispatch to pf-aliasd for renewal
		if !ok || exp.Sub(now) < (ttlDuration/5) {
			uncachedIPs = append(uncachedIPs, ip)
		}
	}
	p.cacheMu.RUnlock()

	if len(uncachedIPs) == 0 {
		return nil // All resolved IPs are already active and fresh in the PF table
	}

	// 4. Send to pf-aliasd over SOCK_SEQPACKET
	err := p.sendToAddDaemon(uncachedIPs, minTTL)
	if err != nil {
		// Log warning but DO NOT fail the DNS query (fail-safe / fail-open)
		p.logger.Warn("failed to sync IPs to pf-aliasd",
			zap.Error(err),
			zap.Int("ip_count", len(uncachedIPs)),
		)
		return nil
	}

	// 5. Update local cache
	p.cacheMu.Lock()
	newExp := now.Add(ttlDuration)
	for _, ip := range uncachedIPs {
		p.localCache[ip.String()] = newExp
	}
	// Light garbage collection if local cache is large
	if len(p.localCache) > 10000 {
		for k, v := range p.localCache {
			if v.Before(now) {
				delete(p.localCache, k)
			}
		}
	}
	p.cacheMu.Unlock()

	return nil
}

// sendToAddDaemon acquires a connection and sends a CmdAdd message
func (p *PFAliasPlugin) sendToAddDaemon(ips []net.IP, ttl uint32) error {
	conn, err := p.getConn()
	if err != nil {
		return fmt.Errorf("connect to pf-aliasd failed: %w", err)
	}

	seq := p.seq.Add(1)
	msg := protocol.NewAddMessage(seq, p.cfg.Table, ttl, ips)
	data, err := msg.Encode()
	if err != nil {
		p.putConn(conn, false)
		return fmt.Errorf("encode message error: %w", err)
	}

	// Set deadline for atomic write and read
	_ = conn.SetDeadline(time.Now().Add(p.timeout))

	// SOCK_SEQPACKET write (atomic packet)
	if _, err := conn.Write(data); err != nil {
		p.putConn(conn, false)
		return fmt.Errorf("write packet error: %w", err)
	}

	if p.cfg.Sync {
		// Wait for ACK
		var ackBuf [256]byte
		n, err := conn.Read(ackBuf[:])
		if err != nil {
			p.putConn(conn, false)
			return fmt.Errorf("read ack error: %w", err)
		}

		ackMsg, err := protocol.Decode(ackBuf[:n])
		if err != nil {
			p.putConn(conn, false)
			return fmt.Errorf("decode ack error: %w", err)
		}

		if ackMsg.Header.Status != protocol.StatusOK {
			p.putConn(conn, true)
			return fmt.Errorf("daemon error (seq=%d): %s", ackMsg.Header.Seq, ackMsg.ErrMsg)
		}
	}

	// Reset deadline and return to pool
	_ = conn.SetDeadline(time.Time{})
	p.putConn(conn, true)
	return nil
}

func (p *PFAliasPlugin) getConn() (net.Conn, error) {
	if p.closed.Load() {
		return nil, errors.New("plugin is closed")
	}

	select {
	case conn := <-p.connPool:
		return conn, nil
	default:
		// unixpacket corresponds to AF_UNIX with SOCK_SEQPACKET
		return net.Dial("unixpacket", p.cfg.SocketPath)
	}
}

func (p *PFAliasPlugin) putConn(conn net.Conn, healthy bool) {
	if conn == nil {
		return
	}
	if !healthy || p.closed.Load() {
		_ = conn.Close()
		return
	}

	select {
	case p.connPool <- conn:
	default:
		_ = conn.Close()
	}
}

// Close releases pooled connections and resources
func (p *PFAliasPlugin) Close() error {
	p.closed.Store(true)
	close(p.connPool)
	for conn := range p.connPool {
		_ = conn.Close()
	}
	return nil
}
