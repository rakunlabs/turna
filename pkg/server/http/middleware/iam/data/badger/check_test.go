package badger

import (
	"context"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

func TestBadgerCheck(t *testing.T) {
	// tempdir := t.TempDir()

	db, err := New("", "", true, false, data.CheckConfig{})
	// db, err := New(tempdir, false)
	if err != nil {
		t.Fatalf("failed to create badger db: %v", err)
	}
	defer db.Close()

	type checkData struct {
		checkRequest data.CheckRequest
		expected     bool
	}

	testCases := []struct {
		name        string
		checkConfig data.CheckConfig
		permissions []data.Permission
		roles       []data.Role
		users       []data.User
		check       []checkData
	}{
		{
			name: "test",
			checkConfig: data.CheckConfig{
				DefaultHosts: []string{"*.example.com"},
			},
			permissions: []data.Permission{
				{
					Name: "perm",
					Resources: []data.Resource{
						{
							Methods: []string{"*"},
							Path:    "/test/**",
							// Hosts:   []string{"*.example.com"},
						},
					},
				},
			},
			roles: []data.Role{
				{
					ID:            "role-test",
					Name:          "role-test",
					PermissionIDs: []string{"perm"},
				},
			},
			users: []data.User{
				{
					Alias:   []string{"my-user"},
					RoleIDs: []string{"role-test"},
				},
			},
			check: []checkData{
				{
					checkRequest: data.CheckRequest{
						Alias:  "my-user",
						Path:   "/test/example/1234",
						Method: "POST",
						Host:   "test.example.com",
					},
					expected: true,
				},
			},
		},
	}

	ctx := data.WithContextUserName(context.Background(), "testing")

	for _, tc := range testCases {
		for i := range tc.permissions {
			id, err := db.CreatePermission(ctx, tc.permissions[i])
			if err != nil {
				t.Fatalf("failed to create permission: %v", err)
			}

			tc.permissions[i].ID = id
		}

		for i := range tc.roles {
			// find permission ids
			permissions := make([]string, 0)
			for _, permission := range tc.permissions {
				for _, rolePermissionID := range tc.roles[i].PermissionIDs {
					if rolePermissionID == permission.Name {
						permissions = append(permissions, permission.ID)
					}
				}
			}

			tc.roles[i].PermissionIDs = permissions

			id, err := db.CreateRole(ctx, tc.roles[i])
			if err != nil {
				t.Fatalf("failed to create role: %v", err)
			}

			tc.roles[i].ID = id
		}

		for i := range tc.users {
			// find role ids
			roles := make([]string, 0)
			for _, role := range tc.roles {
				for _, userRoleID := range tc.users[i].RoleIDs {
					if userRoleID == role.Name {
						roles = append(roles, role.ID)
					}
				}
			}

			tc.users[i].RoleIDs = roles

			id, err := db.CreateUser(ctx, tc.users[i])
			if err != nil {
				t.Fatalf("failed to create user: %v", err)
			}

			tc.users[i].ID = id
		}

		for _, check := range tc.check {
			db.check = tc.checkConfig
			access, err := db.Check(check.checkRequest)
			if err != nil {
				t.Fatalf("failed to check access: %v", err)
			}

			if access.Allowed != check.expected {
				t.Fatalf("expected access %v, got %v", check.expected, access)
			}
		}
	}
}
