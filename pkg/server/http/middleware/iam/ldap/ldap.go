package ldap

import (
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/rakunlabs/into"
	"github.com/worldline-go/turna/pkg/server/http/httputil"
)

var DefaultLdapSyncDuration = 10 * time.Minute

type Conn = ldap.Conn

type Ldap struct {
	DisableFirstConnect bool     `cfg:"disable_first_connect"`
	Addr                string   `cfg:"addr"`
	Bind                LdapBind `cfg:"bind"`

	Group      []Group `cfg:"group"`
	UserBaseDN string  `cfg:"user_base_dn"`

	SyncDuration time.Duration `cfg:"sync_duration"`
	DisableSync  bool          `cfg:"disable_sync"`

	conn     *ldap.Conn
	connUser *ldap.Conn
	mUser    sync.Mutex
	m        sync.Mutex
}

type LdapBind struct {
	Simple Simple `cfg:"simple"`
}

type Simple struct {
	Username string `cfg:"username"`
	Password string `cfg:"password"`
}

type Group struct {
	BaseDN     string   `cfg:"base_dn"`
	Filter     string   `cfg:"filter"`
	Attributes []string `cfg:"attributes"`
}

type LdapUser struct {
	UID        string `json:"uid"`
	Email      string `json:"email"`
	Name       string `json:"name"`
	GivenName  string `json:"given_name"`
	FamilyName string `json:"family_name"`
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

func ldapUserFilter(uids []string) string {
	filters := make([]string, 0, len(uids)*2)
	for _, uid := range uids {
		filters = append(filters, fmt.Sprintf("(uid=%s)", uid), fmt.Sprintf("(mail=%s)", uid))
	}

	return fmt.Sprintf("(|%s)", strings.Join(filters, ""))
}

func (l *Ldap) Connect() (*ldap.Conn, error) {
	conn, err := ldap.DialURL(l.Addr)
	if err != nil {
		return nil, fmt.Errorf("failed connecting to LDAP server: %w", err)
	}

	into.ShutdownAdd(conn.Close, "ldap")

	req := ldap.NewSimpleBindRequest(l.Bind.Simple.Username, l.Bind.Simple.Password, nil)
	_, err = conn.SimpleBind(req)
	if err != nil {
		return nil, fmt.Errorf("failed binding to LDAP server: %w", err)
	}

	slog.Info("LDAP connection established")

	return conn, nil
}

func (l *Ldap) ConnectWithCheck() (*ldap.Conn, error) {
	l.m.Lock()
	defer l.m.Unlock()

	if l.conn == nil || l.conn.IsClosing() {
		conn, err := l.Connect()
		if err != nil {
			return nil, err
		}

		l.conn = conn
	}

	return l.conn, nil
}

func (l *Ldap) Groups(conn *ldap.Conn) ([]LdapGroup, error) {
	groups := make([]LdapGroup, 0)

	for _, groupCfg := range l.Group {
		groupsGet, err := l.groups(conn, groupCfg)
		if err != nil {
			return nil, err
		}

		groups = append(groups, groupsGet...)
	}

	return groups, nil
}

func (l *Ldap) groups(conn *ldap.Conn, groupCfg Group) ([]LdapGroup, error) {
	result, err := conn.Search(&ldap.SearchRequest{
		BaseDN:     groupCfg.BaseDN,
		Scope:      ldap.ScopeWholeSubtree,
		Filter:     groupCfg.Filter,
		Attributes: groupCfg.Attributes,
	})
	if err != nil {
		return nil, fmt.Errorf("failed searching LDAP server: %w", err)
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

func (l *Ldap) Users(conn *ldap.Conn, uids []string) ([]LdapUser, error) {
	result, err := conn.Search(&ldap.SearchRequest{
		BaseDN:       l.UserBaseDN,
		Scope:        ldap.ScopeWholeSubtree,
		DerefAliases: ldap.NeverDerefAliases,
		TypesOnly:    false,
		Filter:       ldapUserFilter(uids),
		Attributes:   []string{"uid", "mail", "gecos", "displayName", "sn", "givenName"},
	})
	if err != nil {
		if ldap.IsErrorWithCode(err, ldap.LDAPResultNoSuchObject) {
			return nil, httputil.NewError("user not found", err, http.StatusNotFound)
		}

		return nil, httputil.NewError("failed searching LDAP server", err, http.StatusInternalServerError)
	}

	if len(result.Entries) == 0 {
		return nil, httputil.NewError("user not found", nil, http.StatusNotFound)
	}

	ldapUsers := make([]LdapUser, 0, len(result.Entries))
	for _, entry := range result.Entries {
		user := LdapUser{
			UID:        entry.GetAttributeValue("uid"),
			Email:      entry.GetAttributeValue("mail"),
			Name:       entry.GetAttributeValue("gecos"),
			GivenName:  entry.GetAttributeValue("givenName"),
			FamilyName: entry.GetAttributeValue("sn"),
		}

		ldapUsers = append(ldapUsers, user)
	}

	return ldapUsers, nil
}
