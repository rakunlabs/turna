package geoip

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testdata/*.mmdb are generated with github.com/maxmind/mmdbwriter:
//
//	country.mmdb: 1.0.0.0/24 US, 2.0.0.0/24 DE, 2001:db8::/32 TR,
//	              3.0.0.0/24 registered_country JP
//	country_reload.mmdb: 1.0.0.0/24 FR, 2.0.0.0/24 DE
//	asn.mmdb: 1.0.0.0/24 AS13335, 2.0.0.0/24 AS3320

const (
	countryDB = "testdata/country.mmdb"
	asnDB     = "testdata/asn.mmdb"
)

type result struct {
	code    int
	country string
	asn     string
}

func newHandler(t *testing.T, m *GeoIP) http.Handler {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	mw, err := m.Middleware(ctx)
	if err != nil {
		t.Fatal(err)
	}

	return mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Got-Country", r.Header.Get("X-Country-Code"))
		w.Header().Set("X-Got-Asn", r.Header.Get("X-Asn"))
		w.WriteHeader(http.StatusNoContent)
	}))
}

func do(h http.Handler, remote string, headers map[string]string) result {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remote
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	return result{
		code:    rec.Code,
		country: rec.Header().Get("X-Got-Country"),
		asn:     rec.Header().Get("X-Got-Asn"),
	}
}

func TestGeoIP(t *testing.T) {
	tests := []struct {
		name    string
		geoip   GeoIP
		remote  string
		headers map[string]string
		want    int
	}{
		{
			name:   "allow country",
			geoip:  GeoIP{CountryDB: countryDB, AllowCountries: []string{"us"}},
			remote: "1.0.0.1:1234",
			want:   http.StatusNoContent,
		},
		{
			name:   "allow country rejects others",
			geoip:  GeoIP{CountryDB: countryDB, AllowCountries: []string{"US"}},
			remote: "2.0.0.1:1234",
			want:   http.StatusForbidden,
		},
		{
			name:   "deny country",
			geoip:  GeoIP{CountryDB: countryDB, DenyCountries: []string{"DE"}},
			remote: "2.0.0.1:1234",
			want:   http.StatusForbidden,
		},
		{
			name:   "deny wins over allow",
			geoip:  GeoIP{CountryDB: countryDB, AllowCountries: []string{"DE"}, DenyCountries: []string{"de"}},
			remote: "2.0.0.1:1234",
			want:   http.StatusForbidden,
		},
		{
			name:   "ipv6",
			geoip:  GeoIP{CountryDB: countryDB, DenyCountries: []string{"TR"}},
			remote: "[2001:db8::1]:1234",
			want:   http.StatusForbidden,
		},
		{
			name:   "registered country fallback",
			geoip:  GeoIP{CountryDB: countryDB, DenyCountries: []string{"JP"}},
			remote: "3.0.0.1:1234",
			want:   http.StatusForbidden,
		},
		{
			name:   "unknown allowed by default",
			geoip:  GeoIP{CountryDB: countryDB, AllowCountries: []string{"US"}},
			remote: "10.0.0.1:1234",
			want:   http.StatusNoContent,
		},
		{
			name:   "unknown denied",
			geoip:  GeoIP{CountryDB: countryDB, AllowCountries: []string{"US"}, Unknown: "deny"},
			remote: "10.0.0.1:1234",
			want:   http.StatusForbidden,
		},
		{
			name:   "unix socket is unknown",
			geoip:  GeoIP{CountryDB: countryDB, AllowCountries: []string{"US"}, Unknown: "deny"},
			remote: "@",
			want:   http.StatusForbidden,
		},
		{
			name:   "deny asn",
			geoip:  GeoIP{ASNDB: asnDB, DenyASN: []uint{13335}},
			remote: "1.0.0.1:1234",
			want:   http.StatusForbidden,
		},
		{
			name:   "allow asn",
			geoip:  GeoIP{ASNDB: asnDB, AllowASN: []uint{3320}},
			remote: "2.0.0.1:1234",
			want:   http.StatusNoContent,
		},
		{
			name:   "allow asn rejects others",
			geoip:  GeoIP{ASNDB: asnDB, AllowASN: []uint{3320}},
			remote: "1.0.0.1:1234",
			want:   http.StatusForbidden,
		},
		{
			name:   "country and asn both apply",
			geoip:  GeoIP{CountryDB: countryDB, ASNDB: asnDB, AllowCountries: []string{"US"}, DenyASN: []uint{13335}},
			remote: "1.0.0.1:1234",
			want:   http.StatusForbidden,
		},
		{
			name:   "custom status",
			geoip:  GeoIP{CountryDB: countryDB, DenyCountries: []string{"DE"}, StatusCode: http.StatusNotFound},
			remote: "2.0.0.1:1234",
			want:   http.StatusNotFound,
		},
		{
			name:    "forwarded header ignored without trusted proxy",
			geoip:   GeoIP{CountryDB: countryDB, DenyCountries: []string{"DE"}},
			remote:  "1.0.0.1:1234",
			headers: map[string]string{"X-Forwarded-For": "2.0.0.1"},
			want:    http.StatusNoContent,
		},
		{
			name:    "forwarded header from trusted proxy",
			geoip:   GeoIP{CountryDB: countryDB, DenyCountries: []string{"DE"}, TrustedProxies: []string{"10.0.0.0/8"}},
			remote:  "10.0.0.1:1234",
			headers: map[string]string{"X-Forwarded-For": "2.0.0.1"},
			want:    http.StatusForbidden,
		},
		{
			name:    "malformed forwarded header from trusted proxy",
			geoip:   GeoIP{CountryDB: countryDB, AllowCountries: []string{"US"}, TrustedProxies: []string{"10.0.0.0/8"}},
			remote:  "10.0.0.1:1234",
			headers: map[string]string{"X-Forwarded-For": "bogus"},
			want:    http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := do(newHandler(t, &tt.geoip), tt.remote, tt.headers).code; got != tt.want {
				t.Fatalf("status = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestGeoIPHeaders(t *testing.T) {
	h := newHandler(t, &GeoIP{
		CountryDB:     countryDB,
		ASNDB:         asnDB,
		CountryHeader: "X-Country-Code",
		ASNHeader:     "X-ASN",
	})

	got := do(h, "1.0.0.1:1234", nil)
	if got.code != http.StatusNoContent || got.country != "US" || got.asn != "13335" {
		t.Fatalf("got %+v", got)
	}

	// client supplied values must not reach the upstream
	got = do(h, "10.0.0.1:1234", map[string]string{"X-Country-Code": "US", "X-ASN": "1"})
	if got.code != http.StatusNoContent || got.country != "" || got.asn != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestGeoIPConfig(t *testing.T) {
	tests := []struct {
		name  string
		geoip GeoIP
	}{
		{name: "empty", geoip: GeoIP{CountryDB: countryDB}},
		{name: "missing country db", geoip: GeoIP{DenyCountries: []string{"DE"}}},
		{name: "missing asn db", geoip: GeoIP{CountryDB: countryDB, DenyASN: []uint{1}}},
		{name: "invalid country", geoip: GeoIP{CountryDB: countryDB, DenyCountries: []string{"DEU"}}},
		{name: "invalid unknown", geoip: GeoIP{CountryDB: countryDB, DenyCountries: []string{"DE"}, Unknown: "maybe"}},
		{name: "missing file", geoip: GeoIP{CountryDB: "testdata/missing.mmdb", DenyCountries: []string{"DE"}}},
		{name: "invalid file", geoip: GeoIP{CountryDB: "geoip.go", DenyCountries: []string{"DE"}}},
		{name: "invalid proxy", geoip: GeoIP{CountryDB: countryDB, DenyCountries: []string{"DE"}, TrustedProxies: []string{"nope"}}},
		{name: "negative reload", geoip: GeoIP{CountryDB: countryDB, DenyCountries: []string{"DE"}, ReloadInterval: -time.Second}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.geoip.Middleware(context.Background()); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func copyFile(t *testing.T, src, dst string, mod time.Time) {
	t.Helper()

	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}

	// write and rename like database updaters do, the old file stays mapped
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tmp, mod, mod); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		t.Fatal(err)
	}
}

func TestGeoIPReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "country.mmdb")
	copyFile(t, countryDB, path, time.Now().Add(-time.Hour))

	h := newHandler(t, &GeoIP{
		CountryDB:      path,
		DenyCountries:  []string{"FR"},
		ReloadInterval: 10 * time.Millisecond,
	})

	if got := do(h, "1.0.0.1:1234", nil).code; got != http.StatusNoContent {
		t.Fatalf("before reload status = %d", got)
	}

	copyFile(t, "testdata/country_reload.mmdb", path, time.Now())

	deadline := time.Now().Add(2 * time.Second)
	for do(h, "1.0.0.1:1234", nil).code != http.StatusForbidden {
		if time.Now().After(deadline) {
			t.Fatal("database was not reloaded")
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func TestGeoIPCloseOnDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	m := &GeoIP{CountryDB: countryDB, DenyCountries: []string{"DE"}}
	mw, err := m.Middleware(ctx)
	if err != nil {
		t.Fatal(err)
	}

	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	cancel()

	// a closed database has no records, so requests are treated as unknown
	deadline := time.Now().Add(2 * time.Second)
	for do(h, "2.0.0.1:1234", nil).code != http.StatusNoContent {
		if time.Now().After(deadline) {
			t.Fatal("database was not closed")
		}

		time.Sleep(10 * time.Millisecond)
	}
}

func TestGeoIPConfigUnchanged(t *testing.T) {
	m := &GeoIP{
		CountryDB:      countryDB,
		ASNDB:          asnDB,
		AllowCountries: []string{"us", " de"},
		DenyASN:        []uint{3320},
		CountryHeader:  " x-country-code ",
		Unknown:        " Deny",
	}
	before, _ := json.Marshal(m)

	newHandler(t, m)

	if after, _ := json.Marshal(m); string(after) != string(before) {
		t.Fatalf("config changed by Middleware: %s -> %s", before, after)
	}
}
