package auth

import (
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

func hasAccessID(values []data.IDName, id string) bool {
	for _, item := range values {
		if item.ID == id {
			return true
		}
	}

	return false
}

// TestAPIKeyUserAccess covers the two access modes of a key: an empty key
// acts as its owner (live inheritance), an explicit key carries only its own
// list.
func TestAPIKeyUserAccess(t *testing.T) {
	m := &Auth{cache: testCache()}

	// user-1 carries role-parent -> role-1 -> perm-1 in the test snapshot.
	owner, err := m.cache.GetUser(data.GetUserRequest{ID: "user-1"})
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}

	t.Run("empty access inherits the owner live", func(t *testing.T) {
		user := m.apiKeyUser(&APIKeyMeta{ID: "key-1", UserID: "user-1"}, owner)

		if !hasAccessID(user.Roles, "role-parent") {
			t.Fatalf("expected inherited role role-parent, got %v", user.Roles)
		}
		if !hasAccessID(user.Permissions, "perm-1") {
			t.Fatalf("expected inherited permission perm-1, got %v", user.Permissions)
		}
	})

	t.Run("explicit access does not inherit", func(t *testing.T) {
		user := m.apiKeyUser(&APIKeyMeta{
			ID:            "key-2",
			UserID:        "user-1",
			PermissionIDs: []string{"perm-1"},
		}, owner)

		if len(user.Roles) != 0 {
			t.Fatalf("expected no inherited roles, got %v", user.Roles)
		}
		if !hasAccessID(user.Permissions, "perm-1") {
			t.Fatalf("expected explicit permission perm-1, got %v", user.Permissions)
		}
	})

	t.Run("system key carries exactly its own access", func(t *testing.T) {
		user := m.apiKeyUser(&APIKeyMeta{ID: "key-3", RoleIDs: []string{"role-1"}}, nil)

		if !hasAccessID(user.Roles, "role-1") {
			t.Fatalf("expected explicit role role-1, got %v", user.Roles)
		}
		if !hasAccessID(user.Permissions, "perm-1") {
			t.Fatalf("expected role-derived permission perm-1, got %v", user.Permissions)
		}
		if _, ok := user.Details["owner_user_id"]; ok {
			t.Fatal("system key must not carry owner_user_id")
		}
	})
}

// TestAPIKeyRegistryAccess covers the system-key validation path: ids must
// exist in the registry, and the create path refuses a key with no access.
func TestAPIKeyRegistryAccess(t *testing.T) {
	m := &Auth{cache: testCache()}

	t.Run("known ids are accepted", func(t *testing.T) {
		roleIDs, permissionIDs, err := m.apiKeyRegistryAccess(&[]string{"role-1"}, &[]string{"perm-1"})
		if err != nil {
			t.Fatalf("apiKeyRegistryAccess() error = %v", err)
		}
		if len(roleIDs) != 1 || len(permissionIDs) != 1 {
			t.Fatalf("expected 1 role + 1 permission, got %v / %v", roleIDs, permissionIDs)
		}
	})

	t.Run("unknown role is refused", func(t *testing.T) {
		if _, _, err := m.apiKeyRegistryAccess(&[]string{"missing-role"}, nil); err == nil {
			t.Fatal("expected error for a role that does not exist")
		}
	})

	t.Run("unknown permission is refused", func(t *testing.T) {
		if _, _, err := m.apiKeyRegistryAccess(nil, &[]string{"missing-perm"}); err == nil {
			t.Fatal("expected error for a permission that does not exist")
		}
	})
}

// TestAPIKeyAccessForOwner checks that explicit lists are validated against
// the owner and that empty lists stay empty for live inheritance.
func TestAPIKeyAccessForOwner(t *testing.T) {
	m := &Auth{cache: testCache()}

	owner, err := m.cache.GetUser(data.GetUserRequest{ID: "user-1", AddRoles: true, AddPermissions: true})
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}

	t.Run("nil requests stay empty", func(t *testing.T) {
		roleIDs, permissionIDs, err := apiKeyAccessForOwner(owner, nil, nil)
		if err != nil {
			t.Fatalf("apiKeyAccessForOwner() error = %v", err)
		}
		if len(roleIDs) != 0 || len(permissionIDs) != 0 {
			t.Fatalf("expected empty access, got roles %v permissions %v", roleIDs, permissionIDs)
		}
	})

	t.Run("empty arrays stay empty", func(t *testing.T) {
		roleIDs, permissionIDs, err := apiKeyAccessForOwner(owner, &[]string{}, &[]string{})
		if err != nil {
			t.Fatalf("apiKeyAccessForOwner() error = %v", err)
		}
		if len(roleIDs) != 0 || len(permissionIDs) != 0 {
			t.Fatalf("expected empty access, got roles %v permissions %v", roleIDs, permissionIDs)
		}
	})

	t.Run("owner subset is accepted", func(t *testing.T) {
		roleIDs, _, err := apiKeyAccessForOwner(owner, &[]string{"role-parent"}, nil)
		if err != nil {
			t.Fatalf("apiKeyAccessForOwner() error = %v", err)
		}
		if len(roleIDs) != 1 || roleIDs[0] != "role-parent" {
			t.Fatalf("expected [role-parent], got %v", roleIDs)
		}
	})

	t.Run("foreign access is refused", func(t *testing.T) {
		if _, _, err := apiKeyAccessForOwner(owner, &[]string{"not-owned"}, nil); err == nil {
			t.Fatal("expected error for a role the owner does not carry")
		}
	})
}
