// Package dns is a UDP middleware that answers DNS queries from a set of
// statically configured records, falling back to upstream resolvers for names
// it does not own.
//
// Records are written in standard zone-file syntax and parsed with
// github.com/miekg/dns, so zone-file conveniences work:
//
//   - "@" and relative names resolve against the configured origin (zone),
//   - "*" wildcard owner names match per RFC 4592 (closest encloser),
//   - "$TTL"/"$ORIGIN" directives inside records are honoured.
package dns

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
)

type DNS struct {
	// Origin is the zone name used to expand "@" and relative record names,
	// for example "example.com". When set, the responder answers
	// authoritatively (NXDOMAIN/NODATA) for names inside the zone.
	Origin string `cfg:"origin"`
	// TTL is the default TTL (seconds) applied to records that omit one.
	TTL uint32 `cfg:"ttl"`
	// Records are zone-file lines, e.g. "www 3600 IN A 10.0.0.1" or
	// "*.example.com. IN A 10.0.0.2".
	Records []string `cfg:"records"`
	// Upstream resolvers (host:port) used when no static record matches.
	Upstream []string `cfg:"upstream"`
	// Timeout for upstream queries, default is 5s.
	Timeout time.Duration `cfg:"timeout"`
}

type handler struct {
	origin   string
	records  map[string][]dns.RR
	upstream []string
	client   *dns.Client
}

func (m *DNS) Middleware(ctx context.Context, _ string) (func(conn net.PacketConn, addr net.Addr, data []byte) error, error) {
	h, err := newHandler(m)
	if err != nil {
		return nil, err
	}

	return func(conn net.PacketConn, addr net.Addr, data []byte) error {
		return h.serve(ctx, conn, addr, data)
	}, nil
}

func newHandler(m *DNS) (*handler, error) {
	origin := ""
	if m.Origin != "" {
		origin = dns.CanonicalName(m.Origin)
	}

	ttl := m.TTL
	if ttl == 0 {
		ttl = 3600
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "$TTL %d\n", ttl)
	for _, r := range m.Records {
		sb.WriteString(r)
		sb.WriteByte('\n')
	}

	records := map[string][]dns.RR{}
	zp := dns.NewZoneParser(strings.NewReader(sb.String()), origin, "")
	for rr, ok := zp.Next(); ok; rr, ok = zp.Next() {
		name := dns.CanonicalName(rr.Header().Name)
		records[name] = append(records[name], rr)
	}

	if err := zp.Err(); err != nil {
		return nil, fmt.Errorf("parse dns records: %w", err)
	}

	timeout := m.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	return &handler{
		origin:   origin,
		records:  records,
		upstream: m.Upstream,
		client:   &dns.Client{Net: "udp", Timeout: timeout},
	}, nil
}

func (h *handler) serve(ctx context.Context, conn net.PacketConn, addr net.Addr, data []byte) error {
	req := new(dns.Msg)
	if err := req.Unpack(data); err != nil {
		return fmt.Errorf("unpack dns request: %w", err)
	}

	resp := h.answer(req)
	if resp == nil && len(h.upstream) > 0 {
		resp = h.forward(ctx, req)
	}

	if resp == nil {
		resp = new(dns.Msg).SetRcode(req, dns.RcodeRefused)
	}

	out, err := resp.Pack()
	if err != nil {
		return fmt.Errorf("pack dns response: %w", err)
	}

	if _, err := conn.WriteTo(out, addr); err != nil {
		return fmt.Errorf("write dns response to %s: %w", addr, err)
	}

	return nil
}

// answer builds a response from the static records. It returns nil when the
// query is not owned by this responder and should be forwarded upstream.
func (h *handler) answer(req *dns.Msg) *dns.Msg {
	m := new(dns.Msg)
	m.SetReply(req)
	m.Authoritative = true

	if len(req.Question) == 0 {
		return m.SetRcode(req, dns.RcodeFormatError)
	}

	q := req.Question[0]
	qname := dns.CanonicalName(q.Name)

	if answers := h.lookup(qname, q.Qtype); len(answers) > 0 {
		m.Answer = answers

		return m
	}

	if answers := h.lookupWildcard(qname, q.Qtype); len(answers) > 0 {
		m.Answer = answers

		return m
	}

	// Authoritative only for names inside the configured zone.
	if h.origin != "" && dns.IsSubDomain(h.origin, qname) {
		if h.nameExists(qname) {
			// NODATA: the name exists but not for this type.
			return m
		}

		return m.SetRcode(req, dns.RcodeNameError) // NXDOMAIN
	}

	// Not our zone: let the caller forward upstream.
	return nil
}

// lookup returns the records at the exact owner name for the given type,
// following a CNAME when the requested type is not CNAME.
func (h *handler) lookup(qname string, qtype uint16) []dns.RR {
	rrs := h.records[qname]
	if len(rrs) == 0 {
		return nil
	}

	var out []dns.RR
	var cname dns.RR

	for _, rr := range rrs {
		switch rr.Header().Rrtype {
		case qtype:
			out = append(out, rr)
		case dns.TypeCNAME:
			cname = rr
		}
	}

	if len(out) == 0 && cname != nil && qtype != dns.TypeCNAME {
		out = append(out, cname)
		target := dns.CanonicalName(cname.(*dns.CNAME).Target)
		out = append(out, h.lookup(target, qtype)...)
	}

	return out
}

// lookupWildcard matches "*" owner names from the closest encloser upward,
// synthesizing answers with the queried name as owner (RFC 4592).
func (h *handler) lookupWildcard(qname string, qtype uint16) []dns.RR {
	labels := dns.SplitDomainName(qname)

	for i := range labels {
		candidate := dns.CanonicalName("*." + strings.Join(labels[i+1:], "."))

		rrs := h.records[candidate]
		if len(rrs) == 0 {
			continue
		}

		if out := synthesize(rrs, qname, qtype); len(out) > 0 {
			return out
		}
	}

	return nil
}

func synthesize(rrs []dns.RR, qname string, qtype uint16) []dns.RR {
	var out []dns.RR
	var cname dns.RR

	for _, rr := range rrs {
		switch rr.Header().Rrtype {
		case qtype:
			c := dns.Copy(rr)
			c.Header().Name = qname
			out = append(out, c)
		case dns.TypeCNAME:
			cname = rr
		}
	}

	if len(out) == 0 && cname != nil && qtype != dns.TypeCNAME {
		c := dns.Copy(cname)
		c.Header().Name = qname
		out = append(out, c)
	}

	return out
}

// nameExists reports whether any record (any type) owns the name, directly or
// through a wildcard match. Used to distinguish NODATA from NXDOMAIN.
func (h *handler) nameExists(qname string) bool {
	if len(h.records[qname]) > 0 {
		return true
	}

	labels := dns.SplitDomainName(qname)
	for i := range labels {
		candidate := dns.CanonicalName("*." + strings.Join(labels[i+1:], "."))
		if len(h.records[candidate]) > 0 {
			return true
		}
	}

	return false
}

func (h *handler) forward(ctx context.Context, req *dns.Msg) *dns.Msg {
	for _, up := range h.upstream {
		r, _, err := h.client.ExchangeContext(ctx, req, up)
		if err == nil && r != nil {
			return r
		}
	}

	return nil
}
