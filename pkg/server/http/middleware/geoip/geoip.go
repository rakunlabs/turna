// Package geoip allows or denies HTTP requests by the country and autonomous
// system of the client IP, looked up in MaxMind DB (mmdb) files.
package geoip

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/maxminddb-golang/v2"
	adaproxy "github.com/rakunlabs/ada/middleware/auth/proxy"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
)

type GeoIP struct {
	// CountryDB is the path of a GeoLite2-Country, GeoLite2-City or DB-IP
	// country/city mmdb file.
	CountryDB string `cfg:"country_db"`
	// ASNDB is the path of a GeoLite2-ASN or DB-IP ASN mmdb file.
	ASNDB string `cfg:"asn_db"`

	// AllowCountries are ISO 3166-1 alpha-2 codes; when set, only matching
	// clients pass.
	AllowCountries []string `cfg:"allow_countries"`
	// DenyCountries are ISO codes that are rejected, checked before allow.
	DenyCountries []string `cfg:"deny_countries"`
	// AllowASN are autonomous system numbers; when set, only matching
	// clients pass.
	AllowASN []uint `cfg:"allow_asn"`
	// DenyASN are autonomous system numbers that are rejected.
	DenyASN []uint `cfg:"deny_asn"`

	// Unknown is allow or deny for clients without a record, e.g. private
	// ranges. Default allow.
	Unknown string `cfg:"unknown"`

	// CountryHeader is set on the upstream request to the country code.
	CountryHeader string `cfg:"country_header"`
	// ASNHeader is set on the upstream request to the AS number.
	ASNHeader string `cfg:"asn_header"`

	// ReloadInterval checks the database files for changes and reopens
	// them, 0 disables it.
	ReloadInterval time.Duration `cfg:"reload_interval"`

	// TrustedProxies are CIDRs whose forwarding headers are used to find the
	// client IP.
	TrustedProxies []string `cfg:"trusted_proxies"`
	// StatusCode of rejected requests, default is 403.
	StatusCode int `cfg:"status_code"`
}

type countryRecord struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
	RegisteredCountry struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"registered_country"`
}

type asnRecord struct {
	Number uint `maxminddb:"autonomous_system_number"`
}

type filter struct {
	countryDB, asnDB *database

	allowCountries, denyCountries map[string]struct{}
	allowASN, denyASN             map[uint]struct{}

	denyUnknown bool

	countryHeader, asnHeader string

	policy adaproxy.Policy
	status int
}

func (m *GeoIP) build() (*filter, error) {
	f := &filter{
		countryHeader: http.CanonicalHeaderKey(strings.TrimSpace(m.CountryHeader)),
		asnHeader:     http.CanonicalHeaderKey(strings.TrimSpace(m.ASNHeader)),
		status:        m.StatusCode,
	}

	var err error
	if f.allowCountries, err = countrySet(m.AllowCountries); err != nil {
		return nil, fmt.Errorf("geoip allow_countries: %w", err)
	}
	if f.denyCountries, err = countrySet(m.DenyCountries); err != nil {
		return nil, fmt.Errorf("geoip deny_countries: %w", err)
	}
	f.allowASN = asnSet(m.AllowASN)
	f.denyASN = asnSet(m.DenyASN)

	countryUsed := f.allowCountries != nil || f.denyCountries != nil || f.countryHeader != ""
	asnUsed := f.allowASN != nil || f.denyASN != nil || f.asnHeader != ""

	if !countryUsed && !asnUsed {
		return nil, errors.New("geoip needs at least one allow, deny or header entry")
	}
	if countryUsed && m.CountryDB == "" {
		return nil, errors.New("geoip country_db is required for country rules and country_header")
	}
	if asnUsed && m.ASNDB == "" {
		return nil, errors.New("geoip asn_db is required for asn rules and asn_header")
	}

	switch strings.ToLower(strings.TrimSpace(m.Unknown)) {
	case "", "allow":
	case "deny":
		f.denyUnknown = true
	default:
		return nil, fmt.Errorf("geoip unknown must be allow or deny, got %q", m.Unknown)
	}

	if m.ReloadInterval < 0 {
		return nil, errors.New("geoip reload_interval must not be negative")
	}

	if f.policy, err = adaproxy.New(m.TrustedProxies...); err != nil {
		return nil, fmt.Errorf("geoip trusted_proxies: %w", err)
	}

	if f.status == 0 {
		f.status = http.StatusForbidden
	}

	if countryUsed {
		if f.countryDB, err = openDatabase(m.CountryDB); err != nil {
			return nil, fmt.Errorf("geoip country_db: %w", err)
		}
	}
	if asnUsed {
		if f.asnDB, err = openDatabase(m.ASNDB); err != nil {
			f.countryDB.close()

			return nil, fmt.Errorf("geoip asn_db: %w", err)
		}
	}

	return f, nil
}

func countrySet(codes []string) (map[string]struct{}, error) {
	if len(codes) == 0 {
		return nil, nil
	}

	set := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		c = strings.ToUpper(strings.TrimSpace(c))
		if len(c) != 2 || c[0] < 'A' || c[0] > 'Z' || c[1] < 'A' || c[1] > 'Z' {
			return nil, fmt.Errorf("invalid country code %q", c)
		}

		set[c] = struct{}{}
	}

	return set, nil
}

func asnSet(numbers []uint) map[uint]struct{} {
	if len(numbers) == 0 {
		return nil
	}

	set := make(map[uint]struct{}, len(numbers))
	for _, n := range numbers {
		set[n] = struct{}{}
	}

	return set
}

func (m *GeoIP) Middleware(ctx context.Context) (func(http.Handler) http.Handler, error) {
	f, err := m.build()
	if err != nil {
		return nil, err
	}

	go func() {
		var tick <-chan time.Time
		if m.ReloadInterval > 0 {
			ticker := time.NewTicker(m.ReloadInterval)
			defer ticker.Stop()

			tick = ticker.C
		}

		for {
			select {
			case <-ctx.Done():
				f.countryDB.close()
				f.asnDB.close()

				return
			case <-tick:
				f.countryDB.reload()
				f.asnDB.reload()
			}
		}
	}()

	return f.middleware, nil
}

func (f *filter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.countryHeader != "" {
			r.Header.Del(f.countryHeader)
		}
		if f.asnHeader != "" {
			r.Header.Del(f.asnHeader)
		}

		ipStr, err := f.policy.ClientIP(r)
		if err != nil {
			// malformed forwarding headers from a trusted proxy
			f.reject(w)

			return
		}

		// non IP transports (unix sockets) have no record
		ip, _ := netip.ParseAddr(ipStr)
		country, asn := f.lookup(ip.Unmap())

		if !f.allowed(country, asn) {
			f.reject(w)

			return
		}

		if f.countryHeader != "" && country != "" {
			r.Header.Set(f.countryHeader, country)
		}
		if f.asnHeader != "" && asn != 0 {
			r.Header.Set(f.asnHeader, strconv.FormatUint(uint64(asn), 10))
		}

		next.ServeHTTP(w, r)
	})
}

func (f *filter) reject(w http.ResponseWriter) {
	httputil.HandleError(w, httputil.NewError("location not allowed", nil, f.status))
}

func (f *filter) lookup(ip netip.Addr) (string, uint) {
	if !ip.IsValid() {
		return "", 0
	}

	var country string
	if f.countryDB != nil {
		var rec countryRecord
		if f.countryDB.lookup(ip, &rec) {
			country = strings.ToUpper(rec.Country.ISOCode)
			if country == "" {
				country = strings.ToUpper(rec.RegisteredCountry.ISOCode)
			}
		}
	}

	var asn uint
	if f.asnDB != nil {
		var rec asnRecord
		if f.asnDB.lookup(ip, &rec) {
			asn = rec.Number
		}
	}

	return country, asn
}

func (f *filter) allowed(country string, asn uint) bool {
	if country != "" {
		if _, ok := f.denyCountries[country]; ok {
			return false
		}
	}
	if asn != 0 {
		if _, ok := f.denyASN[asn]; ok {
			return false
		}
	}

	countryRules := f.allowCountries != nil || f.denyCountries != nil
	asnRules := f.allowASN != nil || f.denyASN != nil

	if (countryRules && country == "") || (asnRules && asn == 0) {
		if f.denyUnknown {
			return false
		}
	}

	if f.allowCountries != nil && country != "" {
		if _, ok := f.allowCountries[country]; !ok {
			return false
		}
	}
	if f.allowASN != nil && asn != 0 {
		if _, ok := f.allowASN[asn]; !ok {
			return false
		}
	}

	return true
}

// database is a mmdb reader that can be swapped by reload. Lookups hold the
// read lock, so the old reader is closed only after they finish.
type database struct {
	path string

	mu      sync.RWMutex
	reader  *maxminddb.Reader
	modTime time.Time
	size    int64
}

func openDatabase(path string) (*database, error) {
	reader, info, err := openReader(path)
	if err != nil {
		return nil, err
	}

	return &database{path: path, reader: reader, modTime: info.ModTime(), size: info.Size()}, nil
}

func openReader(path string) (*maxminddb.Reader, os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}

	reader, err := maxminddb.Open(path)
	if err != nil {
		return nil, nil, err
	}

	return reader, info, nil
}

func (d *database) lookup(ip netip.Addr, v any) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.reader == nil {
		return false
	}

	res := d.reader.Lookup(ip)
	if !res.Found() {
		return false
	}

	return res.Decode(v) == nil
}

func (d *database) reload() {
	if d == nil {
		return
	}

	info, err := os.Stat(d.path)
	if err != nil {
		slog.Warn("geoip database stat failed", "path", d.path, "error", err)

		return
	}

	d.mu.RLock()
	unchanged := info.ModTime().Equal(d.modTime) && info.Size() == d.size
	d.mu.RUnlock()

	if unchanged {
		return
	}

	reader, info, err := openReader(d.path)
	if err != nil {
		slog.Warn("geoip database reload failed, keeping the old one", "path", d.path, "error", err)

		return
	}

	d.mu.Lock()
	old := d.reader
	d.reader, d.modTime, d.size = reader, info.ModTime(), info.Size()
	d.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}

	slog.Info("geoip database reloaded", "path", d.path)
}

func (d *database) close() {
	if d == nil {
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.reader != nil {
		_ = d.reader.Close()
		d.reader = nil
	}
}
