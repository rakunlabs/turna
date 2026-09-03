package auth

import (
	"testing"
	"time"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
	"github.com/rakunlabs/turna/pkg/server/http/middleware/session"
)

func TestPasskeyEnrollmentSettings(t *testing.T) {
	setting := PasskeySettings{Enrollment: PasskeyEnrollmentSettings{
		Enabled:        true,
		Methods:        []string{session.AuthenticationMethodCode},
		SnoozeDuration: "12h",
	}}
	if err := validatePasskeySettings(setting); err != nil {
		t.Fatalf("validatePasskeySettings: %v", err)
	}
	if !setting.Enrollment.AllowsMethod(session.AuthenticationMethodCode) {
		t.Fatal("configured code method was rejected")
	}
	if setting.Enrollment.AllowsMethod(session.AuthenticationMethodPassword) {
		t.Fatal("unconfigured password method was accepted")
	}

	all := PasskeyEnrollmentSettings{}
	if !all.AllowsMethod(session.AuthenticationMethodPassword) || !all.AllowsMethod(session.AuthenticationMethodPasskey) {
		t.Fatal("empty methods must allow every interactive method")
	}
	if got := all.GetSnoozeDuration(); got != 30*24*time.Hour {
		t.Fatalf("default snooze = %v", got)
	}

	setting.Enrollment.Methods = []string{"unknown"}
	if err := validatePasskeySettings(setting); err == nil {
		t.Fatal("unsupported enrollment method was accepted")
	}
	setting.Enrollment.Methods = nil
	setting.Enrollment.SnoozeDuration = "later"
	if err := validatePasskeySettings(setting); err == nil {
		t.Fatal("invalid enrollment snooze duration was accepted")
	}
}

func testSnapshot() *Snapshot {
	perm := &data.Permission{
		ID:   "perm-1",
		Name: "perm",
		Resources: []data.Resource{
			{
				Methods: []string{"*"},
				Paths:   []string{"/test/**"},
				Excluded: []data.Resource{
					{
						Paths:   []string{"/test/example/excluded/**"},
						Methods: []string{"*"},
					},
				},
			},
		},
		Scope: map[string][]string{
			"openid": {"admin"},
		},
	}

	role := &data.Role{
		ID:            "role-1",
		Name:          "role-test",
		PermissionIDs: []string{"perm-1"},
	}

	parentRole := &data.Role{
		ID:      "role-parent",
		Name:    "role-parent",
		RoleIDs: []string{"role-1"},
	}

	user := &data.User{
		ID:          "user-1",
		Alias:       []string{"my-user"},
		RoleIDs:     []string{"role-parent"},
		Description: "Primary automation account",
		Details: map[string]any{
			"name":     "My User",
			"email":    "my@user.com",
			"password": "password-hash",
			"secret":   "client-secret",
		},
	}

	disabledUser := &data.User{
		ID:       "user-2",
		Alias:    []string{"disabled-user"},
		RoleIDs:  []string{"role-1"},
		Disabled: true,
	}

	return &Snapshot{
		Version: 1,
		Users: map[string]*data.User{
			user.ID:         user,
			disabledUser.ID: disabledUser,
		},
		UserIDs: []string{user.ID, disabledUser.ID},
		Alias: map[string]string{
			"my-user":       user.ID,
			"disabled-user": disabledUser.ID,
		},
		Roles: map[string]*data.Role{
			role.ID:       role,
			parentRole.ID: parentRole,
		},
		RoleIDs: []string{role.ID, parentRole.ID},
		RoleNames: map[string]string{
			role.Name:       role.ID,
			parentRole.Name: parentRole.ID,
		},
		Permissions: map[string]*data.Permission{perm.ID: perm},
		PermIDs:     []string{perm.ID},
		PermNames:   map[string]string{perm.Name: perm.ID},
		LMaps:       map[string]*data.LMap{},
		Check:       data.CheckConfig{NoHostCheck: true},
	}
}

func testCache() *Cache {
	c := NewCache(nil)
	c.snap.Store(testSnapshot())

	return c
}

func TestCacheCheck(t *testing.T) {
	c := testCache()

	cases := []struct {
		name    string
		req     data.CheckRequest
		allowed bool
	}{
		{
			name:    "allowed through virtual role",
			req:     data.CheckRequest{Alias: "my-user", Path: "/test/example/1234", Method: "POST"},
			allowed: true,
		},
		{
			name:    "excluded path",
			req:     data.CheckRequest{Alias: "my-user", Path: "/test/example/excluded/1", Method: "POST"},
			allowed: false,
		},
		{
			name:    "unknown user",
			req:     data.CheckRequest{Alias: "nobody", Path: "/test/x", Method: "GET"},
			allowed: false,
		},
		{
			name:    "disabled user",
			req:     data.CheckRequest{Alias: "disabled-user", Path: "/test/x", Method: "GET"},
			allowed: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := c.Check(tc.req)
			if err != nil {
				t.Fatalf("Check() error = %v", err)
			}

			if resp.Allowed != tc.allowed {
				t.Fatalf("Check() allowed = %v, want %v", resp.Allowed, tc.allowed)
			}
		})
	}
}

func TestCacheGetUserExtended(t *testing.T) {
	c := testCache()

	user, err := c.GetUser(data.GetUserRequest{
		Alias:          "my-user",
		AddRoles:       true,
		AddPermissions: true,
		AddScopeRoles:  true,
	})
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}

	if !user.IsActive {
		t.Fatal("expected user to be active")
	}

	if len(user.Roles) != 2 {
		t.Fatalf("expected 2 roles (direct + virtual), got %d", len(user.Roles))
	}

	if len(user.Permissions) != 1 {
		t.Fatalf("expected 1 permission, got %d", len(user.Permissions))
	}

	if len(user.Scope["openid"]) != 1 || user.Scope["openid"][0] != "admin" {
		t.Fatalf("expected scope openid -> [admin], got %v", user.Scope)
	}
}

func TestCacheGetUsersFilter(t *testing.T) {
	c := testCache()

	res, err := c.GetUsers(data.GetUserRequest{Name: "my"})
	if err != nil {
		t.Fatalf("GetUsers() error = %v", err)
	}

	if res.Meta.TotalItemCount != 1 {
		t.Fatalf("expected 1 user, got %d", res.Meta.TotalItemCount)
	}

	// role-parent expands to role-1; both users match, disabled filter narrows it down
	res, err = c.GetUsers(data.GetUserRequest{RoleIDs: []string{"role-parent"}})
	if err != nil {
		t.Fatalf("GetUsers() error = %v", err)
	}

	if res.Meta.TotalItemCount != 2 {
		t.Fatalf("expected 2 users by virtual role, got %d", res.Meta.TotalItemCount)
	}

	res, err = c.GetUsers(data.GetUserRequest{RoleIDs: []string{"role-parent"}, Disabled: &data.False})
	if err != nil {
		t.Fatalf("GetUsers() error = %v", err)
	}

	if res.Meta.TotalItemCount != 1 || res.Payload[0].ID != "user-1" {
		t.Fatalf("expected only user-1, got %v", res.Meta.TotalItemCount)
	}
}

func TestCacheGetUsersSearch(t *testing.T) {
	c := testCache()

	cases := []struct {
		name   string
		search string
		want   []string
	}{
		{name: "matches alias substring", search: "my-us", want: []string{"user-1"}},
		{name: "matches email substring", search: "my@user", want: []string{"user-1"}},
		{name: "matches detail name fold", search: "MY USER", want: []string{"user-1"}},
		{name: "matches across users", search: "user", want: []string{"user-1", "user-2"}},
		{name: "no match", search: "nobody-here", want: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := c.GetUsers(data.GetUserRequest{Search: tc.search})
			if err != nil {
				t.Fatalf("GetUsers() error = %v", err)
			}

			if int(res.Meta.TotalItemCount) != len(tc.want) {
				t.Fatalf("expected %d users, got %d", len(tc.want), res.Meta.TotalItemCount)
			}

			for i, id := range tc.want {
				if res.Payload[i].ID != id {
					t.Fatalf("expected user %s at %d, got %s", id, i, res.Payload[i].ID)
				}
			}
		})
	}
}

func TestCacheSearchRolesAndPermissions(t *testing.T) {
	c := testCache()

	t.Run("roles", func(t *testing.T) {
		for _, tc := range []struct {
			search string
			want   string
		}{
			{search: "PARENT", want: "role-parent"},
			{search: "ROLE-1", want: "role-1"},
		} {
			roles, err := c.GetRoles(data.GetRoleRequest{Search: tc.search})
			if err != nil {
				t.Fatalf("GetRoles() error = %v", err)
			}

			if roles.Meta.TotalItemCount != 1 || roles.Payload[0].ID != tc.want {
				t.Fatalf("search %q: expected %s, got %v", tc.search, tc.want, roles.Payload)
			}
		}
	})

	t.Run("permissions", func(t *testing.T) {
		for _, search := range []string{"PER", "PERM-1"} {
			perms, err := c.GetPermissions(data.GetPermissionRequest{Search: search})
			if err != nil {
				t.Fatalf("GetPermissions() error = %v", err)
			}

			if perms.Meta.TotalItemCount != 1 || perms.Payload[0].ID != "perm-1" {
				t.Fatalf("search %q: expected perm-1, got %v", search, perms.Payload)
			}
		}

		perms, err := c.GetPermissions(data.GetPermissionRequest{Search: "no-such"})
		if err != nil {
			t.Fatalf("GetPermissions() error = %v", err)
		}

		if perms.Meta.TotalItemCount != 0 {
			t.Fatalf("expected no permissions, got %d", perms.Meta.TotalItemCount)
		}
	})
}

func TestCacheGetUsersSearchPaged(t *testing.T) {
	c := testCache()

	descriptionResult, err := c.GetUsers(data.GetUserRequest{Search: "AUTOMATION"})
	if err != nil {
		t.Fatalf("GetUsers() description search error = %v", err)
	}
	if descriptionResult.Meta.TotalItemCount != 1 || descriptionResult.Payload[0].ID != "user-1" {
		t.Fatalf("description search: expected user-1, got %v", descriptionResult.Payload)
	}

	// search + windowing together: total counts every match, payload is the page
	res, err := c.GetUsers(data.GetUserRequest{Search: "user", Limit: 1, Offset: 1})
	if err != nil {
		t.Fatalf("GetUsers() error = %v", err)
	}

	if res.Meta.TotalItemCount != 2 {
		t.Fatalf("expected total 2, got %d", res.Meta.TotalItemCount)
	}

	if len(res.Payload) != 1 || res.Payload[0].ID != "user-2" {
		t.Fatalf("expected page holding user-2, got %v", res.Payload)
	}
}

func TestCacheSanitizesUserCredentials(t *testing.T) {
	c := testCache()

	user, err := c.GetUser(data.GetUserRequest{ID: "user-1", Sanitize: true})
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	if user.Details["password"] != nil || user.Details["secret"] != nil {
		t.Fatalf("GetUser() exposed credentials: %v", user.Details)
	}

	serviceAccount, err := c.GetUser(data.GetUserRequest{ID: "user-1", Sanitize: true, IncludeSecret: true})
	if err != nil {
		t.Fatalf("GetUser() with secret error = %v", err)
	}
	if serviceAccount.Details["password"] != nil || serviceAccount.Details["secret"] != "client-secret" {
		t.Fatalf("GetUser() returned invalid service account details: %v", serviceAccount.Details)
	}

	users, err := c.GetUsers(data.GetUserRequest{ID: "user-1", Sanitize: true})
	if err != nil {
		t.Fatalf("GetUsers() error = %v", err)
	}
	if users.Payload[0].Details["password"] != nil || users.Payload[0].Details["secret"] != nil {
		t.Fatalf("GetUsers() exposed credentials: %v", users.Payload[0].Details)
	}

	stored := c.Snapshot().Users["user-1"].Details
	if stored["password"] == nil || stored["secret"] == nil {
		t.Fatalf("sanitization mutated cached credentials: %v", stored)
	}
}

func TestCacheGetRolesByPermission(t *testing.T) {
	c := testCache()

	res, err := c.GetRoles(data.GetRoleRequest{Path: "/test/abc", Method: "GET", AddRoles: true, AddTotalUsers: true})
	if err != nil {
		t.Fatalf("GetRoles() error = %v", err)
	}

	if res.Meta.TotalItemCount != 1 {
		t.Fatalf("expected 1 role, got %d", res.Meta.TotalItemCount)
	}

	if res.Payload[0].ID != "role-1" {
		t.Fatalf("expected role-1, got %s", res.Payload[0].ID)
	}
}
