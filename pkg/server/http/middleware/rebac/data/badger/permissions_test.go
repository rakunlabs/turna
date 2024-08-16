package badger

import (
	"errors"
	"testing"

	"github.com/rakunlabs/turna/pkg/server/http/middleware/rebac/data"
)

func TestBadgerGetPermissions(t *testing.T) {
	tempdir := t.TempDir()

	db, err := New(tempdir)
	if err != nil {
		t.Fatalf("failed to create badger db: %v", err)
	}
	defer db.Close()

	permission := data.Permission{
		ID:   "test",
		Name: "test",
	}

	if err := db.CreatePermission(permission); err != nil {
		t.Fatalf("failed to create permission: %v", err)
	}

	permission2 := data.Permission{
		ID:   "test2",
		Name: "test2",
	}

	if err := db.CreatePermission(permission2); err != nil {
		t.Fatalf("failed to create permission: %v", err)
	}

	req := data.GetPermissionRequest{
		Name: "test",
	}

	res, err := db.GetPermissions(req)
	if err != nil {
		t.Fatalf("failed to get permissions: %v", err)
	}

	if len(res.Payload) != 1 {
		t.Fatalf("expected 1 permission, got %d", len(res.Payload))
	}

	req = data.GetPermissionRequest{
		Name: res.Payload[0].Name,
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
	tempdir := t.TempDir()

	db, err := New(tempdir)
	if err != nil {
		t.Fatalf("failed to create badger db: %v", err)
	}
	defer db.Close()

	permission := data.Permission{
		ID:   "test",
		Name: "test",
	}

	if err := db.CreatePermission(permission); err != nil {
		t.Fatalf("failed to create permission: %v", err)
	}

	err = db.CreatePermission(permission)
	if err == nil {
		t.Fatalf("expected to fail creating permission with the same name")
	}

	if !errors.Is(err, data.ErrConflict) {
		t.Fatalf("expected conflict error, got %v", err)
	}
}
