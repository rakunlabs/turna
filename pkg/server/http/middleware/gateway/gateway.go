// Package gateway implements request matching and filters of the Kubernetes
// Gateway API HTTPRoute. It can also be used directly from the configuration
// to dispatch requests to middleware chains by path, method, header and
// query parameters.
package gateway

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/service"
	"github.com/rakunlabs/turna/pkg/server/registry"
)

const (
	PathExact  = "exact"
	PathPrefix = "prefix"
	PathRegex  = "regex"

	ValueExact = "exact"
	ValueRegex = "regex"
)

type Gateway struct {
	// ListenerPort is the public port used by redirects without an explicit
	// port.
	ListenerPort int `cfg:"listener_port"`

	Rules []Rule `cfg:"rules"`

	// GRPC rules match gRPC requests (content type application/grpc). When
	// set, gRPC requests are only matched against these rules and Rules only
	// see other requests.
	GRPC []Rule `cfg:"grpc"`

	// Passthrough runs the next handler after a rule chain instead of
	// answering with the rule status, and when no rule matches. Use it to
	// apply filters in front of other middlewares.
	Passthrough bool `cfg:"passthrough"`

	// Listeners are the hostnames of the HTTPS listeners sharing the port,
	// "" for a listener without hostname. A TLS request whose Host header
	// selects another listener than its TLS server name, for example on a
	// reused HTTP/2 connection, is answered 421 Misdirected Request.
	Listeners []string `cfg:"listeners"`
}

type Rule struct {
	// Priority orders rules before match specificity, higher first.
	Priority int `cfg:"priority"`
	// Matches of the rule, empty matches every request.
	Matches []Match `cfg:"matches"`
	// Filters run in order before Middlewares.
	Filters []Filter `cfg:"filters"`
	// Middlewares are run after the filters, usually a service.
	Middlewares []string `cfg:"middlewares"`
	// Backends selects one weighted middleware chain per request after
	// Middlewares.
	Backends []Backend `cfg:"backends"`
	// Timeout of the whole request, 0 disables.
	Timeout time.Duration `cfg:"timeout"`
	// BackendTimeout bounds each attempt when Retry is set, 0 disables.
	BackendTimeout time.Duration `cfg:"backend_timeout"`
	// Retry sends the request again when the backend answers a retryable
	// status.
	Retry *Retry `cfg:"retry"`
	// Status is returned when the chain does not write a response, default is
	// 500 as the rule has no backend.
	Status int `cfg:"status"`
}

type Backend struct {
	Weight int `cfg:"weight"`
	// Middlewares run for this backend, usually filters and a service.
	Middlewares []string `cfg:"middlewares"`
	// Status is returned instead of running middlewares, used for invalid
	// backends that still take their share of requests.
	Status int `cfg:"status"`
}

type Match struct {
	// Path match, default is prefix "/".
	Path *PathMatch `cfg:"path"`
	// GRPC matches the gRPC service and method, used by grpc rules.
	GRPC    *GRPCMatch   `cfg:"grpc"`
	Method  string       `cfg:"method"`
	Headers []ValueMatch `cfg:"headers"`
	Query   []ValueMatch `cfg:"query"`
}

type PathMatch struct {
	// Type is exact, prefix or regex.
	Type  string `cfg:"type"`
	Value string `cfg:"value"`
}

type GRPCMatch struct {
	// Service is the fully qualified service name, e.g. "pkg.Service".
	Service string `cfg:"service"`
	// Method name, e.g. "Get".
	Method string `cfg:"method"`
	// Type is exact or regex, default is exact.
	Type string `cfg:"type"`
}

type ValueMatch struct {
	Name  string `cfg:"name"`
	Value string `cfg:"value"`
	// Type is exact or regex, default is exact.
	Type string `cfg:"type"`
}

type Filter struct {
	RequestHeaders  *HeaderModifier `cfg:"request_headers"`
	ResponseHeaders *HeaderModifier `cfg:"response_headers"`
	Redirect        *Redirect       `cfg:"redirect"`
	Rewrite         *Rewrite        `cfg:"rewrite"`
	Mirror          *service.Mirror `cfg:"mirror"`
	CORS            *CORS           `cfg:"cors"`
	// Middlewares runs named middlewares at the position of the filter.
	Middlewares []string `cfg:"middlewares"`
}

type HeaderModifier struct {
	Set    map[string]string `cfg:"set"`
	Add    map[string]string `cfg:"add"`
	Remove []string          `cfg:"remove"`
}

type Redirect struct {
	Scheme   string `cfg:"scheme"`
	Hostname string `cfg:"hostname"`
	Port     int    `cfg:"port"`
	// StatusCode is 301 or 302, default is 302.
	StatusCode int           `cfg:"status_code"`
	Path       *PathModifier `cfg:"path"`
}

type Rewrite struct {
	Hostname string        `cfg:"hostname"`
	Path     *PathModifier `cfg:"path"`
}

type PathModifier struct {
	ReplaceFullPath    *string `cfg:"replace_full_path"`
	ReplacePrefixMatch *string `cfg:"replace_prefix_match"`
}

// Refs returns the middleware names used by the rules.
func (m *Gateway) Refs() []string {
	var refs []string

	for _, r := range slices.Concat(m.Rules, m.GRPC) {
		refs = append(refs, r.Middlewares...)
		for _, f := range r.Filters {
			refs = append(refs, f.Middlewares...)
		}
		for _, b := range r.Backends {
			refs = append(refs, b.Middlewares...)
		}
	}

	return refs
}

type entry struct {
	match   compiledMatch
	handler http.Handler

	priority int
	order    int
}

type compiledMatch struct {
	pathType string
	path     string
	// explicit is set when the match has a path, filters of nested gateways
	// without one use the explicit match of the outer gateway.
	explicit bool
	regex    *regexp.Regexp
	method   string
	headers  []compiledValue
	query    []compiledValue

	// grpc service and method, empty matches any
	grpc        bool
	grpcService compiledValue
	grpcMethod  compiledValue
}

type compiledValue struct {
	name  string
	value string
	regex *regexp.Regexp
}

func (m *Gateway) compile(rules []Rule, grpc bool) ([]entry, error) {
	entries := make([]entry, 0, len(rules))

	for i, rule := range rules {
		matches := rule.Matches
		if len(matches) == 0 {
			matches = []Match{{}}
		}

		for j, match := range matches {
			cm, err := compileMatch(match, grpc)
			if err != nil {
				return nil, fmt.Errorf("rule %d match %d: %w", i, j, err)
			}

			handler, err := m.chain(rule, cm)
			if err != nil {
				return nil, fmt.Errorf("rule %d: %w", i, err)
			}

			entries = append(entries, entry{match: cm, handler: handler, priority: rule.Priority, order: len(entries)})
		}
	}

	sort.SliceStable(entries, func(a, b int) bool {
		if grpc {
			return lessGRPC(&entries[a], &entries[b])
		}

		return less(&entries[a], &entries[b])
	})

	return entries, nil
}

func (m *Gateway) Middleware() (func(http.Handler) http.Handler, error) {
	entries, err := m.compile(m.Rules, false)
	if err != nil {
		return nil, err
	}

	grpcEntries, err := m.compile(m.GRPC, true)
	if err != nil {
		return nil, fmt.Errorf("grpc: %w", err)
	}

	splitGRPC := len(m.GRPC) > 0

	return func(next http.Handler) http.Handler {
		return grpcErrors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if m.misdirected(r) {
				writeError(w, r, http.StatusMisdirectedRequest)

				return
			}

			r = normalizePath(r)

			list := entries
			if splitGRPC && isGRPC(r) {
				list = grpcEntries
			}

			for i := range list {
				if list[i].match.matches(r) {
					if m.Passthrough {
						r = withNext(r, m, next)
					}

					if list[i].match.explicit {
						r = withMatch(r, &list[i].match)
					}

					list[i].handler.ServeHTTP(w, r)

					return
				}
			}

			if m.Passthrough && next != nil {
				next.ServeHTTP(w, r)

				return
			}

			writeError(w, r, http.StatusNotFound)
		}))
	}, nil
}

// less implements the HTTPRoute match precedence.
func less(a, b *entry) bool {
	if a.priority != b.priority {
		return a.priority > b.priority
	}

	ra, rb := pathRank(a.match.pathType), pathRank(b.match.pathType)
	if ra != rb {
		return ra > rb
	}

	if a.match.pathType == PathPrefix && len(a.match.path) != len(b.match.path) {
		return len(a.match.path) > len(b.match.path)
	}

	if (a.match.method != "") != (b.match.method != "") {
		return a.match.method != ""
	}

	if len(a.match.headers) != len(b.match.headers) {
		return len(a.match.headers) > len(b.match.headers)
	}

	if len(a.match.query) != len(b.match.query) {
		return len(a.match.query) > len(b.match.query)
	}

	return a.order < b.order
}

func pathRank(t string) int {
	switch t {
	case PathExact:
		return 2
	case PathPrefix:
		return 1
	default:
		return 0
	}
}

// ValidateMatch reports whether m compiles, e.g. whether its regular
// expressions are supported.
func ValidateMatch(m Match, grpc bool) error {
	_, err := compileMatch(m, grpc)

	return err
}

func compileMatch(m Match, grpc bool) (compiledMatch, error) {
	cm := compiledMatch{pathType: PathPrefix, path: "/", method: strings.ToUpper(m.Method), grpc: grpc}

	if grpc && m.GRPC != nil {
		values, err := compileValues([]ValueMatch{
			{Name: "service", Value: m.GRPC.Service, Type: m.GRPC.Type},
			{Name: "method", Value: m.GRPC.Method, Type: m.GRPC.Type},
		}, func(s string) string { return s })
		if err != nil {
			return cm, fmt.Errorf("grpc: %w", err)
		}

		cm.grpcService, cm.grpcMethod = values[0], values[1]
	}

	if m.Path != nil {
		cm.explicit = true

		switch m.Path.Type {
		case "", PathPrefix:
			cm.path = normalizePrefix(m.Path.Value)
		case PathExact:
			cm.pathType = PathExact
			cm.path = m.Path.Value
		case PathRegex:
			re, err := regexp.Compile("^(?:" + m.Path.Value + ")$")
			if err != nil {
				return cm, fmt.Errorf("path regex: %w", err)
			}

			cm.pathType = PathRegex
			cm.path = m.Path.Value
			cm.regex = re
		default:
			return cm, fmt.Errorf("unsupported path match type %q", m.Path.Type)
		}
	}

	var err error

	cm.headers, err = compileValues(m.Headers, http.CanonicalHeaderKey)
	if err != nil {
		return cm, fmt.Errorf("headers: %w", err)
	}

	cm.query, err = compileValues(m.Query, func(s string) string { return s })
	if err != nil {
		return cm, fmt.Errorf("query: %w", err)
	}

	return cm, nil
}

func compileValues(values []ValueMatch, name func(string) string) ([]compiledValue, error) {
	out := make([]compiledValue, 0, len(values))

	for _, v := range values {
		cv := compiledValue{name: name(v.Name), value: v.Value}

		switch v.Type {
		case "", ValueExact:
		case ValueRegex:
			re, err := regexp.Compile("^(?:" + v.Value + ")$")
			if err != nil {
				return nil, fmt.Errorf("%s: %w", v.Name, err)
			}

			cv.regex = re
		default:
			return nil, fmt.Errorf("unsupported match type %q", v.Type)
		}

		out = append(out, cv)
	}

	return out, nil
}

func normalizePrefix(p string) string {
	if p == "" {
		return "/"
	}

	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
		if p == "" {
			p = "/"
		}
	}

	return p
}

func (m *compiledMatch) matches(r *http.Request) bool {
	path := r.URL.Path

	if m.grpc {
		service, method, ok := splitGRPCPath(path)
		if !ok {
			return false
		}

		if m.grpcService.value != "" && !m.grpcService.matchAny([]string{service}) {
			return false
		}

		if m.grpcMethod.value != "" && !m.grpcMethod.matchAny([]string{method}) {
			return false
		}
	}

	switch m.pathType {
	case PathExact:
		if path != m.path {
			return false
		}
	case PathPrefix:
		if !prefixMatch(m.path, path) {
			return false
		}
	case PathRegex:
		if !m.regex.MatchString(path) {
			return false
		}
	}

	if m.method != "" && r.Method != m.method {
		return false
	}

	for _, h := range m.headers {
		values, ok := r.Header[h.name]
		// Host is not part of r.Header in net/http.
		if h.name == "Host" && r.Host != "" {
			values, ok = []string{r.Host}, true
		}

		if !ok {
			return false
		}

		if !h.matchAny(values) {
			return false
		}
	}

	if len(m.query) > 0 {
		q := r.URL.Query()
		for _, v := range m.query {
			values, ok := q[v.name]
			if !ok || !v.matchAny(values[:1]) {
				return false
			}
		}
	}

	return true
}

func (v *compiledValue) matchAny(values []string) bool {
	for _, value := range values {
		if v.regex != nil {
			if v.regex.MatchString(value) {
				return true
			}

			continue
		}

		if value == v.value {
			return true
		}
	}

	return false
}

// prefixMatch matches path elements, "/foo" matches "/foo" and "/foo/bar"
// but not "/foobar".
func prefixMatch(prefix, path string) bool {
	if prefix == "/" {
		return true
	}

	if !strings.HasPrefix(path, prefix) {
		return false
	}

	return len(path) == len(prefix) || path[len(prefix)] == '/'
}

func (m *Gateway) chain(rule Rule, match compiledMatch) (http.Handler, error) {
	status := rule.Status
	if status == 0 {
		status = http.StatusInternalServerError
	}

	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if next := nextFrom(r, m); next != nil {
			next.ServeHTTP(w, r)

			return
		}

		writeError(w, r, status)
	})

	middlewares := make([]func(http.Handler) http.Handler, 0, len(rule.Filters)+1)

	for i, f := range rule.Filters {
		mws, err := m.filter(f, match)
		if err != nil {
			return nil, fmt.Errorf("filter %d: %w", i, err)
		}

		middlewares = append(middlewares, mws...)
	}

	named, err := resolve(rule.Middlewares)
	if err != nil {
		return nil, err
	}

	if len(rule.Backends) > 0 {
		backends, err := newWeighted(rule.Backends)
		if err != nil {
			return nil, err
		}

		handler = backends
	}

	for i := len(named) - 1; i >= 0; i-- {
		handler = named[i](handler)
	}

	// filters such as mirrors run once, backends are chosen again on retry
	if rule.Retry != nil {
		handler = rule.Retry.middleware(rule.BackendTimeout)(handler)
	}

	if rule.Timeout > 0 {
		middlewares = append([]func(http.Handler) http.Handler{timeout(rule.Timeout)}, middlewares...)
	}

	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}

	return handler, nil
}

func resolve(names []string) ([]func(http.Handler) http.Handler, error) {
	out := make([]func(http.Handler) http.Handler, 0, len(names))

	for _, name := range names {
		m, err := registry.GlobalReg.GetHttpMiddleware(name)
		if err != nil {
			return nil, err
		}

		out = append(out, m...)
	}

	return out, nil
}

func (m *Gateway) filter(f Filter, match compiledMatch) ([]func(http.Handler) http.Handler, error) {
	var out []func(http.Handler) http.Handler

	// preflight requests are answered before other filters run
	if f.CORS != nil {
		out = append(out, f.CORS.middleware())
	}

	if f.RequestHeaders != nil {
		out = append(out, f.RequestHeaders.request())
	}

	if f.ResponseHeaders != nil {
		out = append(out, f.ResponseHeaders.response())
	}

	if f.Rewrite != nil {
		out = append(out, f.Rewrite.middleware(match))
	}

	if f.Redirect != nil {
		mw, err := f.Redirect.middleware(match, m.ListenerPort)
		if err != nil {
			return nil, err
		}

		out = append(out, mw)
	}

	if f.Mirror != nil {
		mw, err := f.Mirror.MirrorMiddleware()
		if err != nil {
			return nil, err
		}

		out = append(out, mw)
	}

	if len(f.Middlewares) > 0 {
		named, err := resolve(f.Middlewares)
		if err != nil {
			return nil, err
		}

		out = append(out, named...)
	}

	return out, nil
}

func (h *HeaderModifier) apply(header http.Header) {
	for k, v := range h.Set {
		header.Set(k, v)
	}

	for k, v := range h.Add {
		header.Add(k, v)
	}

	for _, k := range h.Remove {
		header.Del(k)
	}
}

func (h *HeaderModifier) request() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.apply(r.Header)

			// Host is not part of r.Header in net/http.
			for k, v := range h.Set {
				if strings.EqualFold(k, "Host") {
					r.Host = v
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (h *HeaderModifier) response() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(&headerWriter{ResponseWriter: w, modify: h}, r)
		})
	}
}

// headerWriter modifies response headers right before they are written.
type headerWriter struct {
	http.ResponseWriter

	modify  *HeaderModifier
	written bool
}

func (w *headerWriter) WriteHeader(code int) {
	if informational(code) {
		w.ResponseWriter.WriteHeader(code)

		return
	}

	if !w.written {
		w.written = true
		w.modify.apply(w.Header())
	}

	w.ResponseWriter.WriteHeader(code)
}

func (w *headerWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}

	return w.ResponseWriter.Write(b)
}

func (w *headerWriter) Flush() {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}

	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *headerWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// rewrite applies the modifier to u, keeping the escaping of the part of the
// path after the matched prefix.
func (p *PathModifier) rewrite(u *url.URL, match compiledMatch) {
	switch {
	case p == nil:
	case p.ReplaceFullPath != nil:
		u.Path = *p.ReplaceFullPath
		u.RawPath = ""
	case p.ReplacePrefixMatch != nil && match.pathType == PathPrefix:
		if !prefixMatch(match.path, u.Path) {
			return
		}

		raw := ""
		if u.RawPath != "" {
			if rest, ok := escapedRest(u.EscapedPath(), match.path); ok {
				raw = joinPrefix((&url.URL{Path: *p.ReplacePrefixMatch}).EscapedPath(), rest)
			}
		}

		u.Path = joinPrefix(*p.ReplacePrefixMatch, trimPrefix(u.Path, match.path))
		u.RawPath = raw
	}
}

// trimPrefix returns the path after a prefix that matches with prefixMatch.
func trimPrefix(path, prefix string) string {
	if prefix == "/" {
		return path
	}

	return path[len(prefix):]
}

// escapedRest returns the part of the escaped path after the decoded prefix.
func escapedRest(escaped, prefix string) (string, bool) {
	if prefix == "/" {
		return escaped, true
	}

	for i := 1; i <= len(escaped); i++ {
		if i < len(escaped) && escaped[i] != '/' {
			continue
		}

		if dec, err := url.PathUnescape(escaped[:i]); err == nil && dec == prefix {
			return escaped[i:], true
		}
	}

	return "", false
}

func joinPrefix(replacement, rest string) string {
	if strings.HasSuffix(replacement, "/") && strings.HasPrefix(rest, "/") {
		rest = rest[1:]
	}

	if replacement == "" && !strings.HasPrefix(rest, "/") {
		replacement = "/"
	}

	out := replacement + rest
	if out == "" {
		return "/"
	}

	return out
}

// effectiveMatch returns the match used by path modifiers. Filters of a rule
// without a path, like the per backend filters generated by the Kubernetes
// provider, use the path match of the outer gateway.
func effectiveMatch(r *http.Request, own compiledMatch) compiledMatch {
	if own.explicit {
		return own
	}

	if outer := matchFrom(r); outer != nil {
		return *outer
	}

	return own
}

func (rw *Rewrite) middleware(match compiledMatch) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rw.Hostname != "" {
				r.Host = rw.Hostname
			}

			if rw.Path != nil {
				u := *r.URL
				rw.Path.rewrite(&u, effectiveMatch(r, match))
				r.URL = &u
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (rd *Redirect) middleware(match compiledMatch, listenerPort int) (func(http.Handler) http.Handler, error) {
	code := rd.StatusCode
	switch code {
	case 0:
		code = http.StatusFound
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
	default:
		return nil, fmt.Errorf("unsupported redirect status code %d", code)
	}

	return func(_ http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scheme := rd.Scheme
			if scheme == "" {
				scheme = httputil.Scheme(r)
			}

			host := rd.Hostname
			if host == "" {
				host = r.Host
				if h, _, err := net.SplitHostPort(r.Host); err == nil {
					host = h
				}
			}

			port := rd.Port
			if port == 0 && rd.Scheme == "" {
				port = listenerPort
			}

			if port != 0 && port != wellKnownPort(scheme) {
				host = net.JoinHostPort(host, strconv.Itoa(port))
			}

			u := *r.URL
			u.Scheme = scheme
			u.Host = host
			rd.Path.rewrite(&u, effectiveMatch(r, match))

			http.Redirect(w, r, u.String(), code)
		})
	}, nil
}

func wellKnownPort(scheme string) int {
	switch scheme {
	case "https":
		return 443
	default:
		return 80
	}
}

// normalizePath removes "." and ".." segments, also percent encoded ones, so
// a request like "/public/../admin" can not match "/public" and reach
// "/admin" on an upstream that normalizes the path itself.
func normalizePath(r *http.Request) *http.Request {
	if !hasDotSegment(r.URL.Path) {
		return r
	}

	u := *r.URL
	u.Path = removeDotSegments(u.Path)
	u.RawPath = ""

	r2 := new(http.Request)
	*r2 = *r
	r2.URL = &u

	return r2
}

func hasDotSegment(p string) bool {
	for seg := range strings.SplitSeq(p, "/") {
		if seg == "." || seg == ".." {
			return true
		}
	}

	return false
}

// removeDotSegments implements RFC 3986 section 5.2.4 for absolute paths.
func removeDotSegments(p string) string {
	segs := strings.Split(p, "/")
	out := make([]string, 0, len(segs))

	for i, seg := range segs {
		last := i == len(segs)-1

		switch seg {
		case ".":
			if last {
				out = append(out, "")
			}
		case "..":
			if len(out) > 1 {
				out = out[:len(out)-1]
			}

			if last {
				out = append(out, "")
			}
		default:
			out = append(out, seg)
		}
	}

	res := strings.Join(out, "/")
	if !strings.HasPrefix(res, "/") {
		res = "/" + res
	}

	return res
}
