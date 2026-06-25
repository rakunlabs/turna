package dns

import (
	"testing"

	"github.com/miekg/dns"
)

func newTestHandler(t *testing.T) *handler {
	t.Helper()

	h, err := newHandler(&DNS{
		Origin: "example.com",
		Records: []string{
			"@ IN A 10.0.0.1",          // apex via @
			"www IN A 10.0.0.2",        // relative name
			"*.wild IN A 10.0.0.3",     // wildcard
			"alias IN CNAME www",       // cname to relative name
			"txtonly IN TXT \"hello\"", // name exists, only TXT
		},
	})
	if err != nil {
		t.Fatalf("newHandler error: %v", err)
	}

	return h
}

func query(name string, qtype uint16) *dns.Msg {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), qtype)

	return m
}

func aValues(rrs []dns.RR) []string {
	var out []string
	for _, rr := range rrs {
		if a, ok := rr.(*dns.A); ok {
			out = append(out, a.A.String())
		}
	}

	return out
}

func TestHandler_Apex(t *testing.T) {
	h := newTestHandler(t)

	resp := h.answer(query("example.com", dns.TypeA))
	if resp == nil {
		t.Fatal("expected authoritative answer for apex")
	}

	if got := aValues(resp.Answer); len(got) != 1 || got[0] != "10.0.0.1" {
		t.Fatalf("apex A = %v, want [10.0.0.1]", got)
	}
}

func TestHandler_RelativeName(t *testing.T) {
	h := newTestHandler(t)

	resp := h.answer(query("www.example.com", dns.TypeA))
	if got := aValues(resp.Answer); len(got) != 1 || got[0] != "10.0.0.2" {
		t.Fatalf("www A = %v, want [10.0.0.2]", got)
	}
}

func TestHandler_Wildcard(t *testing.T) {
	h := newTestHandler(t)

	resp := h.answer(query("anything.wild.example.com", dns.TypeA))
	if resp == nil || len(resp.Answer) == 0 {
		t.Fatal("expected wildcard answer")
	}

	if got := aValues(resp.Answer); len(got) != 1 || got[0] != "10.0.0.3" {
		t.Fatalf("wildcard A = %v, want [10.0.0.3]", got)
	}

	// synthesized owner must be the queried name, not the wildcard.
	if name := resp.Answer[0].Header().Name; name != dns.Fqdn("anything.wild.example.com") {
		t.Fatalf("wildcard owner = %s, want anything.wild.example.com.", name)
	}
}

func TestHandler_CNAMEFollow(t *testing.T) {
	h := newTestHandler(t)

	resp := h.answer(query("alias.example.com", dns.TypeA))
	if resp == nil {
		t.Fatal("expected cname answer")
	}

	var hasCNAME bool
	for _, rr := range resp.Answer {
		if _, ok := rr.(*dns.CNAME); ok {
			hasCNAME = true
		}
	}

	if !hasCNAME {
		t.Fatalf("expected a CNAME in answer, got %v", resp.Answer)
	}

	if got := aValues(resp.Answer); len(got) != 1 || got[0] != "10.0.0.2" {
		t.Fatalf("cname target A = %v, want [10.0.0.2]", got)
	}
}

func TestHandler_NoData(t *testing.T) {
	h := newTestHandler(t)

	// name exists (TXT) but no AAAA -> NODATA (NOERROR, empty answer).
	resp := h.answer(query("txtonly.example.com", dns.TypeAAAA))
	if resp == nil {
		t.Fatal("expected authoritative NODATA response")
	}

	if resp.Rcode != dns.RcodeSuccess {
		t.Fatalf("NODATA rcode = %d, want %d", resp.Rcode, dns.RcodeSuccess)
	}

	if len(resp.Answer) != 0 {
		t.Fatalf("NODATA answers = %v, want empty", resp.Answer)
	}
}

func TestHandler_NXDomain(t *testing.T) {
	h := newTestHandler(t)

	resp := h.answer(query("absent.example.com", dns.TypeA))
	if resp == nil {
		t.Fatal("expected authoritative NXDOMAIN response")
	}

	if resp.Rcode != dns.RcodeNameError {
		t.Fatalf("rcode = %d, want NXDOMAIN %d", resp.Rcode, dns.RcodeNameError)
	}
}

func TestHandler_OutOfZoneFallsThrough(t *testing.T) {
	h := newTestHandler(t)

	// name outside the configured zone -> nil so the caller forwards upstream.
	if resp := h.answer(query("other.org", dns.TypeA)); resp != nil {
		t.Fatalf("out-of-zone answer = %v, want nil (fallback)", resp)
	}
}

func TestHandler_NoOriginOnlyMatches(t *testing.T) {
	h, err := newHandler(&DNS{
		Records: []string{"absolute.test. IN A 10.1.1.1"},
	})
	if err != nil {
		t.Fatalf("newHandler error: %v", err)
	}

	if got := aValues(h.answer(query("absolute.test", dns.TypeA)).Answer); len(got) != 1 || got[0] != "10.1.1.1" {
		t.Fatalf("absolute A = %v, want [10.1.1.1]", got)
	}

	// Without an origin, unknown names are not authoritative -> fallback (nil).
	if resp := h.answer(query("unknown.test", dns.TypeA)); resp != nil {
		t.Fatalf("unknown answer = %v, want nil", resp)
	}
}
