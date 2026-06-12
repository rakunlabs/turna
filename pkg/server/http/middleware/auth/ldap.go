package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/rakunlabs/logi"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/ldap"
	"github.com/xhit/go-str2duration/v2"
)

var DefaultLDAPSyncDuration = 10 * time.Minute

type ldapManager struct {
	m       sync.Mutex
	version uint64
	runtime *ldap.Ldap
	cfg     *LDAPSettings
}

func (s LDAPSettings) runtime() *ldap.Ldap {
	groups := make([]ldap.Group, 0, len(s.Groups))
	for _, g := range s.Groups {
		attributes := g.Attributes
		if len(attributes) == 0 {
			attributes = []string{"cn", "uniqueMember", "description"}
		}

		groups = append(groups, ldap.Group{
			BaseDN:     g.BaseDN,
			Filter:     g.Filter,
			Attributes: attributes,
		})
	}

	l := &ldap.Ldap{
		Addr:                s.Addr,
		UserBaseDN:          s.UserBaseDN,
		Group:               groups,
		DisableSync:         s.DisableSync,
		DisableFirstConnect: true,
	}

	l.Bind.Simple.Username = s.Bind.Username
	l.Bind.Simple.Password = s.Bind.Password

	if s.SyncDuration != "" {
		if d, err := str2duration.ParseDuration(s.SyncDuration); err == nil {
			l.SyncDuration = d
		}
	}
	if l.SyncDuration == 0 {
		l.SyncDuration = DefaultLDAPSyncDuration
	}

	return l
}

// ldapRuntime returns the LDAP runtime built from the first enabled stored config.
func (m *Auth) ldapRuntime() *ldap.Ldap {
	sn := m.cache.Snapshot()

	m.ldap.m.Lock()
	defer m.ldap.m.Unlock()

	if m.ldap.runtime != nil && m.ldap.version == sn.Version {
		return m.ldap.runtime
	}

	if len(sn.LDAP) == 0 {
		m.ldap.runtime = nil
		m.ldap.cfg = nil
		m.ldap.version = sn.Version

		return nil
	}

	cfg := sn.LDAP[0]
	m.ldap.runtime = cfg.runtime()
	m.ldap.cfg = &cfg
	m.ldap.version = sn.Version

	return m.ldap.runtime
}

func (m *Auth) ldapEnabled() bool {
	return m.ldapRuntime() != nil
}

func (m *Auth) LdapCheckPassword(username, password string) (bool, error) {
	runtime := m.ldapRuntime()
	if runtime == nil {
		return false, errors.New("ldap is not configured")
	}

	return runtime.CheckPassword(username, password)
}

// LdapSync syncs LDAP groups and users into the store.
// When uid is set, only that user is synced.
func (m *Auth) LdapSync(ctx context.Context, force bool, uid string) error {
	runtime := m.ldapRuntime()
	if runtime == nil {
		return errors.New("ldap is not configured")
	}

	m.ldapSyncM.Lock()
	defer m.ldapSyncM.Unlock()

	ctx = data.WithContextUserName(ctx, "LDAP")

	logi.Ctx(ctx).Info("syncing ldap starting")
	defer logi.Ctx(ctx).Info("syncing ldap done")

	conn, err := runtime.ConnectWithCheck()
	if err != nil {
		return fmt.Errorf("ldap connection problem: %w", err)
	}

	groups, err := runtime.Groups(conn)
	if err != nil {
		return fmt.Errorf("failed getting groups: %w", err)
	}

	users := make(map[string][]string)
	if uid != "" {
		users[uid] = nil
	}

	lmapGroups := make([]data.LMapCheckCreate, 0, len(groups))
	for _, group := range groups {
		lmapGroups = append(lmapGroups, data.LMapCheckCreate{
			Name:        group.Name,
			Description: group.Description,
		})

		for _, member := range group.Members {
			if member == "" {
				continue
			}

			if uid != "" && member != uid {
				continue
			}

			users[member] = append(users[member], group.Name)
		}
	}

	if err := m.store.EnsureLMaps(ctx, lmapGroups); err != nil {
		return fmt.Errorf("failed creating roles: %w", err)
	}

	if err := m.cache.Reload(ctx); err != nil {
		return fmt.Errorf("failed reloading cache: %w", err)
	}

	sn := m.cache.Snapshot()

	roleIDsFor := func(groupNames []string) []string {
		roleIDs := make([]string, 0, len(groupNames))
		for _, name := range groupNames {
			if lmap, ok := sn.LMaps[name]; ok {
				roleIDs = append(roleIDs, lmap.RoleIDs...)
			}
		}

		return slicesUnique(roleIDs)
	}

	for member, groupNames := range users {
		roleIDs := roleIDsFor(groupNames)

		userDB := sn.UserByAlias(member)
		if userDB != nil {
			if !data.CompareSlices(userDB.SyncRoleIDs, roleIDs) {
				if err := m.store.UpdateUserSyncRoles(ctx, userDB.ID, roleIDs); err != nil {
					return fmt.Errorf("failed updating user sync roles: %w", err)
				}
			}

			if !force {
				continue
			}

			usersLdap, err := runtime.Users(conn, []string{member})
			if err != nil || len(usersLdap) == 0 {
				continue
			}

			u := usersLdap[0]

			userPut := *userDB
			if userPut.Details == nil {
				userPut.Details = map[string]any{}
			}

			userPut.Alias = []string{u.Email, u.UID}
			userPut.Details["email"] = u.Email
			userPut.Details["uid"] = u.UID
			userPut.Details["name"] = u.Name
			userPut.Details["family_name"] = u.FamilyName
			userPut.Details["given_name"] = u.GivenName
			userPut.SyncRoleIDs = roleIDs

			if err := m.store.PutUser(ctx, userPut); err != nil {
				return fmt.Errorf("failed updating user: %w", err)
			}

			continue
		}

		// user not found, fetch and create
		usersLdap, err := runtime.Users(conn, []string{member})
		if err != nil || len(usersLdap) == 0 {
			continue
		}

		u := usersLdap[0]

		if _, err := m.store.CreateUser(ctx, data.User{
			SyncRoleIDs: roleIDs,
			Alias:       []string{u.Email, u.UID},
			Details: map[string]any{
				"email":       u.Email,
				"uid":         u.UID,
				"name":        u.Name,
				"family_name": u.FamilyName,
				"given_name":  u.GivenName,
			},
		}); err != nil {
			if errors.Is(err, data.ErrConflict) {
				continue
			}

			return fmt.Errorf("failed creating user: %w", err)
		}
	}

	// reset sync roles for users that left all groups
	if uid == "" {
		for _, id := range sn.UserIDs {
			user := sn.Users[id]
			if user.ServiceAccount || user.Local || len(user.SyncRoleIDs) == 0 {
				continue
			}

			found := false
			for _, alias := range user.Alias {
				if _, ok := users[alias]; ok {
					found = true
					break
				}
			}

			if found {
				continue
			}

			if err := m.store.UpdateUserSyncRoles(ctx, user.ID, nil); err != nil {
				return fmt.Errorf("failed clearing user sync roles: %w", err)
			}

			slog.Info("user sync roles cleared", slog.String("id", user.ID), slog.String("by", "LDAP"))
		}
	}

	return m.cache.Reload(ctx)
}

// watchLDAP periodically syncs LDAP when an enabled config exists.
func (m *Auth) watchLDAP(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	var lastSync time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runtime := m.ldapRuntime()
			if runtime == nil || runtime.DisableSync {
				continue
			}

			if time.Since(lastSync) < runtime.SyncDuration {
				continue
			}

			if err := m.LdapSync(ctx, false, ""); err != nil {
				slog.Error("ldap sync failed", slog.String("error", err.Error()))
			}

			lastSync = time.Now()
		}
	}
}

// ////////////////////////////////////////////////////////////////////
// handlers

func (m *Auth) LdapGetGroupsAPI(w http.ResponseWriter, r *http.Request) {
	runtime := m.ldapRuntime()
	if runtime == nil {
		httputil.HandleError(w, httputil.NewError("ldap is not configured", nil, http.StatusFailedDependency))
		return
	}

	conn, err := runtime.ConnectWithCheck()
	if err != nil {
		httputil.HandleError(w, httputil.NewError("ldap connection problem", err, http.StatusInternalServerError))
		return
	}

	groups, err := runtime.Groups(conn)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("failed getting groups", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[[]ldap.LdapGroup]{
		Meta:    &data.Meta{TotalItemCount: uint64(len(groups))},
		Payload: groups,
	})
}

func (m *Auth) LdapGetUserAPI(w http.ResponseWriter, r *http.Request) {
	runtime := m.ldapRuntime()
	if runtime == nil {
		httputil.HandleError(w, httputil.NewError("ldap is not configured", nil, http.StatusFailedDependency))
		return
	}

	conn, err := runtime.ConnectWithCheck()
	if err != nil {
		httputil.HandleError(w, httputil.NewError("ldap connection problem", err, http.StatusInternalServerError))
		return
	}

	uid := r.PathValue("uid")
	if uid == "" {
		httputil.HandleError(w, httputil.NewError("uid is required", nil, http.StatusBadRequest))
		return
	}

	uid, err = url.PathUnescape(uid)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("failed unescaping uid", err, http.StatusBadRequest))
		return
	}

	users, err := runtime.Users(conn, []string{uid})
	if err != nil {
		httputil.HandleError(w, httputil.NewErrorAs(err))
		return
	}

	httputil.JSON(w, http.StatusOK, data.Response[ldap.LdapUser]{Payload: users[0]})
}

type LdapSyncRequest struct {
	Force bool `json:"force"`
}

func (m *Auth) LdapSyncAPI(w http.ResponseWriter, r *http.Request) {
	var req LdapSyncRequest
	_ = httputil.Decode(r, &req)

	if err := m.LdapSync(userCtx(r), req.Force, ""); err != nil {
		httputil.HandleError(w, httputil.NewError("ldap sync failed", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("users synced"))
}

func (m *Auth) LdapSyncUIDAPI(w http.ResponseWriter, r *http.Request) {
	uid := r.PathValue("uid")
	if uid == "" {
		httputil.HandleError(w, httputil.NewError("uid is required", nil, http.StatusBadRequest))
		return
	}

	var req LdapSyncRequest
	_ = httputil.Decode(r, &req)

	if err := m.LdapSync(userCtx(r), req.Force, uid); err != nil {
		httputil.HandleError(w, httputil.NewError("ldap sync failed", err, http.StatusInternalServerError))
		return
	}

	httputil.JSON(w, http.StatusOK, data.NewResponseMessage("user synced"))
}
