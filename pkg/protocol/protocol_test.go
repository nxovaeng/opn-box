package protocol

import (
	"net"
	"testing"
)

func TestEncodeDecodeAdd(t *testing.T) {
	ips := []net.IP{
		net.ParseIP("1.1.1.1").To4(),
		net.ParseIP("2606:4700:4700::1111"),
	}

	orig := NewAddMessage(1234, "gfw_proxy", 300, ips)
	data, err := orig.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decoded.Header.Command != CmdAdd {
		t.Errorf("Expected CmdAdd, got %d", decoded.Header.Command)
	}
	if decoded.Header.Seq != 1234 {
		t.Errorf("Expected Seq 1234, got %d", decoded.Header.Seq)
	}
	if decoded.Table != "gfw_proxy" {
		t.Errorf("Expected table 'gfw_proxy', got '%s'", decoded.Table)
	}
	if decoded.TTL != 300 {
		t.Errorf("Expected TTL 300, got %d", decoded.TTL)
	}
	if len(decoded.IPs) != 2 {
		t.Fatalf("Expected 2 IPs, got %d", len(decoded.IPs))
	}
	if !decoded.IPs[0].Equal(ips[0]) {
		t.Errorf("IP 0 mismatch: got %v, expected %v", decoded.IPs[0], ips[0])
	}
	if !decoded.IPs[1].Equal(ips[1]) {
		t.Errorf("IP 1 mismatch: got %v, expected %v", decoded.IPs[1], ips[1])
	}
}

func TestEncodeDecodeAck(t *testing.T) {
	orig := NewAckMessage(999, StatusErr, "table does not exist")
	data, err := orig.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decoded.Header.Command != CmdAck {
		t.Errorf("Expected CmdAck, got %d", decoded.Header.Command)
	}
	if decoded.Header.Status != StatusErr {
		t.Errorf("Expected StatusErr, got %d", decoded.Header.Status)
	}
	if decoded.ErrMsg != "table does not exist" {
		t.Errorf("Expected 'table does not exist', got '%s'", decoded.ErrMsg)
	}
}

func TestInvalidPackets(t *testing.T) {
	// Too short
	_, err := Decode([]byte{1, 2, 3})
	if err == nil {
		t.Error("Expected error on short packet, got nil")
	}

	// Invalid magic
	data := make([]byte, 10)
	data[0] = 'X'
	data[1] = 'Y'
	data[2] = ProtocolVersion
	_, err = Decode(data)
	if err == nil {
		t.Error("Expected error on invalid magic, got nil")
	}
}
