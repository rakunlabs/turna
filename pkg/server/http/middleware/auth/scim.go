package auth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/auth/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

// SCIM 2.0 (RFC 7643, RFC 7644) provisioning of users and groups. A SCIM
// Group is a turna role; membership lives in the user's role_ids.

const (
	scimSchemaUser       = "urn:ietf:params:scim:schemas:core:2.0:User"
	scimSchemaEnterprise = "urn:ietf:params:scim:schemas:extension:enterprise:2.0:User"
	scimSchemaGroup      = "urn:ietf:params:scim:schemas:core:2.0:Group"
	scimSchemaList       = "urn:ietf:params:scim:api:messages:2.0:ListResponse"
	scimSchemaError      = "urn:ietf:params:scim:api:messages:2.0:Error"
	scimSchemaPatch      = "urn:ietf:params:scim:api:messages:2.0:PatchOp"
	scimContentType      = "application/scim+json"
	scimMaxResults       = 200
	// scimUserMarker in user details records that SCIM manages the account.
	scimUserMarker = "scim_external_id"
)

// SCIMSettings is the decoded "scim" setting namespace.
type SCIMSettings struct {
	// Enabled turns the /scim/v2 endpoints on.
	Enabled bool `json:"enabled"`
	// Token is the bearer token SCIM clients send. Stored encrypted with the
	// other settings and write-only on the management API.
	Token string `json:"bearer_token"`
	// DeleteDisables keeps users on DELETE and only disables them.
	DeleteDisables bool `json:"delete_disables"`
}

type scimError struct {
	Schemas  []string `json:"schemas"`
	Status   string   `json:"status"`
	ScimType string   `json:"scimType,omitempty"`
	Detail   string   `json:"detail,omitempty"`
}

func scimJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", scimContentType)
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func scimFail(w http.ResponseWriter, code int, scimType, detail string) {
	scimJSON(w, code, scimError{Schemas: []string{scimSchemaError}, Status: strconv.Itoa(code), ScimType: scimType, Detail: detail})
}

// scimAuth admits requests with the configured SCIM bearer token.
func (m *Auth) scimAuth(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := m.cache.Snapshot().SCIM
		if !cfg.Enabled || cfg.Token == "" {
			scimFail(w, http.StatusNotFound, "", "SCIM is disabled")

			return
		}

		token := session.BearerToken(r.Header.Get("Authorization"))
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(cfg.Token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="scim"`)
			scimFail(w, http.StatusUnauthorized, "", "invalid SCIM token")

			return
		}

		ctx := data.WithContextUserName(r.Context(), "scim")
		next(w, r.WithContext(ctx))
	}
}

func (m *Auth) scimBase(r *http.Request) string {
	return strings.TrimSuffix(m.issuerURL(r), "/oauth2") + "/scim/v2"
}

// ---- users ---------------------------------------------------------------

type scimName struct {
	Formatted  string `json:"formatted,omitempty"`
	GivenName  string `json:"givenName,omitempty"`
	FamilyName string `json:"familyName,omitempty"`
}

type scimMulti struct {
	Value   string `json:"value"`
	Type    string `json:"type,omitempty"`
	Primary bool   `json:"primary,omitempty"`
	Display string `json:"display,omitempty"`
	Ref     string `json:"$ref,omitempty"`
}

type scimMeta struct {
	ResourceType string `json:"resourceType"`
	Created      string `json:"created,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
	Location     string `json:"location,omitempty"`
	Version      string `json:"version,omitempty"`
}

type scimUser struct {
	Schemas     []string    `json:"schemas"`
	ID          string      `json:"id,omitempty"`
	ExternalID  string      `json:"externalId,omitempty"`
	UserName    string      `json:"userName"`
	Name        *scimName   `json:"name,omitempty"`
	DisplayName string      `json:"displayName,omitempty"`
	Emails      []scimMulti `json:"emails,omitempty"`
	PhoneNums   []scimMulti `json:"phoneNumbers,omitempty"`
	Active      *bool       `json:"active,omitempty"`
	Password    string      `json:"password,omitempty"`
	Groups      []scimMulti `json:"groups,omitempty"`
	Meta        *scimMeta   `json:"meta,omitempty"`
}

func primaryValue(values []scimMulti) string {
	for _, v := range values {
		if v.Primary {
			return v.Value
		}
	}
	if len(values) > 0 {
		return values[0].Value
	}

	return ""
}

func detailString(details map[string]any, key string) string {
	v, _ := details[key].(string)

	return v
}

// toSCIMUser renders a turna user. userName is the first alias.
func (m *Auth) toSCIMUser(r *http.Request, sn *Snapshot, user *data.User) scimUser {
	active := !user.Disabled
	out := scimUser{
		Schemas:     []string{scimSchemaUser},
		ID:          user.ID,
		ExternalID:  detailString(user.Details, scimUserMarker),
		DisplayName: detailString(user.Details, "name"),
		Active:      &active,
		Meta: &scimMeta{
			ResourceType: "User",
			Created:      user.CreatedAt,
			LastModified: user.UpdatedAt,
			Location:     m.scimBase(r) + "/Users/" + user.ID,
			Version:      scimVersion(user.Revision),
		},
	}
	if len(user.Alias) > 0 {
		out.UserName = user.Alias[0]
	}

	given, family := detailString(user.Details, "given_name"), detailString(user.Details, "family_name")
	if given != "" || family != "" || out.DisplayName != "" {
		out.Name = &scimName{Formatted: out.DisplayName, GivenName: given, FamilyName: family}
	}
	if email := detailString(user.Details, "email"); email != "" {
		out.Emails = []scimMulti{{Value: email, Type: "work", Primary: true}}
	}
	if phone := detailString(user.Details, "phone_number"); phone != "" {
		out.PhoneNums = []scimMulti{{Value: phone, Type: "work", Primary: true}}
	}

	for _, id := range user.RoleIDs {
		if role, ok := sn.Roles[id]; ok {
			out.Groups = append(out.Groups, scimMulti{Value: id, Display: role.Name, Ref: m.scimBase(r) + "/Groups/" + id})
		}
	}

	return out
}

func scimVersion(revision string) string {
	if revision == "" {
		return ""
	}

	return `W/"` + revision + `"`
}

// applySCIMUser copies SCIM attributes onto a turna user.
func applySCIMUser(user *data.User, in scimUser) error {
	if strings.TrimSpace(in.UserName) == "" {
		return errors.New("userName is required")
	}

	if user.Details == nil {
		user.Details = map[string]any{}
	}

	// userName is the primary alias; the primary email is an alias too so
	// the user can sign in with it
	aliases := []string{strings.TrimSpace(in.UserName)}
	email := primaryValue(in.Emails)
	if email != "" && !strings.EqualFold(email, aliases[0]) {
		aliases = append(aliases, email)
	}
	for _, a := range user.Alias {
		// keep other aliases (federated logins) that SCIM does not own
		if !slices.ContainsFunc(aliases, func(v string) bool { return strings.EqualFold(v, a) }) && a != detailString(user.Details, "email") {
			aliases = append(aliases, a)
		}
	}
	user.Alias = aliases

	set := func(key, value string) {
		if value == "" {
			delete(user.Details, key)

			return
		}
		user.Details[key] = value
	}

	name := in.DisplayName
	if name == "" && in.Name != nil {
		name = in.Name.Formatted
		if name == "" {
			name = strings.TrimSpace(in.Name.GivenName + " " + in.Name.FamilyName)
		}
	}
	set("name", name)
	if in.Name != nil {
		set("given_name", in.Name.GivenName)
		set("family_name", in.Name.FamilyName)
	}
	set("email", email)
	set("phone_number", primaryValue(in.PhoneNums))
	set(scimUserMarker, in.ExternalID)
	if _, ok := user.Details[scimUserMarker]; !ok {
		user.Details[scimUserMarker] = ""
	}
	if in.Password != "" {
		user.Details["password"] = in.Password
		user.Local = true
	}
	if in.Active != nil {
		user.Disabled = !*in.Active
	}

	return nil
}

func scimStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, data.ErrNotFound):
		scimFail(w, http.StatusNotFound, "", "resource not found")
	case errors.Is(err, data.ErrConflict) || strings.Contains(err.Error(), "alias"):
		scimFail(w, http.StatusConflict, "uniqueness", err.Error())
	case errors.Is(err, errPrecondition):
		scimFail(w, http.StatusPreconditionFailed, "", err.Error())
	default:
		scimFail(w, http.StatusInternalServerError, "", err.Error())
	}
}

func decodeSCIM(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}

func (m *Auth) scimReloaded(ctx context.Context) *Snapshot {
	if err := m.cache.Reload(ctx); err != nil {
		return m.cache.Snapshot()
	}

	return m.cache.Snapshot()
}

func (m *Auth) SCIMCreateUser(w http.ResponseWriter, r *http.Request) {
	var in scimUser
	if err := decodeSCIM(r, &in); err != nil {
		scimFail(w, http.StatusBadRequest, "invalidSyntax", err.Error())

		return
	}

	if user := m.cache.Snapshot().UserByAlias(strings.TrimSpace(in.UserName)); user != nil {
		scimFail(w, http.StatusConflict, "uniqueness", "userName already exists")

		return
	}

	user := data.User{Local: false}
	if err := applySCIMUser(&user, in); err != nil {
		scimFail(w, http.StatusBadRequest, "invalidValue", err.Error())

		return
	}

	id, err := m.store.CreateUser(r.Context(), user)
	if err != nil {
		scimStoreError(w, err)

		return
	}

	sn := m.scimReloaded(r.Context())
	created, ok := sn.Users[id]
	if !ok {
		scimFail(w, http.StatusInternalServerError, "", "created user not found")

		return
	}

	m.audit(r, AuditEvent{Event: auditAdminChange, Outcome: auditSuccess, Subject: id, Detail: map[string]any{"scim": "create user"}})
	w.Header().Set("Location", m.scimBase(r)+"/Users/"+id)
	scimJSON(w, http.StatusCreated, m.toSCIMUser(r, sn, created))
}

func (m *Auth) SCIMGetUser(w http.ResponseWriter, r *http.Request) {
	sn := m.cache.Snapshot()
	user, ok := sn.Users[r.PathValue("id")]
	if !ok || user.ServiceAccount {
		scimFail(w, http.StatusNotFound, "", "user not found")

		return
	}

	scimJSON(w, http.StatusOK, m.toSCIMUser(r, sn, user))
}

func (m *Auth) SCIMReplaceUser(w http.ResponseWriter, r *http.Request) {
	sn := m.cache.Snapshot()
	found, ok := sn.Users[r.PathValue("id")]
	if !ok || found.ServiceAccount {
		scimFail(w, http.StatusNotFound, "", "user not found")

		return
	}

	var in scimUser
	if err := decodeSCIM(r, &in); err != nil {
		scimFail(w, http.StatusBadRequest, "invalidSyntax", err.Error())

		return
	}

	user := cloneUser(found)
	if err := applySCIMUser(&user, in); err != nil {
		scimFail(w, http.StatusBadRequest, "invalidValue", err.Error())

		return
	}
	if in.Active == nil {
		// PUT replaces: an omitted active means active (RFC 7643 default)
		user.Disabled = false
	}

	m.scimPutUser(w, r, user)
}

func (m *Auth) scimPutUser(w http.ResponseWriter, r *http.Request, user data.User) {
	if err := m.store.PutUser(r.Context(), user); err != nil {
		scimStoreError(w, err)

		return
	}

	sn := m.scimReloaded(r.Context())
	m.audit(r, AuditEvent{Event: auditAdminChange, Outcome: auditSuccess, Subject: user.ID, Detail: map[string]any{"scim": "update user"}})
	scimJSON(w, http.StatusOK, m.toSCIMUser(r, sn, sn.Users[user.ID]))
}

func cloneUser(u *data.User) data.User {
	out := *u
	out.Alias = slices.Clone(u.Alias)
	out.RoleIDs = slices.Clone(u.RoleIDs)
	out.Details = make(map[string]any, len(u.Details))
	for k, v := range u.Details {
		out.Details[k] = v
	}
	// the stored hash is preserved by PutUser when the field is empty
	delete(out.Details, "password")

	return out
}

func (m *Auth) SCIMDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sn := m.cache.Snapshot()
	found, ok := sn.Users[id]
	if !ok || found.ServiceAccount {
		scimFail(w, http.StatusNotFound, "", "user not found")

		return
	}

	var err error
	if sn.SCIM.DeleteDisables {
		user := cloneUser(found)
		user.Disabled = true
		err = m.store.PutUser(r.Context(), user)
	} else {
		err = m.store.DeleteUser(r.Context(), id)
	}
	if err != nil {
		scimStoreError(w, err)

		return
	}

	m.scimReloaded(r.Context())
	m.audit(r, AuditEvent{Event: auditAdminChange, Outcome: auditSuccess, Subject: id, Detail: map[string]any{"scim": "delete user"}})
	w.WriteHeader(http.StatusNoContent)
}

// scimListParams reads startIndex (1-based) and count.
func scimListParams(r *http.Request) (int, int) {
	start, _ := strconv.Atoi(r.URL.Query().Get("startIndex"))
	if start < 1 {
		start = 1
	}

	count := 100
	if v := r.URL.Query().Get("count"); v != "" {
		count, _ = strconv.Atoi(v)
	}
	count = min(max(count, 0), scimMaxResults)

	return start, count
}

type scimListResponse struct {
	Schemas      []string `json:"schemas"`
	TotalResults int      `json:"totalResults"`
	StartIndex   int      `json:"startIndex"`
	ItemsPerPage int      `json:"itemsPerPage"`
	Resources    []any    `json:"Resources"`
}

func page[T any](items []T, start, count int) []T {
	if start-1 >= len(items) {
		return nil
	}
	items = items[start-1:]
	if len(items) > count {
		items = items[:count]
	}

	return items
}

// scimFilter is the supported subset of RFC 7644 §3.4.2.2: a single
// "attribute eq value" comparison, which is what Okta and Entra ID send.
type scimFilter struct {
	Attr  string
	Value string
}

func parseSCIMFilter(raw string) (*scimFilter, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	attr, rest, ok := strings.Cut(raw, " ")
	if !ok {
		return nil, errors.New("unsupported filter")
	}
	op, value, ok := strings.Cut(strings.TrimSpace(rest), " ")
	if !ok || !strings.EqualFold(op, "eq") {
		return nil, errors.New("only the eq operator is supported")
	}

	value = strings.TrimSpace(value)
	if unq, err := strconv.Unquote(value); err == nil {
		value = unq
	} else if value != "true" && value != "false" {
		return nil, errors.New("filter value must be a quoted string")
	}

	return &scimFilter{Attr: strings.ToLower(attr), Value: value}, nil
}

func (f *scimFilter) matchUser(u *data.User) bool {
	if f == nil {
		return true
	}

	switch f.Attr {
	case "username":
		return len(u.Alias) > 0 && strings.EqualFold(u.Alias[0], f.Value)
	case "externalid":
		return detailString(u.Details, scimUserMarker) == f.Value
	case "emails.value", "emails[type eq \"work\"].value":
		return strings.EqualFold(detailString(u.Details, "email"), f.Value)
	case "id":
		return u.ID == f.Value
	case "active":
		return strconv.FormatBool(!u.Disabled) == f.Value
	}

	return false
}

func (m *Auth) SCIMListUsers(w http.ResponseWriter, r *http.Request) {
	filter, err := parseSCIMFilter(r.URL.Query().Get("filter"))
	if err != nil {
		scimFail(w, http.StatusBadRequest, "invalidFilter", err.Error())

		return
	}

	sn := m.cache.Snapshot()
	users := []*data.User{}
	for _, id := range sn.UserIDs {
		u := sn.Users[id]
		if u.ServiceAccount || !filter.matchUser(u) {
			continue
		}
		users = append(users, u)
	}

	start, count := scimListParams(r)
	resources := []any{}
	for _, u := range page(users, start, count) {
		resources = append(resources, m.toSCIMUser(r, sn, u))
	}

	scimJSON(w, http.StatusOK, scimListResponse{
		Schemas: []string{scimSchemaList}, TotalResults: len(users), StartIndex: start, ItemsPerPage: len(resources), Resources: resources,
	})
}

// ---- groups --------------------------------------------------------------

type scimGroup struct {
	Schemas     []string    `json:"schemas"`
	ID          string      `json:"id,omitempty"`
	ExternalID  string      `json:"externalId,omitempty"`
	DisplayName string      `json:"displayName"`
	Members     []scimMulti `json:"members,omitempty"`
	Meta        *scimMeta   `json:"meta,omitempty"`
}

func (m *Auth) toSCIMGroup(r *http.Request, sn *Snapshot, role *data.Role, members bool) scimGroup {
	externalID, _ := role.Data[scimUserMarker].(string)
	out := scimGroup{
		Schemas:     []string{scimSchemaGroup},
		ID:          role.ID,
		ExternalID:  externalID,
		DisplayName: role.Name,
		Meta: &scimMeta{
			ResourceType: "Group",
			Created:      role.CreatedAt,
			LastModified: role.UpdatedAt,
			Location:     m.scimBase(r) + "/Groups/" + role.ID,
			Version:      scimVersion(role.Revision),
		},
	}

	if members {
		for _, id := range sn.UserIDs {
			u := sn.Users[id]
			if slices.Contains(u.RoleIDs, role.ID) {
				display := ""
				if len(u.Alias) > 0 {
					display = u.Alias[0]
				}
				out.Members = append(out.Members, scimMulti{Value: u.ID, Display: display, Ref: m.scimBase(r) + "/Users/" + u.ID})
			}
		}
	}

	return out
}

func (m *Auth) SCIMListGroups(w http.ResponseWriter, r *http.Request) {
	filter, err := parseSCIMFilter(r.URL.Query().Get("filter"))
	if err != nil {
		scimFail(w, http.StatusBadRequest, "invalidFilter", err.Error())

		return
	}

	sn := m.cache.Snapshot()
	roles := []*data.Role{}
	for _, role := range sn.Roles {
		if filter != nil {
			ext, _ := role.Data[scimUserMarker].(string)
			switch filter.Attr {
			case "displayname":
				if !strings.EqualFold(role.Name, filter.Value) {
					continue
				}
			case "externalid":
				if ext != filter.Value {
					continue
				}
			case "id":
				if role.ID != filter.Value {
					continue
				}
			default:
				continue
			}
		}
		roles = append(roles, role)
	}
	slices.SortFunc(roles, func(a, b *data.Role) int { return strings.Compare(a.ID, b.ID) })

	// members are large; excludedAttributes=members (Entra) skips them
	withMembers := !strings.Contains(r.URL.Query().Get("excludedAttributes"), "members")

	start, count := scimListParams(r)
	resources := []any{}
	for _, role := range page(roles, start, count) {
		resources = append(resources, m.toSCIMGroup(r, sn, role, withMembers))
	}

	scimJSON(w, http.StatusOK, scimListResponse{
		Schemas: []string{scimSchemaList}, TotalResults: len(roles), StartIndex: start, ItemsPerPage: len(resources), Resources: resources,
	})
}

func (m *Auth) SCIMGetGroup(w http.ResponseWriter, r *http.Request) {
	sn := m.cache.Snapshot()
	role, ok := sn.Roles[r.PathValue("id")]
	if !ok {
		scimFail(w, http.StatusNotFound, "", "group not found")

		return
	}

	scimJSON(w, http.StatusOK, m.toSCIMGroup(r, sn, role, !strings.Contains(r.URL.Query().Get("excludedAttributes"), "members")))
}

func (m *Auth) SCIMCreateGroup(w http.ResponseWriter, r *http.Request) {
	var in scimGroup
	if err := decodeSCIM(r, &in); err != nil {
		scimFail(w, http.StatusBadRequest, "invalidSyntax", err.Error())

		return
	}
	if strings.TrimSpace(in.DisplayName) == "" {
		scimFail(w, http.StatusBadRequest, "invalidValue", "displayName is required")

		return
	}

	for _, role := range m.cache.Snapshot().Roles {
		if strings.EqualFold(role.Name, in.DisplayName) {
			scimFail(w, http.StatusConflict, "uniqueness", "displayName already exists")

			return
		}
	}

	id, err := m.store.CreateRole(r.Context(), data.Role{
		Name:        strings.TrimSpace(in.DisplayName),
		Description: "Provisioned by SCIM",
		Data:        map[string]any{scimUserMarker: in.ExternalID},
	})
	if err != nil {
		scimStoreError(w, err)

		return
	}

	if err := m.setGroupMembers(r.Context(), id, memberIDs(in.Members)); err != nil {
		scimStoreError(w, err)

		return
	}

	sn := m.scimReloaded(r.Context())
	m.audit(r, AuditEvent{Event: auditAdminChange, Outcome: auditSuccess, Detail: map[string]any{"scim": "create group", "group": id}})
	w.Header().Set("Location", m.scimBase(r)+"/Groups/"+id)
	scimJSON(w, http.StatusCreated, m.toSCIMGroup(r, sn, sn.Roles[id], true))
}

func memberIDs(members []scimMulti) []string {
	ids := make([]string, 0, len(members))
	for _, mem := range members {
		if mem.Value != "" {
			ids = append(ids, mem.Value)
		}
	}

	return ids
}

// setGroupMembers makes exactly ids members of the role.
func (m *Auth) setGroupMembers(ctx context.Context, roleID string, ids []string) error {
	sn := m.scimReloaded(ctx)
	want := map[string]bool{}
	for _, id := range ids {
		if _, ok := sn.Users[id]; !ok {
			return fmt.Errorf("member %s not found; %w", id, data.ErrNotFound)
		}
		want[id] = true
	}

	for _, uid := range sn.UserIDs {
		u := sn.Users[uid]
		has := slices.Contains(u.RoleIDs, roleID)
		if has == want[uid] {
			continue
		}

		if err := m.changeMembership(ctx, u, roleID, want[uid]); err != nil {
			return err
		}
	}

	return nil
}

func (m *Auth) changeMembership(ctx context.Context, u *data.User, roleID string, member bool) error {
	roles := slices.DeleteFunc(slices.Clone(u.RoleIDs), func(id string) bool { return id == roleID })
	if member {
		roles = append(roles, roleID)
	}

	return m.store.PatchUser(ctx, u.ID, data.UserPatch{RoleIDs: &roles})
}

func (m *Auth) SCIMReplaceGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sn := m.cache.Snapshot()
	role, ok := sn.Roles[id]
	if !ok {
		scimFail(w, http.StatusNotFound, "", "group not found")

		return
	}

	var in scimGroup
	if err := decodeSCIM(r, &in); err != nil {
		scimFail(w, http.StatusBadRequest, "invalidSyntax", err.Error())

		return
	}

	name := strings.TrimSpace(in.DisplayName)
	if name == "" {
		name = role.Name
	}
	roleData := map[string]any{}
	for k, v := range role.Data {
		roleData[k] = v
	}
	roleData[scimUserMarker] = in.ExternalID
	if err := m.store.PatchRole(r.Context(), id, data.RolePatch{Name: &name, Data: &roleData}); err != nil {
		scimStoreError(w, err)

		return
	}

	if err := m.setGroupMembers(r.Context(), id, memberIDs(in.Members)); err != nil {
		scimStoreError(w, err)

		return
	}

	sn = m.scimReloaded(r.Context())
	m.audit(r, AuditEvent{Event: auditAdminChange, Outcome: auditSuccess, Detail: map[string]any{"scim": "update group", "group": id}})
	scimJSON(w, http.StatusOK, m.toSCIMGroup(r, sn, sn.Roles[id], true))
}

func (m *Auth) SCIMDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := m.cache.Snapshot().Roles[id]; !ok {
		scimFail(w, http.StatusNotFound, "", "group not found")

		return
	}

	if err := m.setGroupMembers(r.Context(), id, nil); err != nil {
		scimStoreError(w, err)

		return
	}
	if err := m.store.DeleteRole(r.Context(), id); err != nil {
		scimStoreError(w, err)

		return
	}

	m.scimReloaded(r.Context())
	m.audit(r, AuditEvent{Event: auditAdminChange, Outcome: auditSuccess, Detail: map[string]any{"scim": "delete group", "group": id}})
	w.WriteHeader(http.StatusNoContent)
}

// ---- PATCH ---------------------------------------------------------------

type scimPatchOp struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

type scimPatch struct {
	Schemas    []string      `json:"schemas"`
	Operations []scimPatchOp `json:"Operations"`
}

func (m *Auth) SCIMPatchUser(w http.ResponseWriter, r *http.Request) {
	sn := m.cache.Snapshot()
	found, ok := sn.Users[r.PathValue("id")]
	if !ok || found.ServiceAccount {
		scimFail(w, http.StatusNotFound, "", "user not found")

		return
	}

	var patch scimPatch
	if err := decodeSCIM(r, &patch); err != nil {
		scimFail(w, http.StatusBadRequest, "invalidSyntax", err.Error())

		return
	}

	// apply the operations to the SCIM view, then write it back like a PUT
	current := m.toSCIMUser(r, sn, found)
	doc, _ := json.Marshal(current)
	var view map[string]any
	_ = json.Unmarshal(doc, &view)

	for _, op := range patch.Operations {
		if err := applyPatchOp(view, op); err != nil {
			scimFail(w, http.StatusBadRequest, "invalidPath", err.Error())

			return
		}
	}

	doc, _ = json.Marshal(view)
	var in scimUser
	if err := json.Unmarshal(doc, &in); err != nil {
		scimFail(w, http.StatusBadRequest, "invalidValue", err.Error())

		return
	}

	user := cloneUser(found)
	if err := applySCIMUser(&user, in); err != nil {
		scimFail(w, http.StatusBadRequest, "invalidValue", err.Error())

		return
	}

	m.scimPutUser(w, r, user)
}

// patchAttr maps the SCIM attribute paths Okta and Entra ID send to the
// user view keys.
var patchAttr = map[string][]string{
	"username":                           {"userName"},
	"displayname":                        {"displayName"},
	"externalid":                         {"externalId"},
	"active":                             {"active"},
	"password":                           {"password"},
	"name.givenname":                     {"name", "givenName"},
	"name.familyname":                    {"name", "familyName"},
	"name.formatted":                     {"name", "formatted"},
	`emails[type eq "work"].value`:       {"emails"},
	"emails":                             {"emails"},
	"emails.value":                       {"emails"},
	`phonenumbers[type eq "work"].value`: {"phoneNumbers"},
	"phonenumbers":                       {"phoneNumbers"},
}

func applyPatchOp(view map[string]any, op scimPatchOp) error {
	kind := strings.ToLower(op.Op)
	if kind != "add" && kind != "replace" && kind != "remove" {
		return fmt.Errorf("unsupported op %q", op.Op)
	}

	var value any
	if len(op.Value) > 0 {
		if err := json.Unmarshal(op.Value, &value); err != nil {
			return err
		}
	}

	// no path: value is an object of attributes (Entra ID style)
	if op.Path == "" {
		attrs, ok := value.(map[string]any)
		if !ok {
			return errors.New("patch without path needs an object value")
		}
		for k, v := range attrs {
			raw, _ := json.Marshal(v)
			if err := applyPatchOp(view, scimPatchOp{Op: op.Op, Path: k, Value: raw}); err != nil {
				return err
			}
		}

		return nil
	}

	path := strings.ToLower(strings.TrimPrefix(op.Path, scimSchemaUser+":"))
	if strings.HasPrefix(path, strings.ToLower(scimSchemaEnterprise)) {
		// enterprise attributes are accepted and ignored
		return nil
	}

	keys, ok := patchAttr[path]
	if !ok {
		return fmt.Errorf("unsupported path %q", op.Path)
	}

	if keys[0] == "emails" || keys[0] == "phoneNumbers" {
		if kind == "remove" {
			delete(view, keys[0])

			return nil
		}
		if s, ok := value.(string); ok {
			value = []any{map[string]any{"value": s, "type": "work", "primary": true}}
		}
		view[keys[0]] = value

		return nil
	}

	// Entra ID sends "active" as the string "True"/"False"
	if keys[0] == "active" {
		if s, ok := value.(string); ok {
			value = strings.EqualFold(s, "true")
		}
	}

	target := view
	for _, k := range keys[:len(keys)-1] {
		next, _ := target[k].(map[string]any)
		if next == nil {
			next = map[string]any{}
			target[k] = next
		}
		target = next
	}

	last := keys[len(keys)-1]
	if kind == "remove" {
		delete(target, last)
	} else {
		target[last] = value
	}

	return nil
}

func (m *Auth) SCIMPatchGroup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sn := m.cache.Snapshot()
	role, ok := sn.Roles[id]
	if !ok {
		scimFail(w, http.StatusNotFound, "", "group not found")

		return
	}

	var patch scimPatch
	if err := decodeSCIM(r, &patch); err != nil {
		scimFail(w, http.StatusBadRequest, "invalidSyntax", err.Error())

		return
	}

	for _, op := range patch.Operations {
		kind := strings.ToLower(op.Op)
		path := strings.ToLower(op.Path)

		switch {
		case path == "members" || (path == "" && kind != "remove"):
			var members []scimMulti
			raw := op.Value
			if path == "" {
				var obj struct {
					Members     []scimMulti `json:"members"`
					DisplayName string      `json:"displayName"`
				}
				if err := json.Unmarshal(op.Value, &obj); err != nil {
					scimFail(w, http.StatusBadRequest, "invalidValue", err.Error())

					return
				}
				if obj.DisplayName != "" {
					name := obj.DisplayName
					if err := m.store.PatchRole(r.Context(), id, data.RolePatch{Name: &name}); err != nil {
						scimStoreError(w, err)

						return
					}
				}
				if obj.Members == nil {
					continue
				}
				members = obj.Members
			} else if len(raw) > 0 {
				if err := json.Unmarshal(raw, &members); err != nil {
					scimFail(w, http.StatusBadRequest, "invalidValue", err.Error())

					return
				}
			}

			var err error
			switch kind {
			case "add":
				err = m.addGroupMembers(r.Context(), id, memberIDs(members), true)
			case "remove":
				if len(members) == 0 {
					err = m.setGroupMembers(r.Context(), id, nil)
				} else {
					err = m.addGroupMembers(r.Context(), id, memberIDs(members), false)
				}
			case "replace":
				err = m.setGroupMembers(r.Context(), id, memberIDs(members))
			default:
				scimFail(w, http.StatusBadRequest, "invalidSyntax", "unsupported op")

				return
			}
			if err != nil {
				scimStoreError(w, err)

				return
			}
		case strings.HasPrefix(path, "members[value eq "):
			if kind != "remove" {
				scimFail(w, http.StatusBadRequest, "invalidPath", "member filter only supports remove")

				return
			}
			memberID, err := strconv.Unquote(strings.TrimSuffix(strings.TrimPrefix(op.Path[len("members[value eq "):], " "), "]"))
			if err != nil {
				scimFail(w, http.StatusBadRequest, "invalidPath", "invalid member filter")

				return
			}
			if err := m.addGroupMembers(r.Context(), id, []string{memberID}, false); err != nil {
				scimStoreError(w, err)

				return
			}
		case path == "displayname":
			var name string
			if err := json.Unmarshal(op.Value, &name); err != nil || strings.TrimSpace(name) == "" {
				scimFail(w, http.StatusBadRequest, "invalidValue", "displayName must be a string")

				return
			}
			if err := m.store.PatchRole(r.Context(), id, data.RolePatch{Name: &name}); err != nil {
				scimStoreError(w, err)

				return
			}
		case path == "externalid":
			var ext string
			_ = json.Unmarshal(op.Value, &ext)
			roleData := map[string]any{}
			for k, v := range role.Data {
				roleData[k] = v
			}
			roleData[scimUserMarker] = ext
			if err := m.store.PatchRole(r.Context(), id, data.RolePatch{Data: &roleData}); err != nil {
				scimStoreError(w, err)

				return
			}
		default:
			scimFail(w, http.StatusBadRequest, "invalidPath", fmt.Sprintf("unsupported path %q", op.Path))

			return
		}
	}

	sn = m.scimReloaded(r.Context())
	m.audit(r, AuditEvent{Event: auditAdminChange, Outcome: auditSuccess, Detail: map[string]any{"scim": "patch group", "group": id}})
	// RFC 7644 §3.5.2: 200 with the resource, or 204
	scimJSON(w, http.StatusOK, m.toSCIMGroup(r, sn, sn.Roles[id], !strings.Contains(r.URL.Query().Get("excludedAttributes"), "members")))
}

func (m *Auth) addGroupMembers(ctx context.Context, roleID string, ids []string, member bool) error {
	sn := m.scimReloaded(ctx)
	for _, id := range ids {
		u, ok := sn.Users[id]
		if !ok {
			if !member {
				continue
			}

			return fmt.Errorf("member %s not found; %w", id, data.ErrNotFound)
		}
		if slices.Contains(u.RoleIDs, roleID) == member {
			continue
		}
		if err := m.changeMembership(ctx, u, roleID, member); err != nil {
			return err
		}
	}

	return nil
}

// ---- discovery -----------------------------------------------------------

func (m *Auth) SCIMServiceProviderConfig(w http.ResponseWriter, r *http.Request) {
	scimJSON(w, http.StatusOK, map[string]any{
		"schemas":          []string{"urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig"},
		"documentationUri": "https://rakunlabs.github.io/turna/reference/server/http/middlewares/auth#scim-2-0",
		"patch":            map[string]any{"supported": true},
		"bulk":             map[string]any{"supported": false, "maxOperations": 0, "maxPayloadSize": 0},
		"filter":           map[string]any{"supported": true, "maxResults": scimMaxResults},
		"changePassword":   map[string]any{"supported": true},
		"sort":             map[string]any{"supported": false},
		"etag":             map[string]any{"supported": false},
		"authenticationSchemes": []map[string]any{{
			"type": "oauthbearertoken", "name": "Bearer token", "description": "The SCIM token from the auth scim settings", "primary": true,
		}},
		"meta": map[string]any{"resourceType": "ServiceProviderConfig", "location": m.scimBase(r) + "/ServiceProviderConfig"},
	})
}

func (m *Auth) SCIMResourceTypes(w http.ResponseWriter, r *http.Request) {
	base := m.scimBase(r)
	types := []any{
		map[string]any{
			"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"}, "id": "User", "name": "User",
			"endpoint": "/Users", "schema": scimSchemaUser,
			"meta": map[string]any{"resourceType": "ResourceType", "location": base + "/ResourceTypes/User"},
		},
		map[string]any{
			"schemas": []string{"urn:ietf:params:scim:schemas:core:2.0:ResourceType"}, "id": "Group", "name": "Group",
			"endpoint": "/Groups", "schema": scimSchemaGroup,
			"meta": map[string]any{"resourceType": "ResourceType", "location": base + "/ResourceTypes/Group"},
		},
	}
	scimJSON(w, http.StatusOK, scimListResponse{Schemas: []string{scimSchemaList}, TotalResults: len(types), StartIndex: 1, ItemsPerPage: len(types), Resources: types})
}

func (m *Auth) SCIMSchemas(w http.ResponseWriter, r *http.Request) {
	attr := func(name, typ string, required bool, extra ...map[string]any) map[string]any {
		a := map[string]any{"name": name, "type": typ, "multiValued": false, "required": required, "mutability": "readWrite", "returned": "default", "uniqueness": "none"}
		for _, e := range extra {
			for k, v := range e {
				a[k] = v
			}
		}

		return a
	}
	schemas := []any{
		map[string]any{
			"id": scimSchemaUser, "name": "User",
			"attributes": []any{
				attr("userName", "string", true, map[string]any{"uniqueness": "server"}),
				attr("externalId", "string", false),
				attr("displayName", "string", false),
				attr("active", "boolean", false),
				attr("password", "string", false, map[string]any{"mutability": "writeOnly", "returned": "never"}),
				attr("name", "complex", false),
				attr("emails", "complex", false, map[string]any{"multiValued": true}),
				attr("phoneNumbers", "complex", false, map[string]any{"multiValued": true}),
				attr("groups", "complex", false, map[string]any{"multiValued": true, "mutability": "readOnly"}),
			},
		},
		map[string]any{
			"id": scimSchemaGroup, "name": "Group",
			"attributes": []any{
				attr("displayName", "string", true),
				attr("externalId", "string", false),
				attr("members", "complex", false, map[string]any{"multiValued": true}),
			},
		},
	}
	scimJSON(w, http.StatusOK, scimListResponse{Schemas: []string{scimSchemaList}, TotalResults: len(schemas), StartIndex: 1, ItemsPerPage: len(schemas), Resources: schemas})
}
