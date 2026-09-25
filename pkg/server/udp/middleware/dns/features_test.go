package dns

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// startUpstream runs a DNS server answering every A query with ip.
func startUpstream(t *testing.T, ip string, ttl uint32) (string, *atomic.Int64) {
	t.Helper()

	var count atomic.Int64

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	handler := dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		count.Add(1)

		m := new(dns.Msg)
		m.SetReply(r)

		if r.Question[0].Qtype == dns.TypeA {
			rr, _ := dns.NewRR(r.Question[0].Name + " " + strconv.FormatUint(uint64(ttl), 10) + " IN A " + ip)
			m.Answer = append(m.Answer, rr)
		}

		_ = w.WriteMsg(m)
	})

	// tcp on the same port for tcp:// upstreams and truncation retries
	l, err := net.Listen("tcp", pc.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}

	for _, srv := range []*dns.Server{{PacketConn: pc, Handler: handler}, {Listener: l, Handler: handler}} {
		go func() { _ = srv.ActivateAndServe() }()

		t.Cleanup(func() { _ = srv.Shutdown() })
	}

	return pc.LocalAddr().String(), &count
}

func resolveA(t *testing.T, h *handler, name string) *dns.Msg {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	return h.resolve(ctx, query(name, dns.TypeA))
}

func TestConditionalForward(t *testing.T) {
	def, _ := startUpstream(t, "1.1.1.1", 60)
	corp, _ := startUpstream(t, "10.9.9.9", 60)
	deep, _ := startUpstream(t, "10.8.8.8", 60)

	h, err := newHandler(&DNS{
		Upstream: []string{def},
		Forward: []Forward{
			{Domains: []string{"corp.local"}, Upstream: []string{"udp://" + corp}},
			{Domains: []string{"deep.corp.local"}, Upstream: []string{"tcp://" + deep}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string]string{
		"example.com":       "1.1.1.1",
		"a.corp.local":      "10.9.9.9",
		"corp.local":        "10.9.9.9",
		"x.deep.corp.local": "10.8.8.8",
		"notcorp.local":     "1.1.1.1",
	} {
		resp := resolveA(t, h, name)
		if got := aValues(resp.Answer); len(got) != 1 || got[0] != want {
			t.Errorf("%s: got %v rcode %d, want %s", name, got, resp.Rcode, want)
		}
	}
}

func TestCache(t *testing.T) {
	up, count := startUpstream(t, "1.2.3.4", 100)

	h, err := newHandler(&DNS{Upstream: []string{up}, Cache: &Cache{}})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	h.cache.now = func() time.Time { return now }

	resolveA(t, h, "cached.example.com")

	now = now.Add(40 * time.Second)

	resp := resolveA(t, h, "CACHED.example.com")
	if count.Load() != 1 {
		t.Fatalf("upstream queried %d times", count.Load())
	}

	if ttl := resp.Answer[0].Header().Ttl; ttl != 60 {
		t.Fatalf("ttl = %d, want 60", ttl)
	}

	now = now.Add(61 * time.Second)

	resolveA(t, h, "cached.example.com")

	if count.Load() != 2 {
		t.Fatalf("expired entry not refreshed, count %d", count.Load())
	}
}

func TestCacheLimits(t *testing.T) {
	c := newCache(Cache{Size: 2, MaxTTL: 10 * time.Second})

	for _, name := range []string{"a.", "b.", "c."} {
		req := query(name, dns.TypeA)
		resp := new(dns.Msg).SetReply(req)
		rr, _ := dns.NewRR(name + " 3600 IN A 1.1.1.1")
		resp.Answer = []dns.RR{rr}
		c.set(req, resp)
	}

	if c.get(query("a.", dns.TypeA)) != nil {
		t.Fatal("oldest entry should be evicted")
	}

	got := c.get(query("c.", dns.TypeA))
	if got == nil || got.Answer[0].Header().Ttl != 3600 {
		// ttl in the message is kept, the entry expires at MaxTTL
		t.Fatalf("got %v", got)
	}

	c.now = func() time.Time { return time.Now().Add(11 * time.Second) }
	if c.get(query("c.", dns.TypeA)) != nil {
		t.Fatal("entry should expire at max_ttl")
	}

	// server failures are not cached
	req := query("fail.", dns.TypeA)
	c.set(req, new(dns.Msg).SetRcode(req, dns.RcodeServerFailure))

	if c.get(req) != nil {
		t.Fatal("servfail cached")
	}
}

func TestBlocklist(t *testing.T) {
	up, count := startUpstream(t, "1.2.3.4", 60)

	dir := t.TempDir()
	file := filepath.Join(dir, "hosts")

	if err := os.WriteFile(file, []byte("# comment\n0.0.0.0 ads.example.com\n127.0.0.1 localhost\ntracker.test # inline\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	h, err := newHandler(&DNS{
		Upstream: []string{up},
		Blocklist: &Blocklist{
			Domains: []string{"bad.com"},
			Files:   []string{file},
			Allow:   []string{"ok.bad.com"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"bad.com", "x.bad.com", "ads.example.com", "a.tracker.test"} {
		if resp := resolveA(t, h, name); resp.Rcode != dns.RcodeNameError {
			t.Errorf("%s: rcode %d, want NXDOMAIN", name, resp.Rcode)
		}
	}

	if count.Load() != 0 {
		t.Fatal("blocked query forwarded")
	}

	for _, name := range []string{"ok.bad.com", "notbad.com", "localhost", "example.com"} {
		if resp := resolveA(t, h, name); resp.Rcode != dns.RcodeSuccess || len(resp.Answer) != 1 {
			t.Errorf("%s: rcode %d answers %d", name, resp.Rcode, len(resp.Answer))
		}
	}

	null, err := newHandler(&DNS{Blocklist: &Blocklist{Domains: []string{"bad.com"}, Response: "null_ip"}})
	if err != nil {
		t.Fatal(err)
	}

	if got := aValues(resolveA(t, null, "bad.com").Answer); len(got) != 1 || got[0] != "0.0.0.0" {
		t.Fatalf("null_ip: %v", got)
	}
}

func TestDoHUpstream(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/dns-message" {
			http.Error(w, "bad content type", http.StatusBadRequest)

			return
		}

		body, _ := io.ReadAll(r.Body)

		req := new(dns.Msg)
		if err := req.Unpack(body); err != nil || req.Id != 0 {
			http.Error(w, "bad message", http.StatusBadRequest)

			return
		}

		m := new(dns.Msg).SetReply(req)
		rr, _ := dns.NewRR(req.Question[0].Name + " 60 IN A 9.9.9.9")
		m.Answer = []dns.RR{rr}

		out, _ := m.Pack()
		w.Header().Set("Content-Type", "application/dns-message")
		_, _ = w.Write(out)
	}))
	defer srv.Close()

	h, err := newHandler(&DNS{Upstream: []string{srv.URL + "/dns-query"}, InsecureSkipVerify: true})
	if err != nil {
		t.Fatal(err)
	}

	req := query("doh.example.com", dns.TypeA)
	req.Id = 4242

	resp := h.resolve(t.Context(), req)
	if got := aValues(resp.Answer); len(got) != 1 || got[0] != "9.9.9.9" || resp.Id != 4242 {
		t.Fatalf("got %v id %d", got, resp.Id)
	}
}

func TestUpstreamFailure(t *testing.T) {
	// nothing listens on this port
	pc, _ := net.ListenPacket("udp", "127.0.0.1:0")
	addr := pc.LocalAddr().String()
	pc.Close()

	h, err := newHandler(&DNS{Upstream: []string{addr}, Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	if resp := resolveA(t, h, "example.com"); resp.Rcode != dns.RcodeServerFailure {
		t.Fatalf("rcode %d, want SERVFAIL", resp.Rcode)
	}

	noUp, _ := newHandler(&DNS{})
	if resp := resolveA(t, noUp, "example.com"); resp.Rcode != dns.RcodeRefused {
		t.Fatalf("rcode %d, want REFUSED", resp.Rcode)
	}
}

func TestInvalidConfig(t *testing.T) {
	for name, cfg := range map[string]*DNS{
		"scheme":   {Upstream: []string{"quic://1.1.1.1"}},
		"no port":  {Upstream: []string{"1.1.1.1"}},
		"forward":  {Forward: []Forward{{Domains: []string{"a"}}}},
		"response": {Blocklist: &Blocklist{Response: "drop"}},
		"file":     {Blocklist: &Blocklist{Files: []string{"/does/not/exist"}}},
	} {
		if _, err := newHandler(cfg); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}

	if _, err := newHandler(&DNS{Upstream: []string{"tls://1.1.1.1#cloudflare-dns.com", "tls://9.9.9.9:853"}}); err != nil {
		t.Fatalf("tls upstream: %v", err)
	}
}
