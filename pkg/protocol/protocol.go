package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

// Magic bytes identifying the pf-aliasd SEQPACKET protocol ("PF")
var (
	MagicBytes = [2]byte{'P', 'F'}
)

const (
	ProtocolVersion = 1

	// Commands
	CmdAdd   uint8 = 0x01 // Add IPs to table with TTL
	CmdDel   uint8 = 0x02 // Delete IPs from table
	CmdFlush uint8 = 0x03 // Flush all entries for table
	CmdAck   uint8 = 0x80 // Acknowledgment / Response

	// Status codes (in CmdAck)
	StatusOK       uint8 = 0x00
	StatusErr      uint8 = 0x01
	StatusNotFound uint8 = 0x02

	// Limits
	MaxTableNameLen = 31
	MaxIPsPerPacket = 256
)

var (
	ErrPacketTooShort = errors.New("packet too short")
	ErrInvalidMagic   = errors.New("invalid protocol magic")
	ErrInvalidVersion = errors.New("unsupported protocol version")
	ErrTableNameTooLong = errors.New("table name exceeds maximum length")
	ErrTooManyIPs     = errors.New("too many IPs in single packet")
	ErrInvalidIP      = errors.New("invalid IP address")
)

// Header represents the 10-byte fixed protocol header
// 0       1       2       3       4       5       6       7       8       9
// +-------+-------+-------+-------+-------+-------+-------+-------+-------+-------+
// | Magic ("PF")  | Ver   | Cmd   | Seq (uint32, BigEndian)       | Status| Rsvd  |
// +-------+-------+-------+-------+-------+-------+-------+-------+-------+-------+
type Header struct {
	Magic    [2]byte
	Version  uint8
	Command  uint8
	Seq      uint32
	Status   uint8
	Reserved uint8
}

const HeaderSize = 10

// Message represents an application payload transferred over SOCK_SEQPACKET
type Message struct {
	Header Header
	Table  string
	TTL    uint32 // in seconds
	IPs    []net.IP
	ErrMsg string // optional error message in ACK response
}

// Encode serializes a Message into binary for transmission.
func (m *Message) Encode() ([]byte, error) {
	if len(m.Table) > MaxTableNameLen {
		return nil, ErrTableNameTooLong
	}
	if len(m.IPs) > MaxIPsPerPacket {
		return nil, ErrTooManyIPs
	}

	buf := make([]byte, 0, 128)

	// 1. Fixed Header (10 bytes)
	buf = append(buf, MagicBytes[0], MagicBytes[1])
	buf = append(buf, ProtocolVersion)
	buf = append(buf, m.Header.Command)

	var seqBuf [4]byte
	binary.BigEndian.PutUint32(seqBuf[:], m.Header.Seq)
	buf = append(buf, seqBuf[:]...)

	buf = append(buf, m.Header.Status)
	buf = append(buf, 0) // Reserved

	// 2. Payload according to Command
	switch m.Header.Command {
	case CmdAdd, CmdDel:
		// TableName length (1 byte) + TableName
		buf = append(buf, uint8(len(m.Table)))
		buf = append(buf, []byte(m.Table)...)

		// TTL (4 bytes, BigEndian)
		var ttlBuf [4]byte
		binary.BigEndian.PutUint32(ttlBuf[:], m.TTL)
		buf = append(buf, ttlBuf[:]...)

		// IP Count (2 bytes, BigEndian)
		var countBuf [2]byte
		binary.BigEndian.PutUint16(countBuf[:], uint16(len(m.IPs)))
		buf = append(buf, countBuf[:]...)

		// IPs: [Family(1B: 4 or 6)][Bytes(4 or 16)]
		for _, ip := range m.IPs {
			if ip4 := ip.To4(); ip4 != nil {
				buf = append(buf, 4)
				buf = append(buf, ip4...)
			} else if ip6 := ip.To16(); ip6 != nil {
				buf = append(buf, 6)
				buf = append(buf, ip6...)
			} else {
				return nil, ErrInvalidIP
			}
		}

	case CmdFlush:
		buf = append(buf, uint8(len(m.Table)))
		buf = append(buf, []byte(m.Table)...)

	case CmdAck:
		// Optional error message: [Len (2B)][ErrMsg]
		errBytes := []byte(m.ErrMsg)
		var errLen [2]byte
		binary.BigEndian.PutUint16(errLen[:], uint16(len(errBytes)))
		buf = append(buf, errLen[:]...)
		buf = append(buf, errBytes...)

	default:
		return nil, fmt.Errorf("unknown command: 0x%x", m.Header.Command)
	}

	return buf, nil
}

// Decode deserializes binary bytes from a SOCK_SEQPACKET read into a Message.
func Decode(data []byte) (*Message, error) {
	if len(data) < HeaderSize {
		return nil, ErrPacketTooShort
	}

	// Verify Header
	if data[0] != MagicBytes[0] || data[1] != MagicBytes[1] {
		return nil, ErrInvalidMagic
	}
	ver := data[2]
	if ver != ProtocolVersion {
		return nil, fmt.Errorf("%w: got %d, expected %d", ErrInvalidVersion, ver, ProtocolVersion)
	}

	m := &Message{
		Header: Header{
			Magic:    MagicBytes,
			Version:  ver,
			Command:  data[3],
			Seq:      binary.BigEndian.Uint32(data[4:8]),
			Status:   data[8],
			Reserved: data[9],
		},
	}

	offset := HeaderSize

	switch m.Header.Command {
	case CmdAdd, CmdDel:
		if len(data) < offset+1 {
			return nil, ErrPacketTooShort
		}
		tableLen := int(data[offset])
		offset++

		if len(data) < offset+tableLen+4+2 {
			return nil, ErrPacketTooShort
		}
		m.Table = string(data[offset : offset+tableLen])
		offset += tableLen

		m.TTL = binary.BigEndian.Uint32(data[offset : offset+4])
		offset += 4

		ipCount := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		offset += 2

		m.IPs = make([]net.IP, 0, ipCount)
		for i := 0; i < ipCount; i++ {
			if len(data) < offset+1 {
				return nil, ErrPacketTooShort
			}
			family := data[offset]
			offset++

			if family == 4 {
				if len(data) < offset+net.IPv4len {
					return nil, ErrPacketTooShort
				}
				ip := make(net.IP, net.IPv4len)
				copy(ip, data[offset:offset+net.IPv4len])
				m.IPs = append(m.IPs, ip)
				offset += net.IPv4len
			} else if family == 6 {
				if len(data) < offset+net.IPv6len {
					return nil, ErrPacketTooShort
				}
				ip := make(net.IP, net.IPv6len)
				copy(ip, data[offset:offset+net.IPv6len])
				m.IPs = append(m.IPs, ip)
				offset += net.IPv6len
			} else {
				return nil, fmt.Errorf("unsupported address family: %d", family)
			}
		}

	case CmdFlush:
		if len(data) < offset+1 {
			return nil, ErrPacketTooShort
		}
		tableLen := int(data[offset])
		offset++
		if len(data) < offset+tableLen {
			return nil, ErrPacketTooShort
		}
		m.Table = string(data[offset : offset+tableLen])

	case CmdAck:
		if len(data) >= offset+2 {
			errLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
			if len(data) >= offset+errLen {
				m.ErrMsg = string(data[offset : offset+errLen])
			}
		}

	default:
		return nil, fmt.Errorf("unknown command: 0x%x", m.Header.Command)
	}

	return m, nil
}

// NewAddMessage creates a new CmdAdd message
func NewAddMessage(seq uint32, table string, ttl uint32, ips []net.IP) *Message {
	return &Message{
		Header: Header{
			Magic:   MagicBytes,
			Version: ProtocolVersion,
			Command: CmdAdd,
			Seq:     seq,
		},
		Table: table,
		TTL:   ttl,
		IPs:   ips,
	}
}

// NewDelMessage creates a new CmdDel message
func NewDelMessage(seq uint32, table string, ips []net.IP) *Message {
	return &Message{
		Header: Header{
			Magic:   MagicBytes,
			Version: ProtocolVersion,
			Command: CmdDel,
			Seq:     seq,
		},
		Table: table,
		IPs:   ips,
	}
}

// NewAckMessage creates a new CmdAck response
func NewAckMessage(seq uint32, status uint8, errMsg string) *Message {
	return &Message{
		Header: Header{
			Magic:   MagicBytes,
			Version: ProtocolVersion,
			Command: CmdAck,
			Seq:     seq,
			Status:  status,
		},
		ErrMsg: errMsg,
	}
}
