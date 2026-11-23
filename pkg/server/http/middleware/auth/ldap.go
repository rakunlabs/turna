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

// ldapManagedUser reports whether the user was provisioned from LDAP.
func ldapManagedUser(user *data.User) bool {
	if user.ServiceAccount || user.Local {
		return false
	}

	dn, _ := user.Details[ldapDNKey].(string)

	return dn != ""
}

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
	changed := !data.CompareSlices(user.Alias, aliases) || (u.DN != "" && dn != u.DN) ||
		(u.Source != "" && ldapUserSource(user) != u.Source)
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
	if u.Source != "" {
		out[ldapSourceKey] = u.Source
	}

	return out
}

type ldapSource struct {
	id      string
	key     string
	cfg     LDAPSettings
	runtime *ldap.Ldap
}

type ldapManager struct {
	m       sync.Mutex
	version uint64
	sources []*ldapSource
	// keys resolves a stored ldap_source value (a source key, or the id of a
	// config from before keys) to its source key, over every stored config.
	keys map[string]string
	// served holds the source keys of the configs enabled on this instance.
	served map[string]bool
}

// sourceKey is the key written to the users of this config; configs sharing
// it serve the same users. Empty falls back to the config id.
func (s LDAPSettings) sourceKey() string {
	if key := strings.TrimSpace(s.Key); key != "" {
		return key
	}

	return s.ID
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

	l.Bind.Mode = s.Bind.Mode
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

// ldapSources returns the runtimes of every stored LDAP config in priority
// order. The instance-local address override applies to the primary only.
func (m *Auth) ldapSources() []*ldapSource {
	sn := m.cache.Snapshot()

	m.ldap.m.Lock()
	defer m.ldap.m.Unlock()

	if m.ldap.version == sn.Version && (m.ldap.sources != nil || len(sn.LDAP) == 0) {
		return m.ldap.sources
	}

	old := make(map[string]*ldapSource, len(m.ldap.sources))
	for _, s := range m.ldap.sources {
		old[s.id] = s
	}

	// ids first so a key always wins over an id with the same name
	keys := make(map[string]string, 2*len(sn.LDAP))
	for _, cfg := range sn.LDAP {
		keys[cfg.ID] = cfg.sourceKey()
	}
	for _, cfg := range sn.LDAP {
		keys[cfg.sourceKey()] = cfg.sourceKey()
	}

	var sources []*ldapSource
	served := make(map[string]bool)
	for _, cfg := range sn.LDAP {
		if !m.LDAP.ldapConfigAllowed(cfg.ID) {
			continue
		}
		served[cfg.sourceKey()] = true

		if addr := m.LDAP.Addrs[cfg.ID]; addr != "" {
			cfg.Addr = addr
		} else if len(sources) == 0 && m.LDAP.Addr != "" {
			cfg.Addr = m.LDAP.Addr
		}

		// every IAM write bumps the snapshot version; keep the runtime (and
		// its open connections) unless the effective config actually changed
		if prev := old[cfg.ID]; prev != nil && reflect.DeepEqual(prev.cfg, cfg) {
			delete(old, cfg.ID)
			sources = append(sources, prev)

			continue
		}

		sources = append(sources, &ldapSource{id: cfg.ID, key: cfg.sourceKey(), cfg: cfg, runtime: cfg.runtime()})
	}

	for _, s := range old {
		closeLater(s.runtime.Close)
	}

	m.ldap.sources = sources
	m.ldap.keys = keys
	m.ldap.served = served
	m.ldap.version = sn.Version

	return sources
}

// ldapKeyMaps returns the source key resolution of the current snapshot. The
// maps are rebuilt, never mutated, so they are safe to read without the lock.
func (m *Auth) ldapKeyMaps() (keys map[string]string, served map[string]bool) {
	m.ldapSources()

	m.ldap.m.Lock()
	defer m.ldap.m.Unlock()

	return m.ldap.keys, m.ldap.served
}

// ldapExcluded reports whether the stored source belongs to configs that
// exist but none of which is enabled on this instance.
func (m *Auth) ldapExcluded(source string) bool {
	if source == "" {
		return false
	}

	keys, served := m.ldapKeyMaps()
	key, ok := keys[source]

	return ok && !served[key]
}

// ldapUserKeyFunc returns a resolver of a user's source key. A user whose
// source no enabled config serves (removed, or unknown) resolves to "" and is
// handled like an unassigned one.
func (m *Auth) ldapUserKeyFunc() func(*data.User) string {
	keys, served := m.ldapKeyMaps()

	return func(user *data.User) string {
		key := keys[ldapUserSource(user)]
		if key == "" || !served[key] {
			return ""
		}

		return key
	}
}

func (m *Auth) ldapUserKey(user *data.User) string {
	return m.ldapUserKeyFunc()(user)
}

// ldapSyncTargets picks one config per source key for a full sync: configs
// sharing a key serve the same users, so syncing more than one would make
// them fight over the users' roles. The first config in priority order with
// sync enabled wins; a manual sync falls back to the first that reads groups.
func ldapSyncTargets(sources []*ldapSource, periodic bool) []*ldapSource {
	picked := make(map[string]*ldapSource)
	var keys []string
	for _, s := range sources {
		if s.cfg.DisableGroups {
			continue
		}

		current, ok := picked[s.key]
		if !ok {
			keys = append(keys, s.key)
			picked[s.key] = s

			continue
		}

		if current.cfg.DisableSync && !s.cfg.DisableSync {
			picked[s.key] = s
		}
	}

	targets := make([]*ldapSource, 0, len(keys))
	for _, key := range keys {
		s := picked[key]
		if periodic && s.cfg.DisableSync {
			continue
		}

		targets = append(targets, s)
	}

	return targets
}

// ldapPrimary reports whether the source key is the primary one; users
// synced before sources were recorded belong to it.
func ldapPrimary(sources []*ldapSource, key string) bool {
	return len(sources) > 0 && sources[0].key == key
}

// LDAPSourceInfo describes a stored LDAP config as this instance uses it.
type LDAPSourceInfo struct {
	ID      string `json:"id"`
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
	Syncs   bool   `json:"syncs"`
}

// ldapSourceInfo lists every stored LDAP config with its source key, whether
// this instance uses it and whether it is the one that syncs its key here.
func (m *Auth) ldapSourceInfo() []LDAPSourceInfo {
	if m.cache == nil {
		return []LDAPSourceInfo{}
	}

	sources := m.ldapSources()

	syncs := make(map[string]bool)
	for _, s := range ldapSyncTargets(sources, true) {
		syncs[s.id] = true
	}

	enabled := make(map[string]bool, len(sources))
	for _, s := range sources {
		enabled[s.id] = true
	}

	sn := m.cache.Snapshot()
	infos := make([]LDAPSourceInfo, 0, len(sn.LDAP))
	for _, cfg := range sn.LDAP {
		infos = append(infos, LDAPSourceInfo{
			ID:      cfg.ID,
			Key:     cfg.sourceKey(),
			Enabled: enabled[cfg.ID],
			Syncs:   syncs[cfg.ID] && !m.LDAP.DisableSync,
		})
	}

	return infos
}

func (m *Auth) ldapEnabled() bool {
	return len(m.ldapSources()) > 0
}

func (m *Auth) closeLDAP() error {
	m.ldap.m.Lock()
	defer m.ldap.m.Unlock()

	for _, s := range m.ldap.sources {
		closeLater(s.runtime.Close)
	}

	return nil
}

// ldapSourceKey stores the source key of the LDAP configs a user belongs to,
// so password checks and syncs only consult those directories. Users synced
// before source keys hold the config id, which resolves to the config's key.
const ldapSourceKey = "ldap_source"

func ldapUserSource(user *data.User) string {
	if user == nil {
		return ""
	}

	source, _ := user.Details[ldapSourceKey].(string)

	return source
}

// LdapCheckPassword checks the password against the configs of the user's
// source key, in priority order; the first that accepts it wins. Users
// without a (still served) key are checked against every config and adopted
// by the key of the config that accepts the password. It returns that key.
func (m *Auth) LdapCheckPassword(user *data.User, username, password string) (string, bool, error) {
	sources := m.ldapSources()
	if len(sources) == 0 {
		return "", false, errors.New("ldap is not configured")
	}

	// a user whose configs are all disabled on this instance must not be
	// adopted by another directory here; that would rewrite its source in
	// the shared db
	if source := ldapUserSource(user); m.ldapExcluded(source) {
		return "", false, fmt.Errorf("ldap source %q is not enabled on this instance: %w", source, errLDAPSourceExcluded)
	}

	if key := m.ldapUserKey(user); key != "" {
		return checkPasswordIn(sources, key, username, password, func(s *ldapSource) bool { return s.key == key })
	}

	// a stored user without a (still configured) source keeps its directory
	// identity: when a config still serves its DN only those configs are
	// asked, so an entry with the same uid elsewhere cannot take it over.
	// When no config serves the DN anymore (the directory was replaced, e.g.
	// a new base DN) every config is asked and the first match adopts it.
	storedDN := ""
	if user != nil {
		storedDN, _ = user.Details[ldapDNKey].(string)
	}
	if storedDN != "" && !slices.ContainsFunc(sources, func(s *ldapSource) bool {
		return strings.EqualFold(s.runtime.UserDN(username), storedDN)
	}) {
		storedDN = ""
	}

	return checkPasswordIn(sources, "", username, password, func(s *ldapSource) bool {
		if storedDN != "" && !strings.EqualFold(s.runtime.UserDN(username), storedDN) {
			return false
		}

		// a config that may not create users cannot log in unknown ones
		return user != nil || !s.cfg.DisableRegister
	})
}

// checkPasswordIn asks the matching configs in priority order. It returns
// the key of the config that accepted the password; on a mismatch it returns
// key (the user's own key, or "" for an unassigned user).
func checkPasswordIn(sources []*ldapSource, key, username, password string, match func(*ldapSource) bool) (string, bool, error) {
	var errs []error
	for _, s := range sources {
		if !match(s) {
			continue
		}

		ok, err := s.runtime.CheckPassword(username, password)
		if ok {
			return s.key, true, nil
		}
		if err != nil {
			if errors.Is(err, ldap.ErrExceedPasswordRetryLimit) {
				return s.key, false, err
			}

			errs = append(errs, fmt.Errorf("ldap %s: %w", s.id, err))
		}
	}

	// an unreachable directory may hold the user; do not report a mismatch
	if len(errs) > 0 {
		return "", false, errors.Join(errs...)
	}

	return key, false, nil
}

var errLDAPUserNotFound = errors.New("ldap user not found")

// errLDAPSourceExcluded marks a login of a user whose LDAP config is disabled
// on this instance; it is a client-side refusal, not a server failure.
var errLDAPSourceExcluded = errors.New("ldap source excluded")

// LdapSync syncs LDAP groups and users into the store, one config per source
// key. When uid is set, only that user is synced from the first config that
// has it.
func (m *Auth) LdapSync(ctx context.Context, force bool, uid string) error {
	return m.ldapSync(ctx, force, uid, "", false)
}

// ldapSync runs the sync. key limits a single-user sync to the configs of
// one source key; periodic skips configs with disable_sync.
func (m *Auth) ldapSync(ctx context.Context, force bool, uid, key string, periodic bool) error {
	sources := m.ldapSources()
	if len(sources) == 0 {
		return errors.New("ldap is not configured")
	}

	m.ldapSyncM.Lock()
	defer m.ldapSyncM.Unlock()

	ctx = data.WithContextUserName(ctx, "LDAP")

	if uid != "" {
		for _, s := range ldapUserSyncOrder(sources) {
			if key != "" && s.key != key {
				continue
			}

			err := m.ldapSyncSource(ctx, s, ldapPrimary(sources, s.key), force, uid)
			if !errors.Is(err, errLDAPUserNotFound) {
				return err
			}
		}

		return errLDAPUserNotFound
	}

	targets := ldapSyncTargets(sources, periodic)

	logi.Ctx(ctx).Info("syncing ldap starting", slog.Int("sources", len(targets)))
	defer logi.Ctx(ctx).Info("syncing ldap done")

	var errs []error
	for _, s := range targets {
		if err := m.ldapSyncSource(ctx, s, ldapPrimary(sources, s.key), force, ""); err != nil {
			errs = append(errs, fmt.Errorf("ldap %s: %w", s.id, err))

			continue
		}

		// stamp the key's fleet-wide sync clock on a completed full sync so
		// the next periodic run waits a full interval; manual syncs count too.
		// Single-user syncs are left out — they do not cover the whole directory.
		if err := m.store.TouchSyncLock(ctx, ldapSyncLockKey(s.key), m.instanceID); err != nil {
			slog.Warn("ldap sync lock touch failed", slog.String("ldap", s.id), slog.String("error", err.Error()))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// ldapUserSyncOrder orders the configs for a single-user sync: the config
// that full-syncs a key comes first, so a login refreshes the user from the
// same directory as the periodic sync instead of the one that checked the
// password.
func ldapUserSyncOrder(sources []*ldapSource) []*ldapSource {
	targets := ldapSyncTargets(sources, false)

	ordered := make([]*ldapSource, 0, len(sources))
	ordered = append(ordered, targets...)
	for _, s := range sources {
		if !slices.Contains(targets, s) {
			ordered = append(ordered, s)
		}
	}

	return ordered
}

// ldapOwns reports whether the stored user belongs to the source. Users
// synced before sources were recorded belong to the primary until another
// source claims them.
func ldapOwns(owner, source string, primary bool) bool {
	if owner == "" {
		return primary
	}

	return owner == source
}

func (m *Auth) ldapSyncSource(ctx context.Context, s *ldapSource, primary, force bool, uid string) error {
	runtime := s.runtime
	userKey := m.ldapUserKeyFunc()

	// without groups a full sync has no member list to walk
	groupsOff := s.cfg.DisableGroups
	if groupsOff && uid == "" {
		return nil
	}

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
			if httputil.NewErrorAs(err).Code == http.StatusNotFound {
				return errLDAPUserNotFound
			}

			return fmt.Errorf("failed getting user: %w", err)
		}
		if len(usersGet) == 0 {
			return errLDAPUserNotFound
		}

		entry := usersGet[0]
		entry.Source = s.key
		ldapUsers[uid] = entry
		users[uid] = nil

		var groups []ldap.LdapGroup
		if !groupsOff {
			groups, err = runtime.GroupsForUser(conn, entry.DN)
			if err != nil {
				return fmt.Errorf("failed getting user groups: %w", err)
			}
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

		entry := usersGet[0]
		entry.Source = s.key

		return entry, true
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
			// a user synced from another directory is never touched here
			if owner := userKey(userDB); owner != "" && owner != s.key {
				slog.Warn("ldap user belongs to another source, skipped",
					slog.String("id", userDB.ID), slog.String("source", s.key), slog.String("owner", owner))

				continue
			}

			// sync roles are left alone when this source maps no groups
			if groupsOff {
				roleIDs = userDB.SyncRoleIDs
			}

			if !data.CompareSlices(userDB.SyncRoleIDs, roleIDs) {
				if err := m.store.UpdateUserSyncRoles(ctx, userDB.ID, roleIDs); err != nil {
					return fmt.Errorf("failed updating user sync roles: %w", err)
				}
			}

			if !hasEntry {
				// claim users synced before sources were recorded
				if ldapUserSource(userDB) != s.key && s.key != "" {
					if entry, hasEntry = ldapUser(member); !hasEntry {
						continue
					}
				} else {
					continue
				}
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

		if s.cfg.DisableRegister {
			if uid != "" {
				return errLDAPUserNotFound
			}

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

	// reset sync roles for this source's LDAP users that left all groups;
	// users without an LDAP DN (OAuth/SAML federated) keep the roles their
	// provider synced, users of other sources are left to their own sync
	if uid == "" {
		for _, id := range sn.UserIDs {
			user := sn.Users[id]
			if !ldapManagedUser(user) || len(user.SyncRoleIDs) == 0 || !ldapOwns(userKey(user), s.key, primary) {
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

			slog.Info("user sync roles cleared", slog.String("id", user.ID), slog.String("by", "LDAP"), slog.String("source", s.key))
		}
	}

	return m.cache.Reload(ctx)
}

// watchLDAP periodically syncs LDAP when an enabled config exists.
//
// The schedule is coordinated across instances through the auth_sync_locks
// table: each tick at most one instance claims the lease and runs the sync,
// so a fleet of N instances sharing one database still syncs once per
// sync_duration instead of N times. With several configs the shortest
// sync_duration of the configs that sync drives the schedule.
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
			sources := m.ldapSources()
			for _, s := range ldapSyncTargets(sources, true) {
				// The lease runs slightly short of the interval so a tick landing
				// a few seconds early still claims it; floored so tiny intervals
				// keep a sane guard window against doubled syncs.
				lease := s.runtime.SyncDuration - 30*time.Second
				if lease < 30*time.Second {
					lease = 30 * time.Second
				}

				// one lease per source key: the users of a key are synced by
				// exactly one instance among those that have one of its configs
				acquired, err := m.store.AcquireSyncLock(ctx, ldapSyncLockKey(s.key), m.instanceID, lease)
				if err != nil {
					slog.Error("ldap sync lock failed", slog.String("ldap", s.id), slog.String("error", err.Error()))
					continue
				}
				if !acquired {
					continue
				}

				if err := m.ldapSyncPeriodic(ctx, s, ldapPrimary(sources, s.key)); err != nil {
					slog.Error("ldap sync failed", slog.String("ldap", s.id), slog.String("error", err.Error()))
				}
			}
		}
	}
}

func (m *Auth) ldapSyncPeriodic(ctx context.Context, s *ldapSource, primary bool) error {
	m.ldapSyncM.Lock()
	defer m.ldapSyncM.Unlock()

	ctx = data.WithContextUserName(ctx, "LDAP")

	if err := m.ldapSyncSource(ctx, s, primary, false, ""); err != nil {
		return err
	}

	if err := m.store.TouchSyncLock(ctx, ldapSyncLockKey(s.key), m.instanceID); err != nil {
		slog.Warn("ldap sync lock touch failed", slog.String("ldap", s.id), slog.String("error", err.Error()))
	}

	return nil
}

// ////////////////////////////////////////////////////////////////////
// handlers

func (m *Auth) LdapGetGroupsAPI(w http.ResponseWriter, r *http.Request) {
	sources := m.ldapSources()
	if len(sources) == 0 {
		httputil.HandleError(w, httputil.NewError("ldap is not configured", nil, http.StatusFailedDependency))
		return
	}

	all := make([]ldap.LdapGroup, 0)
	for _, s := range sources {
		if s.cfg.DisableGroups {
			continue
		}

		conn, err := s.runtime.ConnectWithCheck()
		if err != nil {
			httputil.HandleError(w, httputil.NewError("ldap connection problem: "+s.id, err, http.StatusInternalServerError))
			return
		}

		groups, err := s.runtime.Groups(conn)
		if err != nil {
			httputil.HandleError(w, httputil.NewError("failed getting groups: "+s.id, err, http.StatusInternalServerError))
			return
		}

		for i := range groups {
			groups[i].Source = s.key
		}

		all = append(all, groups...)
	}

	httputil.JSON(w, http.StatusOK, data.Response[[]ldap.LdapGroup]{
		Meta:    &data.Meta{TotalItemCount: uint64(len(all))},
		Payload: all,
	})
}

func (m *Auth) LdapGetUserAPI(w http.ResponseWriter, r *http.Request) {
	sources := m.ldapSources()
	if len(sources) == 0 {
		httputil.HandleError(w, httputil.NewError("ldap is not configured", nil, http.StatusFailedDependency))
		return
	}

	uid := r.PathValue("uid")
	if uid == "" {
		httputil.HandleError(w, httputil.NewError("uid is required", nil, http.StatusBadRequest))
		return
	}

	uid, err := url.PathUnescape(uid)
	if err != nil {
		httputil.HandleError(w, httputil.NewError("failed unescaping uid", err, http.StatusBadRequest))
		return
	}

	// first config in priority order that has the user wins
	for _, s := range sources {
		conn, err := s.runtime.ConnectWithCheck()
		if err != nil {
			httputil.HandleError(w, httputil.NewError("ldap connection problem: "+s.id, err, http.StatusInternalServerError))
			return
		}

		users, err := s.runtime.Users(conn, []string{uid})
		if err != nil {
			if httputil.NewErrorAs(err).Code == http.StatusNotFound {
				continue
			}

			httputil.HandleError(w, httputil.NewErrorAs(err))
			return
		}
		if len(users) == 0 {
			continue
		}

		users[0].Source = s.key
		httputil.JSON(w, http.StatusOK, data.Response[ldap.LdapUser]{Payload: users[0]})

		return
	}

	httputil.HandleError(w, httputil.NewError("user not found", nil, http.StatusNotFound))
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
