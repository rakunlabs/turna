package auth

import (
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

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
		ID:      "user-1",
		Alias:   []string{"my-user"},
		RoleIDs: []string{"role-parent"},
		Details: map[string]any{"name": "My User", "email": "my@user.com"},
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
