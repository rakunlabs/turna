package badger

import (
	"context"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/iam/data"
)

func TestBadgerGetPermissions(t *testing.T) {
	// tempdir := t.TempDir()

	db, err := New("", "", true, false)
	if err != nil {
		t.Fatalf("failed to create badger db: %v", err)
	}
	defer db.Close()

	permission := data.Permission{
		Name: "test",
	}

	ctx := data.WithContextUserName(context.Background(), "testing")

	if id, err := db.CreatePermission(ctx, permission); err != nil {
		t.Fatalf("failed to create permission: %v", err)
	} else {
		permission.ID = id
	}

	permission2 := data.Permission{
		Name: "test2",
	}

	if _, err := db.CreatePermission(ctx, permission2); err != nil {
		t.Fatalf("failed to create permission: %v", err)
	}

	req := data.GetPermissionRequest{
		ID: permission.ID,
	}

	res, err := db.GetPermissions(req)
	if err != nil {
		t.Fatalf("failed to get permissions: %v", err)
	}

	if len(res.Payload) != 1 {
		t.Fatalf("expected 1 permission, got %d", len(res.Payload))
	}

	req = data.GetPermissionRequest{
		ID: res.Payload[0].ID,
	}

	res, err = db.GetPermissions(req)
	if err != nil {
		t.Fatalf("failed to get permissions: %v", err)
	}

	if len(res.Payload) != 1 {
		t.Fatalf("expected 1 permission, got %d", len(res.Payload))
	}

	req = data.GetPermissionRequest{}

	res, err = db.GetPermissions(req)
	if err != nil {
		t.Fatalf("failed to get permissions: %v", err)
	}
}

func TestBadgerCreatePermission(t *testing.T) {
	// tempdir := t.TempDir()

	db, err := New("", "", true, false)
	if err != nil {
		t.Fatalf("failed to create badger db: %v", err)
	}
	defer db.Close()

	permission := data.Permission{
		Name: "test",
	}

	ctx := data.WithContextUserName(context.Background(), "testing")

	if id, err := db.CreatePermission(ctx, permission); err != nil {
		t.Fatalf("failed to create permission: %v", err)
	} else {
		permission.ID = id
	}

	// _, err = db.CreatePermission(permission)
	// if err == nil {
	// 	t.Fatalf("expected to fail creating permission with the same name")
	// }

	// if !errors.Is(err, data.ErrConflict) {
	// 	t.Fatalf("expected conflict error, got %v", err)
	// }
}
