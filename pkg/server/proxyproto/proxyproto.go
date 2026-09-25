// Package proxyproto reads and writes HAProxy PROXY protocol v1 and v2 headers.
//
// https://www.haproxy.org/download/2.9/doc/proxy-protocol.txt
package proxyproto

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

var (
	v1Prefix  = []byte("PROXY ")
	v2Sig     = []byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A}
	errNoInfo = errors.New("proxy protocol header without address information")

	// ErrNotProxy is returned when the connection does not start with a PROXY header.
	ErrNotProxy = errors.New("not a proxy protocol header")
)

const v1MaxLen = 107

// Header is the address information of a PROXY header. Local is true for
// health checks (v2 LOCAL command, v1 UNKNOWN); the addresses are empty then.
type Header struct {
	Source      net.Addr
	Destination net.Addr
	Local       bool
}

// Write writes a PROXY header for the connection from src to dst.
func Write(w io.Writer, version int, src, dst net.Addr) error {
	var b []byte

	switch version {
	case 1:
		b = V1(src, dst)
	case 2:
		b = V2(src, dst)
	default:
		return fmt.Errorf("unsupported proxy protocol version %d", version)
	}

	_, err := w.Write(b)

	return err
}

func addrPort(a net.Addr) (netip.AddrPort, bool) {
	switch v := a.(type) {
	case *net.TCPAddr:
		return v.AddrPort(), true
	case *net.UDPAddr:
		return v.AddrPort(), true
	}

	ap, err := netip.ParseAddrPort(a.String())

	return ap, err == nil
}

// V1 returns a text PROXY header.
func V1(src, dst net.Addr) []byte {
	s, okS := addrPort(src)
	d, okD := addrPort(dst)

	if !okS || !okD {
		return []byte("PROXY UNKNOWN\r\n")
	}

	sa, da := s.Addr().Unmap(), d.Addr().Unmap()

	family := "TCP4"
	if !sa.Is4() || !da.Is4() {
		family = "TCP6"
		sa, da = netip.AddrFrom16(sa.As16()), netip.AddrFrom16(da.As16())
	}

	return fmt.Appendf(nil, "PROXY %s %s %s %d %d\r\n", family, sa, da, s.Port(), d.Port())
}

// V2 returns a binary PROXY header.
func V2(src, dst net.Addr) []byte {
	var buf bytes.Buffer
	buf.Write(v2Sig)

	s, okS := addrPort(src)
	d, okD := addrPort(dst)

	if !okS || !okD {
		// PROXY command, UNSPEC family
		buf.Write([]byte{0x21, 0x00, 0x00, 0x00})

		return buf.Bytes()
	}

	transport := byte(0x1) // STREAM
	if _, ok := src.(*net.UDPAddr); ok {
		transport = 0x2 // DGRAM
	}

	sa, da := s.Addr().Unmap(), d.Addr().Unmap()

	buf.WriteByte(0x21) // version 2, PROXY command

	if sa.Is4() && da.Is4() {
		buf.WriteByte(0x10 | transport)
		_ = binary.Write(&buf, binary.BigEndian, uint16(12))

		a4, b4 := sa.As4(), da.As4()
		buf.Write(a4[:])
		buf.Write(b4[:])
	} else {
		buf.WriteByte(0x20 | transport)
		_ = binary.Write(&buf, binary.BigEndian, uint16(36))

		a16, b16 := sa.As16(), da.As16()
		buf.Write(a16[:])
		buf.Write(b16[:])
	}

	_ = binary.Write(&buf, binary.BigEndian, s.Port())
	_ = binary.Write(&buf, binary.BigEndian, d.Port())

	return buf.Bytes()
}

// Read parses a PROXY header (v1 or v2) from r. ErrNotProxy is returned
// without consuming data when the stream does not start with a header.
func Read(r *bufio.Reader) (*Header, error) {
	first, err := r.Peek(1)
	if err != nil {
		return nil, err
	}

	switch first[0] {
	case 'P':
		p, err := r.Peek(len(v1Prefix))
		if err != nil || !bytes.Equal(p, v1Prefix) {
			return nil, ErrNotProxy
		}

		return readV1(r)
	case v2Sig[0]:
		p, err := r.Peek(len(v2Sig))
		if err != nil || !bytes.Equal(p, v2Sig) {
			return nil, ErrNotProxy
		}

		return readV2(r)
	}

	return nil, ErrNotProxy
}

func readV1(r *bufio.Reader) (*Header, error) {
	var line []byte

	for len(line) < v1MaxLen {
		b, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("read proxy v1 header: %w", err)
		}

		line = append(line, b)
		if b == '\n' {
			break
		}
	}

	if !bytes.HasSuffix(line, []byte("\r\n")) {
		return nil, errors.New("proxy v1 header too long or not terminated")
	}

	fields := strings.Fields(string(line[:len(line)-2]))
	if len(fields) < 2 {
		return nil, errors.New("invalid proxy v1 header")
	}

	if fields[1] == "UNKNOWN" {
		return &Header{Local: true}, nil
	}

	if len(fields) != 6 || (fields[1] != "TCP4" && fields[1] != "TCP6") {
		return nil, fmt.Errorf("invalid proxy v1 header %q", string(line))
	}

	src, err := parseV1Addr(fields[2], fields[4])
	if err != nil {
		return nil, err
	}

	dst, err := parseV1Addr(fields[3], fields[5])
	if err != nil {
		return nil, err
	}

	if (fields[1] == "TCP4") != src.Addr().Is4() || src.Addr().Is4() != dst.Addr().Is4() {
		return nil, errors.New("proxy v1 address family mismatch")
	}

	return &Header{
		Source:      net.TCPAddrFromAddrPort(src),
		Destination: net.TCPAddrFromAddrPort(dst),
	}, nil
}

func parseV1Addr(ip, port string) (netip.AddrPort, error) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("invalid proxy v1 address %q", ip)
	}

	p, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		return netip.AddrPort{}, fmt.Errorf("invalid proxy v1 port %q", port)
	}

	return netip.AddrPortFrom(addr, uint16(p)), nil
}

func readV2(r *bufio.Reader) (*Header, error) {
	hdr := make([]byte, 16)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, fmt.Errorf("read proxy v2 header: %w", err)
	}

	verCmd, famProto := hdr[12], hdr[13]
	length := int(binary.BigEndian.Uint16(hdr[14:16]))

	if verCmd>>4 != 2 {
		return nil, fmt.Errorf("unsupported proxy v2 version %d", verCmd>>4)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("read proxy v2 payload: %w", err)
	}

	switch verCmd & 0x0F {
	case 0x0: // LOCAL
		return &Header{Local: true}, nil
	case 0x1: // PROXY
	default:
		return nil, fmt.Errorf("unsupported proxy v2 command %d", verCmd&0x0F)
	}

	dgram := famProto&0x0F == 0x2

	mk := func(a netip.Addr, port uint16) net.Addr {
		ap := netip.AddrPortFrom(a, port)
		if dgram {
			return net.UDPAddrFromAddrPort(ap)
		}

		return net.TCPAddrFromAddrPort(ap)
	}

	switch famProto >> 4 {
	case 0x1: // INET
		if length < 12 {
			return nil, errors.New("proxy v2 ipv4 payload too short")
		}

		src := netip.AddrFrom4([4]byte(payload[0:4]))
		dst := netip.AddrFrom4([4]byte(payload[4:8]))

		return &Header{
			Source:      mk(src, binary.BigEndian.Uint16(payload[8:10])),
			Destination: mk(dst, binary.BigEndian.Uint16(payload[10:12])),
		}, nil
	case 0x2: // INET6
		if length < 36 {
			return nil, errors.New("proxy v2 ipv6 payload too short")
		}

		src := netip.AddrFrom16([16]byte(payload[0:16])).Unmap()
		dst := netip.AddrFrom16([16]byte(payload[16:32])).Unmap()

		return &Header{
			Source:      mk(src, binary.BigEndian.Uint16(payload[32:34])),
			Destination: mk(dst, binary.BigEndian.Uint16(payload[34:36])),
		}, nil
	}

	// UNSPEC or UNIX: keep the real peer addresses
	return nil, errNoInfo
}

// IsNoInfo reports whether err means the header was valid but carried no
// usable addresses.
func IsNoInfo(err error) bool {
	return errors.Is(err, errNoInfo)
}
