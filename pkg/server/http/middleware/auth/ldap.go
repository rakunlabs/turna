package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rakunlabs/logi"
	"github.com/rakunlabs/turna/pkg/server/http/httputil"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/ldap"
	"github.com/xhit/go-str2duration/v2"
)

var DefaultLDAPSyncDuration = 10 * time.Minute

// ldapAliases builds the alias list from LDAP attributes, skipping empty
// values (e.g. a user without a mail attribute, which would otherwise add an
// empty-string alias that collides across users) and de-duplicating. The
// looked-up identifier is kept as a fallback so the user stays findable.
func ldapAliases(u ldap.LdapUser, fallback string) []string {
	aliases := make([]string, 0, 2)

	add := func(v string) {
		if v == "" {
			return
		}

		for _, existing := range aliases {
			if existing == v {
				return
			}
		}

		aliases = append(aliases, v)
	}

	add(u.Email)
	add(u.UID)

	if len(aliases) == 0 {
		add(fallback)
	}

	return aliases
}

// ldapDNKey stores the LDAP entry DN in user details; it is the stable
// identity used to match a stored user even when uid or mail change.
const ldapDNKey = "ldap_dn"

// ldapUserIndex resolves the stored user behind an LDAP entry.
type ldapUserIndex struct {
	sn   *Snapshot
	byDN map[string]*data.User
}

func newLDAPUserIndex(sn *Snapshot) ldapUserIndex {
	byDN := make(map[string]*data.User)
	for _, id := range sn.UserIDs {
		user := sn.Users[id]
		if dn, _ := user.Details[ldapDNKey].(string); dn != "" {
			byDN[strings.ToLower(dn)] = user
		}
	}

	return ldapUserIndex{sn: sn, byDN: byDN}
}

// find matches by DN first, then by the looked-up identifier and every
// alias the LDAP entry carries, so a user stored under uid is still found
// when the lookup comes in with a mail added later.
func (x ldapUserIndex) find(member string, u *ldap.LdapUser) *data.User {
	if u != nil && u.DN != "" {
		if user := x.byDN[strings.ToLower(u.DN)]; user != nil && ldapManaged(user) {
			return user
		}
	}

	candidates := []string{member}
	if u != nil {
		candidates = append(candidates, ldapAliases(*u, "")...)
	}

	for _, alias := range candidates {
		if alias == "" {
			continue
		}

		if user := x.sn.UserByAlias(alias); user != nil && ldapManaged(user) {
			return user
		}
	}

	return nil
}

// ldapManaged reports whether a stored user may be bound to an LDAP entry.
// Local accounts (e.g. self signup with a directory user's email) and
// service accounts must never receive directory roles through alias matching.
func ldapManaged(user *data.User) bool {
	return !user.ServiceAccount && !user.Local
}

// ldapMergeAliases returns the user's aliases updated from the LDAP entry:
// previously synced uid/mail values that LDAP no longer reports are dropped,
// current ones are added, manually assigned aliases are kept. Aliases owned
// by another user are skipped and returned as conflicts.
func ldapMergeAliases(sn *Snapshot, user *data.User, u ldap.LdapUser) (aliases, conflicts []string) {
	current := normalizedAliases(ldapAliases(u, ""))

	stale := make(map[string]struct{}, 2)
	for _, key := range []string{"email", "uid"} {
		if v, _ := user.Details[key].(string); normalizeAlias(v) != "" && !slices.Contains(current, normalizeAlias(v)) {
			stale[normalizeAlias(v)] = struct{}{}
		}
	}

	aliases = make([]string, 0, len(user.Alias)+len(current))
	for _, alias := range user.Alias {
		alias = normalizeAlias(alias)
		if _, ok := stale[alias]; ok {
			continue
		}

		aliases = append(aliases, alias)
	}

	for _, alias := range current {
		if owner := sn.UserByAlias(alias); owner != nil && owner.ID != user.ID {
			conflicts = append(conflicts, alias)
			continue
		}

		aliases = append(aliases, alias)
	}

	// never leave a user without any alias
	if len(aliases) == 0 {
		aliases = append(aliases, user.Alias...)
	}

	return slicesUnique(aliases), conflicts
}

// ldapUpdateUser refreshes aliases and details of an existing user from its
// LDAP entry. Without force it only writes when the aliases or DN changed.
func (m *Auth) ldapUpdateUser(ctx context.Context, sn *Snapshot, user *data.User, u ldap.LdapUser, roleIDs []string, force bool) error {
	aliases, conflicts := ldapMergeAliases(sn, user, u)
	if len(conflicts) > 0 {
		slog.Warn("ldap alias owned by another user, skipped",
			slog.String("id", user.ID), slog.Any("aliases", conflicts), slog.String("dn", u.DN))
	}

	dn, _ := user.Details[ldapDNKey].(string)
	changed := !data.CompareSlices(user.Alias, aliases) || (u.DN != "" && dn != u.DN)
	if !changed && !force {
		return nil
	}

	if err := m.store.UpdateUserLDAP(ctx, user.ID, aliases, ldapDetails(nil, u), roleIDs); err != nil {
		if errors.Is(err, data.ErrConflict) {
			slog.Warn("ldap user update conflict", slog.String("id", user.ID), slog.String("error", err.Error()))
			return nil
		}

		return fmt.Errorf("failed updating user: %w", err)
	}

	if changed {
		slog.Info("ldap user aliases updated", slog.String("id", user.ID), slog.Any("aliases", aliases))
	}

	return nil
}

// ldapDetails returns a copy of details with the LDAP attributes applied;
// the snapshot map is shared and must not be mutated.
func ldapDetails(details map[string]any, u ldap.LdapUser) map[string]any {
	out := make(map[string]any, len(details)+6)
	for k, v := range details {
		out[k] = v
	}

	out["email"] = u.Email
	out["uid"] = u.UID
	out["name"] = u.Name
	out["family_name"] = u.FamilyName
	out["given_name"] = u.GivenName
	if u.DN != "" {
		out[ldapDNKey] = u.DN
	}

	return out
}

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

// ldapRuntime returns the LDAP runtime built from the first enabled stored config
// with instance-local address overrides applied.
func (m *Auth) ldapRuntime() *ldap.Ldap {
	sn := m.cache.Snapshot()

	m.ldap.m.Lock()
	defer m.ldap.m.Unlock()

	if m.ldap.runtime != nil && m.ldap.version == sn.Version {
		return m.ldap.runtime
	}

	if len(sn.LDAP) == 0 {
		if m.ldap.runtime != nil {
			m.ldap.runtime.Close()
		}
		m.ldap.runtime = nil
		m.ldap.cfg = nil
		m.ldap.version = sn.Version

		return nil
	}

	cfg := sn.LDAP[0]
	if m.LDAP.Addr != "" {
		cfg.Addr = m.LDAP.Addr
	}

	// every IAM write bumps the snapshot version; keep the runtime (and its
	// open connections) unless the effective LDAP config actually changed
	if m.ldap.runtime != nil && m.ldap.cfg != nil && reflect.DeepEqual(*m.ldap.cfg, cfg) {
		m.ldap.version = sn.Version

		return m.ldap.runtime
	}

	if m.ldap.runtime != nil {
		m.ldap.runtime.Close()
	}

	m.ldap.runtime = cfg.runtime()
	m.ldap.cfg = &cfg
	m.ldap.version = sn.Version

	return m.ldap.runtime
}

func (m *Auth) ldapEnabled() bool {
	return m.ldapRuntime() != nil
}

func (m *Auth) closeLDAP() error {
	m.ldap.m.Lock()
	defer m.ldap.m.Unlock()

	if m.ldap.runtime != nil {
		m.ldap.runtime.Close()
	}

	return nil
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

	users := make(map[string][]string)
	ldapUsers := make(map[string]ldap.LdapUser)
	var lmapGroups []data.LMapCheckCreate

	if uid != "" {
		usersGet, err := runtime.Users(conn, []string{uid})
		if err != nil {
			return fmt.Errorf("failed getting user: %w", err)
		}
		if len(usersGet) == 0 {
			return errors.New("ldap user not found")
		}

		ldapUsers[uid] = usersGet[0]
		users[uid] = nil

		groups, err := runtime.GroupsForUser(conn, usersGet[0].DN)
		if err != nil {
			return fmt.Errorf("failed getting user groups: %w", err)
		}

		lmapGroups = make([]data.LMapCheckCreate, 0, len(groups))
		for _, group := range groups {
			lmapGroups = append(lmapGroups, data.LMapCheckCreate{
				Name:        group.Name,
				Description: group.Description,
			})
			users[uid] = append(users[uid], group.Name)
		}
	} else {
		groups, err := runtime.Groups(conn)
		if err != nil {
			return fmt.Errorf("failed getting groups: %w", err)
		}

		lmapGroups = make([]data.LMapCheckCreate, 0, len(groups))
		for _, group := range groups {
			lmapGroups = append(lmapGroups, data.LMapCheckCreate{
				Name:        group.Name,
				Description: group.Description,
			})

			for _, member := range group.Members {
				if member == "" {
					continue
				}

				users[member] = append(users[member], group.Name)
			}
		}
	}

	if err := m.store.EnsureLMaps(ctx, lmapGroups); err != nil {
		return fmt.Errorf("failed creating roles: %w", err)
	}

	if err := m.cache.Reload(ctx); err != nil {
		return fmt.Errorf("failed reloading cache: %w", err)
	}

	sn := m.cache.Snapshot()
	index := newLDAPUserIndex(sn)

	ldapUser := func(member string) (ldap.LdapUser, bool) {
		if user, ok := ldapUsers[member]; ok {
			return user, true
		}

		usersGet, err := runtime.Users(conn, []string{member})
		if err != nil || len(usersGet) == 0 {
			return ldap.LdapUser{}, false
		}

		return usersGet[0], true
	}

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

		// the LDAP entry is already at hand for a single-user sync; a full
		// sync only fetches it when needed so it stays one query per group
		entry, hasEntry := ldapUsers[member]
		if !hasEntry && force {
			entry, hasEntry = ldapUser(member)
		}

		var entryRef *ldap.LdapUser
		if hasEntry {
			entryRef = &entry
		}

		userDB := index.find(member, entryRef)
		if userDB == nil && !hasEntry {
			// not found by the member key; fetch the entry and retry with
			// its DN, uid and mail before treating the user as new
			if entry, hasEntry = ldapUser(member); hasEntry {
				entryRef = &entry
				userDB = index.find(member, entryRef)
			}
		}

		if userDB != nil {
			if !data.CompareSlices(userDB.SyncRoleIDs, roleIDs) {
				if err := m.store.UpdateUserSyncRoles(ctx, userDB.ID, roleIDs); err != nil {
					return fmt.Errorf("failed updating user sync roles: %w", err)
				}
			}

			if !hasEntry {
				continue
			}

			if err := m.ldapUpdateUser(ctx, sn, userDB, entry, roleIDs, force); err != nil {
				return err
			}

			continue
		}

		// user not found, create
		if !hasEntry {
			continue
		}

		u := entry
		if _, err := m.store.CreateUser(ctx, data.User{
			SyncRoleIDs: roleIDs,
			Alias:       ldapAliases(u, member),
			Details:     ldapDetails(nil, u),
		}); err != nil {
			if errors.Is(err, data.ErrConflict) {
				slog.Warn("ldap user create conflict",
					slog.String("member", member), slog.String("dn", u.DN), slog.String("error", err.Error()))

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

	// stamp the fleet-wide sync clock on a completed full sync so the next
	// periodic run waits a full interval; manual syncs count too. Single-user
	// syncs are left out — they do not cover the whole directory.
	if uid == "" {
		if err := m.store.TouchSyncLock(ctx, ldapSyncLockName, m.instanceID); err != nil {
			slog.Warn("ldap sync lock touch failed", slog.String("error", err.Error()))
		}
	}

	return m.cache.Reload(ctx)
}

// watchLDAP periodically syncs LDAP when an enabled config exists.
//
// The schedule is coordinated across instances through the auth_sync_locks
// table: each tick at most one instance claims the lease and runs the sync,
// so a fleet of N instances sharing one database still syncs once per
// sync_duration instead of N times.
func (m *Auth) watchLDAP(ctx context.Context) {
	if m.LDAP.DisableSync {
		slog.Info("periodic ldap sync is disabled for this instance")
		return
	}

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runtime := m.ldapRuntime()
			if runtime == nil || runtime.DisableSync {
				continue
			}

			// The lease runs slightly short of the interval so a tick landing
			// a few seconds early still claims it; floored so tiny intervals
			// keep a sane guard window against doubled syncs.
			lease := runtime.SyncDuration - 30*time.Second
			if lease < 30*time.Second {
				lease = 30 * time.Second
			}

			acquired, err := m.store.AcquireSyncLock(ctx, ldapSyncLockName, m.instanceID, lease)
			if err != nil {
				slog.Error("ldap sync lock failed", slog.String("error", err.Error()))
				continue
			}
			if !acquired {
				continue
			}

			if err := m.LdapSync(ctx, false, ""); err != nil {
				slog.Error("ldap sync failed", slog.String("error", err.Error()))
			}
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
