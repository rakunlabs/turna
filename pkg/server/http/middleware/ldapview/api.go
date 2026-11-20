package ldapview

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-ldap/ldap/v3"
)

const maxBodySize = 1 << 20

type serverInfo struct {
	Name     string `json:"name"`
	Addr     string `json:"addr"`
	BaseDN   string `json:"base_dn,omitempty"`
	StartTLS bool   `json:"start_tls,omitempty"`
}

type infoResponse struct {
	Title        string       `json:"title"`
	Servers      []serverInfo `json:"servers"`
	BindModes    []string     `json:"bind_modes"`
	MaxSizeLimit int          `json:"max_size_limit"`
	MaxTimeLimit int          `json:"max_time_limit"`
	Session      *sessionInfo `json:"session,omitempty"`
}

type loginRequest struct {
	// Server is the name of a configured server.
	Server string `json:"server"`
	// Addr is any LDAP URL, used when Server is empty.
	Addr               string `json:"addr"`
	StartTLS           bool   `json:"start_tls"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
	// CA is PEM trusted in addition to the system CAs, for a custom server.
	CA       string `json:"ca"`
	BaseDN   string `json:"base_dn"`
	Mode     string `json:"mode"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type searchRequest struct {
	BaseDN     string   `json:"base_dn"`
	Scope      string   `json:"scope"`
	Filter     string   `json:"filter"`
	Attributes []string `json:"attributes"`
	SizeLimit  int      `json:"size_limit"`
	TimeLimit  int      `json:"time_limit"`
	Deref      string   `json:"deref"`
	TypesOnly  bool     `json:"types_only"`
	// PageSize > 0 uses the paged results control, needed for servers such
	// as Active Directory that cap a single response.
	PageSize int `json:"page_size"`
	// MaxValues > 0 cuts each attribute to that many values; Total keeps the count.
	MaxValues int `json:"max_values"`
}

type attribute struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
	// Binary marks values sent as base64.
	Binary bool `json:"binary,omitempty"`
	// Total is the value count when Values was cut by max_values.
	Total int `json:"total,omitempty"`
}

type entry struct {
	DN         string      `json:"dn"`
	Attributes []attribute `json:"attributes"`
}

type searchResponse struct {
	Entries   []entry  `json:"entries"`
	Referrals []string `json:"referrals,omitempty"`
	Duration  float64  `json:"duration_ms"`
	// Truncated is set when a size or time limit stopped the search.
	Truncated bool   `json:"truncated,omitempty"`
	Warning   string `json:"warning,omitempty"`
}

type child struct {
	DN          string   `json:"dn"`
	RDN         string   `json:"rdn"`
	HasChildren *bool    `json:"has_children,omitempty"`
	ObjectClass []string `json:"object_class,omitempty"`
}

func (m *LdapView) serveAPI(w http.ResponseWriter, r *http.Request, endpoint string) {
	if r.Method == http.MethodPost {
		if crossSite(r) {
			writeError(w, http.StatusForbidden, errors.New("cross-site request refused"))

			return
		}

		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			writeError(w, http.StatusUnsupportedMediaType, errors.New("content type must be application/json"))

			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	}

	switch {
	case endpoint == "info" && r.Method == http.MethodGet:
		m.apiInfo(w, r)
	case endpoint == "login" && r.Method == http.MethodPost:
		m.apiLogin(w, r)
	case endpoint == "logout" && r.Method == http.MethodPost:
		m.apiLogout(w, r)
	case endpoint == "search" && r.Method == http.MethodPost:
		m.withSession(w, r, m.apiSearch)
	case endpoint == "children" && r.Method == http.MethodGet:
		m.withSession(w, r, m.apiChildren)
	case endpoint == "entry" && r.Method == http.MethodGet:
		m.withSession(w, r, m.apiEntry)
	case endpoint == "rootdse" && r.Method == http.MethodGet:
		m.withSession(w, r, m.apiRootDSE)
	case endpoint == "whoami" && r.Method == http.MethodGet:
		m.withSession(w, r, m.apiWhoAmI)
	case endpoint == "verify" && r.Method == http.MethodPost:
		m.withSession(w, r, m.apiVerify)
	default:
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown endpoint %s %s", r.Method, endpoint))
	}
}

func (m *LdapView) session(r *http.Request) *session {
	c, err := r.Cookie(m.CookieName)
	if err != nil || c.Value == "" {
		return nil
	}

	return m.sessions.get(c.Value)
}

func (m *LdapView) withSession(w http.ResponseWriter, r *http.Request, fn func(http.ResponseWriter, *http.Request, *session)) {
	sess := m.session(r)
	if sess == nil {
		writeError(w, http.StatusUnauthorized, errors.New("not logged in or the LDAP connection was closed"))

		return
	}

	fn(w, r, sess)
}

func (m *LdapView) cookiePath() string {
	return m.PrefixPath + "/"
}

func (m *LdapView) apiInfo(w http.ResponseWriter, r *http.Request) {
	res := infoResponse{
		Title:        m.Title,
		Servers:      make([]serverInfo, 0, len(m.Servers)),
		BindModes:    m.bindModes(),
		MaxSizeLimit: m.MaxSizeLimit,
		MaxTimeLimit: int(m.MaxTimeLimit.Seconds()),
	}

	for _, s := range m.Servers {
		res.Servers = append(res.Servers, serverInfo{Name: s.Name, Addr: s.Addr, BaseDN: s.BaseDN, StartTLS: s.StartTLS})
	}

	if sess := m.session(r); sess != nil {
		info := sess.info
		res.Session = &info
	}

	writeJSON(w, http.StatusOK, res)
}

func (m *LdapView) apiLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))

		return
	}

	if !slices.Contains(m.bindModes(), req.Mode) {
		writeError(w, http.StatusBadRequest, fmt.Errorf("bind mode %q is not allowed", req.Mode))

		return
	}

	opts := connectOptions{Mode: req.Mode}
	info := sessionInfo{Mode: req.Mode, BaseDN: strings.TrimSpace(req.BaseDN), Since: time.Now()}
	template := ""

	if req.Server != "" {
		idx := slices.IndexFunc(m.Servers, func(s Server) bool { return s.Name == req.Server })
		if idx < 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("unknown server %q", req.Server))

			return
		}

		s := m.Servers[idx]
		opts.Addr, opts.StartTLS, opts.InsecureSkipVerify = s.Addr, s.StartTLS, s.InsecureSkipVerify
		opts.RootCAs = s.rootCAs
		info.Server = s.Name
		template = s.UserDNTemplate

		if info.BaseDN == "" {
			info.BaseDN = s.BaseDN
		}
	} else {
		opts.Addr = strings.TrimSpace(req.Addr)
		opts.StartTLS, opts.InsecureSkipVerify = req.StartTLS, req.InsecureSkipVerify

		pool, err := certPool([]byte(req.CA))
		if err != nil {
			writeError(w, http.StatusBadRequest, err)

			return
		}

		opts.RootCAs = pool
		info.Server = opts.Addr
	}

	info.Addr, info.StartTLS = opts.Addr, opts.StartTLS
	info.Insecure, info.CustomCA = opts.InsecureSkipVerify, opts.RootCAs != nil

	switch req.Mode {
	case BindSimple, BindUnauthenticated:
		opts.Username = userDN(template, strings.TrimSpace(req.Username))
		opts.Password = req.Password
		info.BindDN = opts.Username
	}

	if req.Mode == BindUnauthenticated {
		opts.Password = ""
	}

	conn, err := m.connect(opts)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err)

		return
	}

	if old := m.session(r); old != nil {
		m.sessions.remove(old.id)
	}

	dial := connectOptions{Addr: opts.Addr, StartTLS: opts.StartTLS, InsecureSkipVerify: opts.InsecureSkipVerify, RootCAs: opts.RootCAs}
	sess := m.sessions.add(conn, info, dial, template)

	http.SetCookie(w, &http.Cookie{
		Name:     m.CookieName,
		Value:    sess.id,
		Path:     m.cookiePath(),
		HttpOnly: true,
		Secure:   m.SecureCookie || r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})

	writeJSON(w, http.StatusOK, info)
}

func (m *LdapView) apiLogout(w http.ResponseWriter, r *http.Request) {
	if sess := m.session(r); sess != nil {
		m.sessions.remove(sess.id)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     m.CookieName,
		Value:    "",
		Path:     m.cookiePath(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.SecureCookie || r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})

	w.WriteHeader(http.StatusNoContent)
}

var scopes = map[string]int{
	"":         ldap.ScopeWholeSubtree,
	"sub":      ldap.ScopeWholeSubtree,
	"one":      ldap.ScopeSingleLevel,
	"base":     ldap.ScopeBaseObject,
	"children": ldap.ScopeChildren,
}

var derefs = map[string]int{
	"":       ldap.NeverDerefAliases,
	"never":  ldap.NeverDerefAliases,
	"search": ldap.DerefInSearching,
	"find":   ldap.DerefFindingBaseObj,
	"always": ldap.DerefAlways,
}

func (m *LdapView) apiSearch(w http.ResponseWriter, r *http.Request, sess *session) {
	var req searchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))

		return
	}

	scope, ok := scopes[req.Scope]
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Errorf("unknown scope %q, use base, one, sub or children", req.Scope))

		return
	}

	deref, ok := derefs[req.Deref]
	if !ok {
		writeError(w, http.StatusBadRequest, fmt.Errorf("unknown deref %q, use never, search, find or always", req.Deref))

		return
	}

	filter := strings.TrimSpace(req.Filter)
	if filter == "" {
		filter = "(objectClass=*)"
	}

	if _, err := ldap.CompileFilter(filter); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid filter: %w", err))

		return
	}

	sizeLimit := req.SizeLimit
	if sizeLimit <= 0 || sizeLimit > m.MaxSizeLimit {
		sizeLimit = m.MaxSizeLimit
	}

	timeLimit := req.TimeLimit
	if maxT := int(m.MaxTimeLimit.Seconds()); timeLimit <= 0 || timeLimit > maxT {
		timeLimit = maxT
	}

	attrs := make([]string, 0, len(req.Attributes))
	for _, a := range req.Attributes {
		if a = strings.TrimSpace(a); a != "" {
			attrs = append(attrs, a)
		}
	}

	search := ldap.NewSearchRequest(strings.TrimSpace(req.BaseDN), scope, deref, sizeLimit, timeLimit, req.TypesOnly, filter, attrs, nil)

	start := time.Now()
	res, err := m.run(sess, search, req.PageSize, sizeLimit)
	m.writeSearch(w, res, err, time.Since(start), req.MaxValues)
}

// run searches, with the paged results control when pageSize > 0, and stops
// once limit entries are read.
func (m *LdapView) run(sess *session, req *ldap.SearchRequest, pageSize, limit int) (*ldap.SearchResult, error) {
	if pageSize <= 0 {
		return sess.conn.Search(req)
	}

	pageSize = min(pageSize, limit)
	paging := ldap.NewControlPaging(uint32(pageSize)) //nolint:gosec // bounded by limit
	req.Controls = []ldap.Control{paging}
	req.SizeLimit = 0

	out := &ldap.SearchResult{}

	for {
		res, err := sess.conn.Search(req)
		if res != nil {
			out.Entries = append(out.Entries, res.Entries...)
			out.Referrals = append(out.Referrals, res.Referrals...)
		}

		if err != nil {
			return out, err
		}

		if len(out.Entries) >= limit {
			out.Entries = out.Entries[:limit]

			// abandon the paged search on the server with an empty page
			paging.SetCookie(cookieOf(res))
			paging.PagingSize = 0
			if len(paging.Cookie) > 0 {
				_, _ = sess.conn.Search(req)
			}

			return out, ldap.NewError(ldap.LDAPResultSizeLimitExceeded, errors.New("size limit reached"))
		}

		cookie := cookieOf(res)
		if len(cookie) == 0 {
			return out, nil
		}

		paging.SetCookie(cookie)
	}
}

func cookieOf(res *ldap.SearchResult) []byte {
	if res == nil {
		return nil
	}

	if c, ok := ldap.FindControl(res.Controls, ldap.ControlTypePaging).(*ldap.ControlPaging); ok {
		return c.Cookie
	}

	return nil
}

func (m *LdapView) writeSearch(w http.ResponseWriter, res *ldap.SearchResult, err error, d time.Duration, maxValues int) {
	out := searchResponse{Entries: []entry{}, Duration: float64(d.Microseconds()) / 1000}

	if res != nil {
		size := 0
		for _, e := range res.Entries {
			entry, n := toEntry(e, maxValues)
			if size += n; size > m.MaxResponseSize {
				out.Truncated = true
				out.Warning = fmt.Sprintf("response size limit of %d bytes reached after %d entries", m.MaxResponseSize, len(out.Entries))

				break
			}

			out.Entries = append(out.Entries, entry)
		}

		out.Referrals = res.Referrals
	}

	if err != nil {
		switch {
		case ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded),
			ldap.IsErrorWithCode(err, ldap.LDAPResultTimeLimitExceeded),
			ldap.IsErrorWithCode(err, ldap.LDAPResultAdminLimitExceeded):
			out.Truncated = true
			out.Warning = err.Error()
		case ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject) && len(out.Entries) == 0:
			writeError(w, http.StatusNotFound, err)

			return
		case ldap.IsErrorWithCode(err, ldap.ErrorNetwork):
			writeError(w, http.StatusBadGateway, err)

			return
		default:
			writeError(w, http.StatusBadRequest, err)

			return
		}
	}

	writeJSON(w, http.StatusOK, out)
}

// toEntry converts an entry and returns its approximate encoded size.
func toEntry(e *ldap.Entry, maxValues int) (entry, int) {
	out := entry{DN: e.DN, Attributes: make([]attribute, 0, len(e.Attributes))}
	size := len(e.DN) + 32

	for _, a := range e.Attributes {
		values := a.ByteValues
		attr := attribute{Name: a.Name}

		if maxValues > 0 && len(values) > maxValues {
			attr.Total = len(values)
			values = values[:maxValues]
		}

		attr.Values = make([]string, 0, len(values))

		for _, v := range values {
			if !printable(v) {
				attr.Binary = true

				break
			}
		}

		size += len(a.Name) + 32

		for _, v := range values {
			s := string(v)
			if attr.Binary {
				s = base64.StdEncoding.EncodeToString(v)
			}

			size += len(s) + 4
			attr.Values = append(attr.Values, s)
		}

		out.Attributes = append(out.Attributes, attr)
	}

	return out, size
}

func printable(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}

	for _, c := range b {
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
			return false
		}
	}

	return true
}

func (m *LdapView) apiEntry(w http.ResponseWriter, r *http.Request, sess *session) {
	dn := r.URL.Query().Get("dn")
	attrs := []string{"*"}

	if r.URL.Query().Has("operational") {
		attrs = append(attrs, "+")
	}

	req := ldap.NewSearchRequest(dn, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, int(m.MaxTimeLimit.Seconds()), false, "(objectClass=*)", attrs, nil)

	start := time.Now()
	res, err := sess.conn.Search(req)
	m.writeSearch(w, res, err, time.Since(start), 0)
}

func (m *LdapView) apiRootDSE(w http.ResponseWriter, _ *http.Request, sess *session) {
	req := ldap.NewSearchRequest("", ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, int(m.MaxTimeLimit.Seconds()), false, "(objectClass=*)", []string{"*", "+"}, nil)

	start := time.Now()
	res, err := sess.conn.Search(req)
	m.writeSearch(w, res, err, time.Since(start), 0)
}

func (m *LdapView) apiWhoAmI(w http.ResponseWriter, _ *http.Request, sess *session) {
	res, err := sess.conn.WhoAmI(nil)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)

		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"authz_id": res.AuthzID})
}

func (m *LdapView) apiChildren(w http.ResponseWriter, r *http.Request, sess *session) {
	dn := r.URL.Query().Get("dn")

	req := ldap.NewSearchRequest(dn, ldap.ScopeSingleLevel, ldap.NeverDerefAliases, m.MaxSizeLimit, int(m.MaxTimeLimit.Seconds()), false,
		"(objectClass=*)", []string{"objectClass", "hasSubordinates", "numSubordinates", "msDS-Approx-Immed-Subordinates"}, nil)

	res, err := sess.conn.Search(req)

	out := struct {
		Children  []child `json:"children"`
		Truncated bool    `json:"truncated,omitempty"`
	}{Children: []child{}}

	if err != nil {
		if !ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) || res == nil {
			status := http.StatusBadRequest
			if ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject) {
				status = http.StatusNotFound
			}

			writeError(w, status, err)

			return
		}

		out.Truncated = true
	}

	for _, e := range res.Entries {
		c := child{DN: e.DN, RDN: rdnOf(e.DN), ObjectClass: e.GetAttributeValues("objectClass")}

		switch {
		case e.GetAttributeValue("hasSubordinates") != "":
			v := strings.EqualFold(e.GetAttributeValue("hasSubordinates"), "TRUE")
			c.HasChildren = &v
		case e.GetAttributeValue("numSubordinates") != "":
			v := e.GetAttributeValue("numSubordinates") != "0"
			c.HasChildren = &v
		case e.GetAttributeValue("msDS-Approx-Immed-Subordinates") != "":
			v := e.GetAttributeValue("msDS-Approx-Immed-Subordinates") != "0"
			c.HasChildren = &v
		}

		out.Children = append(out.Children, c)
	}

	slices.SortFunc(out.Children, func(a, b child) int { return strings.Compare(strings.ToLower(a.RDN), strings.ToLower(b.RDN)) })

	writeJSON(w, http.StatusOK, out)
}

func rdnOf(dn string) string {
	parsed, err := ldap.ParseDN(dn)
	if err != nil || len(parsed.RDNs) == 0 {
		return dn
	}

	return parsed.RDNs[0].String()
}
