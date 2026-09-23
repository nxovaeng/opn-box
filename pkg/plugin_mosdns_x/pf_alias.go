//go:build mosdns_x

package pf_alias

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
	"go.uber.org/zap"

	"github.com/pmkol/mosdns-x/coremain"
	"github.com/pmkol/mosdns-x/pkg/executable_seq"
	"github.com/pmkol/mosdns-x/pkg/query_context"

	"opn-box/pkg/protocol"
)

const PluginType = "pf_alias"

func init() {
	coremain.RegNewPluginFunc(PluginType, Init, func() interface{} { return new(Args) })
}

var _ coremain.ExecutablePlugin = (*PFAliasPlugin)(nil)

// Args configures the pf_alias plugin
type Args struct {
	SocketPath string `yaml:"socket_path"` // Unix domain socket path (default: /var/run/pf-aliasd.sock)
	Table      string `yaml:"table"`       // Target PF Table name (OPNsense External Alias)
	MinTTL     uint32 `yaml:"min_ttl"`     // Minimum clamped TTL in seconds (default: 60)
	MaxTTL     uint32 `yaml:"max_ttl"`     // Maximum clamped TTL in seconds (default: 86400)
	Timeout    string `yaml:"timeout"`     // Wait timeout for ACK (default: "100ms")
	Sync       bool   `yaml:"sync"`        // Wait synchronously for ACK (default: true)
}

// Init creates a new pf_alias plugin instance for mosdns-x
func Init(bp *coremain.BP, args interface{}) (coremain.Plugin, error) {
	cfg, ok := args.(*Args)
	if !ok {
		return nil, errors.New("invalid plugin configuration type")
	}
	return newPluginFromArgs(bp, cfg)
}

func newPluginFromArgs(bp *coremain.BP, cfg *Args) (*PFAliasPlugin, error) {
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
		BP:         bp,
		cfg:        cfg,
		timeout:    timeout,
		localCache: make(map[string]time.Time),
		connPool:   make(chan net.Conn, 16),
	}

	return p, nil
}

// PFAliasPlugin implements coremain.ExecutablePlugin
type PFAliasPlugin struct {
	*coremain.BP
	cfg     *Args
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
func (p *PFAliasPlugin) Exec(ctx context.Context, qCtx *query_context.Context, next executable_seq.ExecutableChainNode) error {
	resp := qCtx.R()
	if resp != nil && len(resp.Answer) > 0 {
		p.processResponse(resp)
	}

	return executable_seq.ExecChainNode(ctx, qCtx, next)
}

func (p *PFAliasPlugin) processResponse(resp *dns.Msg) {
	var (
		ips    []net.IP
		minTTL uint32 = 0xFFFFFFFF
		now           = time.Now()
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
		return
	}

	// Clamp TTL
	if minTTL < p.cfg.MinTTL {
		minTTL = p.cfg.MinTTL
	} else if minTTL > p.cfg.MaxTTL {
		minTTL = p.cfg.MaxTTL
	}
	ttlDuration := time.Duration(minTTL) * time.Second

	// Local Cache Check
	var uncachedIPs []net.IP
	p.cacheMu.RLock()
	for _, ip := range ips {
		s := ip.String()
		exp, ok := p.localCache[s]
		if !ok || exp.Sub(now) < (ttlDuration/5) {
			uncachedIPs = append(uncachedIPs, ip)
		}
	}
	p.cacheMu.RUnlock()

	if len(uncachedIPs) == 0 {
		return
	}

	// Send to pf-aliasd
	err := p.sendToAddDaemon(uncachedIPs, minTTL)
	if err != nil {
		p.L().Warn("failed to sync IPs to pf-aliasd",
			zap.Error(err),
			zap.Int("ip_count", len(uncachedIPs)),
		)
		return
	}

	// Update local cache
	p.cacheMu.Lock()
	newExp := now.Add(ttlDuration)
	for _, ip := range uncachedIPs {
		p.localCache[ip.String()] = newExp
	}
	if len(p.localCache) > 10000 {
		for k, v := range p.localCache {
			if v.Before(now) {
				delete(p.localCache, k)
			}
		}
	}
	p.cacheMu.Unlock()
}

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

	_ = conn.SetDeadline(time.Now().Add(p.timeout))

	if _, err := conn.Write(data); err != nil {
		p.putConn(conn, false)
		return fmt.Errorf("write packet error: %w", err)
	}

	if p.cfg.Sync {
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
