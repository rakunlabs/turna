package badger

import (
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
)

func TestBadgerCheck(t *testing.T) {
	tempdir := t.TempDir()

	db, err := New(tempdir)
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
		permissions []data.Permission
		roles       []data.Role
		users       []data.User
		check       []checkData
	}{
		{
			name: "test",
			permissions: []data.Permission{
				{
					ID:   "perm",
					Name: "perm",
					Requests: []data.Request{
						{
							Methods: []string{"GET"},
							Path:    "/test",
						},
					},
				},
			},
			roles: []data.Role{
				{
					ID:          "role-test",
					Name:        "role-test",
					Permissions: []string{"perm"},
				},
			},
			users: []data.User{
				{
					ID:    "my-user",
					Alias: []string{"my-user"},
					Roles: []string{"role-test"},
				},
			},
			check: []checkData{
				{
					checkRequest: data.CheckRequest{
						Alias:  "my-user",
						Path:   "/test",
						Method: "GET",
					},
					expected: true,
				},
			},
		},
	}

	for _, tc := range testCases {
		for _, permission := range tc.permissions {
			if err := db.CreatePermission(permission); err != nil {
				t.Fatalf("failed to create permission: %v", err)
			}
		}

		for _, role := range tc.roles {
			if err := db.CreateRole(role); err != nil {
				t.Fatalf("failed to create role: %v", err)
			}
		}

		for _, user := range tc.users {
			if err := db.CreateUser(user); err != nil {
				t.Fatalf("failed to create user: %v", err)
			}
		}

		for _, check := range tc.check {
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
