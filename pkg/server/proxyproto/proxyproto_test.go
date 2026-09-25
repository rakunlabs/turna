package proxyproto

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		src, dst net.Addr
	}{
		{"ipv4", &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 1234}, &net.TCPAddr{IP: net.ParseIP("10.0.0.2"), Port: 443}},
		{"ipv6", &net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 1234}, &net.TCPAddr{IP: net.ParseIP("2001:db8::2"), Port: 443}},
		{"udp", &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 53000}, &net.UDPAddr{IP: net.ParseIP("10.0.0.2"), Port: 53}},
	}

	for _, tt := range tests {
		for _, version := range []int{1, 2} {
			if version == 1 && tt.name == "udp" {
				continue
			}

			t.Run(tt.name+"-v"+string(rune('0'+version)), func(t *testing.T) {
				var buf bytes.Buffer
				if err := Write(&buf, version, tt.src, tt.dst); err != nil {
					t.Fatal(err)
				}

				buf.WriteString("payload")

				r := bufio.NewReader(&buf)

				h, err := Read(r)
				if err != nil {
					t.Fatalf("read: %v", err)
				}

				if h.Source.String() != tt.src.String() || h.Destination.String() != tt.dst.String() {
					t.Fatalf("got %s -> %s", h.Source, h.Destination)
				}

				if h.Source.Network() != tt.src.Network() {
					t.Fatalf("network = %s, want %s", h.Source.Network(), tt.src.Network())
				}

				rest, _ := r.ReadString(0)
				if rest != "payload" {
					t.Fatalf("rest = %q", rest)
				}
			})
		}
	}
}

func TestNotProxy(t *testing.T) {
	for _, in := range []string{"GET / HTTP/1.1\r\n", "PROXZ", "\x0D\x0A\x0D\x0Axx"} {
		r := bufio.NewReader(strings.NewReader(in))
		if _, err := Read(r); !errors.Is(err, ErrNotProxy) {
			t.Fatalf("%q: err = %v", in, err)
		}

		// nothing consumed
		if got, _ := r.ReadString(0); got != in {
			t.Fatalf("consumed data: %q", got)
		}
	}
}

func TestInvalidV1(t *testing.T) {
	for _, in := range []string{
		"PROXY TCP4 1.1.1.1 2.2.2.2 1\r\n",
		"PROXY TCP4 1.1.1.1 ::1 1 2\r\n",
		"PROXY TCP4 1.1.1.1 2.2.2.2 1 99999\r\n",
		"PROXY TCP4 " + strings.Repeat("1", 200),
	} {
		if _, err := Read(bufio.NewReader(strings.NewReader(in))); err == nil || errors.Is(err, ErrNotProxy) {
			t.Fatalf("%q: expected parse error, got %v", in, err)
		}
	}
}

func TestLocal(t *testing.T) {
	h, err := Read(bufio.NewReader(strings.NewReader("PROXY UNKNOWN\r\n")))
	if err != nil || !h.Local {
		t.Fatalf("v1 unknown: %v %+v", err, h)
	}

	v2 := append(append([]byte{}, v2Sig...), 0x20, 0x00, 0x00, 0x00)
	h, err = Read(bufio.NewReader(bytes.NewReader(v2)))
	if err != nil || !h.Local {
		t.Fatalf("v2 local: %v %+v", err, h)
	}
}
