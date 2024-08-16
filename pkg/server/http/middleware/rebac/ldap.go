package rebac

import (
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-ldap/ldap/v3"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/worldline-go/initializer"
)

type Ldap struct {
	Addr string   `cfg:"addr"`
	Bind LdapBind `cfg:"bind"`

	Group      Group  `cfg:"group"`
	UserBaseDN string `cfg:"user_base_dn"`

	m sync.Mutex
}

type LdapBind struct {
	Simple Simple `cfg:"simple"`
}

type Simple struct {
	Username string `cfg:"username"`
	Password string `cfg:"password"`
}

type Group struct {
	Interval   time.Duration `cfg:"interval"`
	BaseDN     string        `cfg:"base_dn"`
	Filter     string        `cfg:"filter"`
	Attributes []string      `cfg:"attributes"`
}

type LdapUser struct {
	UID         string `json:"uid"`
	Email       string `json:"email"`
	Gecos       string `json:"gecos"`
	DisplayName string `json:"display_name"`
}

type LdapGroup struct {
	Name        string   `json:"name"`
	Members     []string `json:"members"`
	Description string   `json:"description"`
}

type LdapMap struct {
	Name string `json:"name" db:"name"`
	Map  string `json:"map"  db:"map"`
}

// func ldapUIDFilter(uids []string) string {
// 	filters := make([]string, 0, len(uids))
// 	for _, uid := range uids {
// 		filters = append(filters, fmt.Sprintf("(uid=%s)", uid))
// 	}

// 	return fmt.Sprintf("(|%s)", strings.Join(filters, ""))
// }

func (l *Ldap) ConnectLdap() (*ldap.Conn, error) {
	conn, err := ldap.DialURL(l.Addr)
	if err != nil {
		return nil, fmt.Errorf("failed connecting to LDAP server: %v", err)
	}

	initializer.Shutdown.Add(func() error {
		conn.Close()

		return nil
	})

	req := ldap.NewSimpleBindRequest(l.Bind.Simple.Username, l.Bind.Simple.Password, nil)
	_, err = conn.SimpleBind(req)
	if err != nil {
		return nil, fmt.Errorf("failed binding to LDAP server: %v", err)
	}

	slog.Info("LDAP connection established")

	return conn, nil
}

func (m *Rebac) ConnectLdapWithCheck() (*ldap.Conn, error) {
	m.Ldap.m.Lock()
	defer m.Ldap.m.Unlock()

	if m.ldapConn == nil || m.ldapConn.IsClosing() {
		conn, err := m.Ldap.ConnectLdap()
		if err != nil {
			return nil, err
		}

		m.ldapConn = conn
	}

	return m.ldapConn, nil
}

func LdapGroups(conn *ldap.Conn, sync Group) ([]LdapGroup, error) {
	result, err := conn.Search(&ldap.SearchRequest{
		BaseDN:     sync.BaseDN,
		Scope:      ldap.ScopeWholeSubtree,
		Filter:     sync.Filter,
		Attributes: sync.Attributes,
	})
	if err != nil {
		return nil, fmt.Errorf("failed searching LDAP server: %v", err)
	}

	groups := make([]LdapGroup, 0, len(result.Entries))

	for _, entry := range result.Entries {
		var group LdapGroup
		group.Name = entry.GetAttributeValue("cn")
		if group.Name == "" {
			continue
		}

		group.Members = make([]string, 0, len(entry.Attributes))

		for _, attr := range entry.GetAttributeValues("uniqueMember") {
			uid := ""
			_ = slices.ContainsFunc(strings.Split(attr, ","), func(v string) bool {
				if strings.Contains(v, "uid=") {
					uid = strings.TrimPrefix(v, "uid=")
					return true
				}
				return false
			})

			group.Members = append(group.Members, uid)
		}

		group.Description = entry.GetAttributeValue("description")

		groups = append(groups, group)
	}

	return groups, nil
}

// LdapGetGroups returns groups info from LDAP.
// @Summary Get LDAP groups
// @Tags ldap
// @Success 200 {object} []LdapGroup
// @Failure 500 {object} httputil.Error
// @Router /ldap/groups [GET]
func (m *Rebac) LdapGetGroups(w http.ResponseWriter, r *http.Request) {
	conn, err := m.ConnectLdapWithCheck()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	groups, err := LdapGroups(conn, m.Ldap.Group)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	httputil.JSON(w, http.StatusOK, groups)
}

// LdapGetUsers returns user info from LDAP.
// @Summary Get LDAP user
// @Tags ldap
// @Param uid path string true "user uid"
// @Success 200 {object} LdapUser
// @Failure 400 {object} httputil.Error
// @Failure 404 {object} httputil.Error
// @Failure 500 {object} httputil.Error
// @Router /ldap/users/{uid} [GET]
func (m *Rebac) LdapGetUsers(w http.ResponseWriter, r *http.Request) {
	conn, err := m.ConnectLdapWithCheck()
	if err != nil {
		httputil.HandleError(w, httputil.NewError("LDAP connection problem", err, http.StatusInternalServerError))
		return
	}

	uid := chi.URLParam(r, "uid")
	if uid == "" {
		httputil.HandleError(w, httputil.NewError("uid is required", nil, http.StatusBadRequest))
		return
	}

	result, err := conn.Search(&ldap.SearchRequest{
		BaseDN:       m.Ldap.UserBaseDN,
		Scope:        ldap.ScopeWholeSubtree,
		DerefAliases: ldap.NeverDerefAliases,
		TypesOnly:    false,
		Filter:       fmt.Sprintf("(uid=%s)", uid),
		Attributes:   []string{"uid", "mail", "gecos", "displayName"},
	})
	if err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject) {
			httputil.HandleError(w, httputil.NewError("user not found", nil, http.StatusNotFound))

			return
		}

		httputil.HandleError(w, httputil.NewError("failed searching LDAP server", err, http.StatusInternalServerError))

		return
	}

	if len(result.Entries) == 0 {
		httputil.HandleError(w, httputil.NewError("user not found", nil, http.StatusNotFound))

		return
	}

	entry := result.Entries[0]

	user := LdapUser{
		UID:         entry.GetAttributeValue("uid"),
		Email:       entry.GetAttributeValue("mail"),
		Gecos:       entry.GetAttributeValue("gecos"),
		DisplayName: entry.GetAttributeValue("displayName"),
	}

	httputil.JSON(w, http.StatusOK, user)
}

// LdapSyncGroups syncs users on LDAP groups with mapped groups in the database.
// @Summary Sync LDAP groups
// @Tags ldap
// @Success 501
// @Failure 500 {object} httputil.Error
// @Router /ldap/sync [POST]
func (m *Rebac) LdapSyncGroups(w http.ResponseWriter, r *http.Request) {
	httputil.HandleError(w, httputil.NewError("Not implemented", nil, http.StatusNotImplemented))
}
