package ldapview

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
)

type verifyRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	// Filter switches to search-then-bind: %s is replaced with the escaped
	// username, e.g. "(uid=%s)", and the entry found gives the bind DN.
	Filter string `json:"filter"`
	// BaseDN for search-then-bind; defaults to the session base DN.
	BaseDN string `json:"base_dn"`
	// Attributes to return from the user entry after a successful bind.
	Attributes []string `json:"attributes"`
}

type verifyStep struct {
	Name     string  `json:"name"`
	OK       bool    `json:"ok"`
	Detail   string  `json:"detail,omitempty"`
	Code     int     `json:"code,omitempty"`
	Duration float64 `json:"duration_ms"`
}

type verifyResponse struct {
	OK     bool         `json:"ok"`
	BindDN string       `json:"bind_dn,omitempty"`
	Steps  []verifyStep `json:"steps"`
	Entry  *entry       `json:"entry,omitempty"`
}

// apiVerify checks another user's password the way an application would,
// on a fresh connection so the session's bind is not changed. The search
// step of search-then-bind runs on the session connection.
func (m *LdapView) apiVerify(w http.ResponseWriter, r *http.Request, sess *session) {
	var req verifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))

		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		// an empty password would be an unauthenticated bind and "succeed" on many servers
		writeError(w, http.StatusBadRequest, errors.New("username and password are required"))

		return
	}

	res := verifyResponse{Steps: []verifyStep{}}

	step := func(name string, start time.Time, err error, detail string) bool {
		s := verifyStep{Name: name, OK: err == nil, Detail: detail, Duration: float64(time.Since(start).Microseconds()) / 1000}
		if err != nil {
			s.Detail = errText(err)

			var lerr *ldap.Error
			if errors.As(err, &lerr) {
				s.Code = int(lerr.ResultCode)
			}
		}

		res.Steps = append(res.Steps, s)

		return err == nil
	}

	bindDN := userDN(sess.template, req.Username)

	if filter := strings.TrimSpace(req.Filter); filter != "" {
		start := time.Now()
		dn, detail, err := m.findUser(sess, req, filter)
		if !step("Find user", start, err, detail) {
			writeJSON(w, http.StatusOK, res)

			return
		}

		bindDN = dn
	}

	res.BindDN = bindDN

	start := time.Now()
	opts := sess.dial
	opts.Mode, opts.Username, opts.Password = BindSimple, bindDN, req.Password

	conn, err := m.connect(opts)
	if !step("Bind", start, err, "as "+bindDN) {
		writeJSON(w, http.StatusOK, res)

		return
	}
	defer conn.Close()

	res.OK = true

	start = time.Now()
	if who, err := conn.WhoAmI(nil); err == nil {
		step("Who am I", start, nil, who.AuthzID)
	}

	// read the entry as the user itself, which also shows what that user may see
	attrs := req.Attributes
	if len(attrs) == 0 {
		attrs = []string{"*"}
	}

	start = time.Now()
	search := ldap.NewSearchRequest(bindDN, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, int(m.MaxTimeLimit.Seconds()), false, "(objectClass=*)", attrs, nil)
	if sr, err := conn.Search(search); err == nil && len(sr.Entries) == 1 {
		e, _ := toEntry(sr.Entries[0], 100)
		res.Entry = &e
		step("Read own entry", start, nil, fmt.Sprintf("%d attributes", len(e.Attributes)))
	} else if err != nil {
		step("Read own entry", start, err, "")
	}

	writeJSON(w, http.StatusOK, res)
}

func (m *LdapView) findUser(sess *session, req verifyRequest, filter string) (string, string, error) {
	if strings.Count(filter, "%s") == 0 {
		return "", "", errors.New("filter needs %s where the username goes, e.g. (uid=%s)")
	}

	filter = strings.ReplaceAll(filter, "%s", ldap.EscapeFilter(req.Username))
	if _, err := ldap.CompileFilter(filter); err != nil {
		return "", "", fmt.Errorf("invalid filter: %w", err)
	}

	base := strings.TrimSpace(req.BaseDN)
	if base == "" {
		base = sess.info.BaseDN
	}

	search := ldap.NewSearchRequest(base, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 2, int(m.MaxTimeLimit.Seconds()), false, filter, []string{"1.1"}, nil)

	sr, err := sess.conn.Search(search)
	if err != nil && !ldap.IsErrorWithCode(err, ldap.LDAPResultSizeLimitExceeded) {
		return "", "", err
	}

	if sr == nil {
		sr = &ldap.SearchResult{}
	}

	switch len(sr.Entries) {
	case 0:
		return "", "", fmt.Errorf("no entry matches %s under %q with this session's bind", filter, base)
	case 1:
		return sr.Entries[0].DN, fmt.Sprintf("%s → %s", filter, sr.Entries[0].DN), nil
	default:
		return "", "", fmt.Errorf("%s matches more than one entry; an application would refuse to log in", filter)
	}
}
