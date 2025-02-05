package badger

import (
	"context"
	"testing"

	"github.com/worldline-go/turna/pkg/server/http/middleware/iam/data"
)

func TestGetRoles(t *testing.T) {
	// tempdir := t.TempDir()

	db, err := New("", "", true, false, data.CheckConfig{})
	if err != nil {
		t.Fatalf("failed to create badger db: %v", err)
	}
	defer db.Close()

	role := data.Role{
		Name: "test",
	}

	ctx := data.WithContextUserName(context.Background(), "testing")

	if _, err := db.CreateRole(ctx, role); err != nil {
		t.Fatalf("failed to create role: %v", err)
	}

	role2 := data.Role{
		Name: "role2",
	}

	if _, err := db.CreateRole(ctx, role2); err != nil {
		t.Fatalf("failed to create role: %v", err)
	}

	req := data.GetRoleRequest{
		Name: "test",
	}

	res, err := db.GetRoles(req)
	if err != nil {
		t.Fatalf("failed to get roles: %v", err)
	}

	if len(res.Payload) != 1 {
		t.Fatalf("expected 1 role, got %d", len(res.Payload))
	}

	req = data.GetRoleRequest{
		Name: res.Payload[0].Name,
	}

	res, err = db.GetRoles(req)
	if err != nil {
		t.Fatalf("failed to get roles: %v", err)
	}

	if len(res.Payload) != 1 {
		t.Fatalf("expected 1 role, got %d", len(res.Payload))
	}

	req = data.GetRoleRequest{}

	res, err = db.GetRoles(req)
	if err != nil {
		t.Fatalf("failed to get roles: %v", err)
	}
}
