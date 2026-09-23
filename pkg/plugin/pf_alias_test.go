package plugin

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/IrineSistiana/mosdns/v5/pkg/query_context"
	"github.com/miekg/dns"
	"go.uber.org/zap"

	"opn-box/pkg/protocol"
)

func TestPFAliasPluginExec(t *testing.T) {
	// Create temporary socket path
	tempDir, err := os.MkdirTemp("", "pf_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)
	sockPath := filepath.Join(tempDir, "test.sock")

	// Start a mock SOCK_SEQPACKET server
	listener, err := net.Listen("unixpacket", sockPath)
	if err != nil {
		t.Fatalf("Failed to listen on unixpacket: %v", err)
	}
	defer listener.Close()

	received := make(chan *protocol.Message, 1)

	// Background worker handling incoming socket connections
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}

		msg, err := protocol.Decode(buf[:n])
		if err == nil {
			received <- msg
			// Send ACK back
			ack := protocol.NewAckMessage(msg.Header.Seq, protocol.StatusOK, "")
			ackData, _ := ack.Encode()
			_, _ = conn.Write(ackData)
		}
	}()

	// Initialize plugin
	cfg := &Args{
		Table:      "gfw_proxy",
		SocketPath: sockPath,
		MinTTL:     30,
		MaxTTL:     3600,
		Timeout:    "500ms",
		Sync:       true,
	}

	p, err := newPluginFromArgs(cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("Failed to init plugin: %v", err)
	}
	defer p.Close()

	// Build a mock DNS query context with A and AAAA answers
	req := new(dns.Msg)
	req.SetQuestion("example.com.", dns.TypeA)
	qCtx := query_context.NewContext(req)

	resp := new(dns.Msg)
	resp.SetReply(req)
	resp.Answer = []dns.RR{
		&dns.A{
			Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 120},
			A:   net.ParseIP("93.184.216.34").To4(),
		},
		&dns.AAAA{
			Hdr:  dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 300},
			AAAA: net.ParseIP("2606:2800:220:1:248:1893:25c8:1946"),
		},
	}
	qCtx.SetResponse(resp)

	// Execute plugin
	err = p.Exec(context.Background(), qCtx)
	if err != nil {
		t.Fatalf("Plugin Exec failed: %v", err)
	}

	// Verify message received on the daemon side
	select {
	case msg := <-received:
		if msg.Table != "gfw_proxy" {
			t.Errorf("Expected table 'gfw_proxy', got '%s'", msg.Table)
		}
		if msg.TTL != 120 { // Min of 120 and 300
			t.Errorf("Expected min TTL 120, got %d", msg.TTL)
		}
		if len(msg.IPs) != 2 {
			t.Fatalf("Expected 2 IPs, got %d", len(msg.IPs))
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for message on mock socket")
	}

	// Test local deduplication cache: calling Exec again should NOT send another packet
	err = p.Exec(context.Background(), qCtx)
	if err != nil {
		t.Fatalf("Second Exec failed: %v", err)
	}

	select {
	case <-received:
		t.Error("Expected no new message due to local cache deduplication")
	case <-time.After(100 * time.Millisecond):
		// Success: cached
	}
}
